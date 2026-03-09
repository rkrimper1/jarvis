// GRPCVoiceService.swift
// Jarvis iOS Client
//
// Manages the full lifecycle of the VoiceService.Converse bidirectional gRPC
// stream. This is the single point of contact between the iOS app and the
// Jarvis voice backend — AudioCaptureEngine feeds audio in, VoiceViewModel
// consumes events out.
//
// Dependencies (via Swift Package Manager):
//   - grpc-swift       → https://github.com/grpc/grpc-swift  (>= 2.0.0)
//   - swift-protobuf   → https://github.com/apple/swift-protobuf (>= 1.26.0)
//
// Generated stubs expected at:
//   JarvisClient/Proto/voice.pb.swift        (swift-protobuf)
//   JarvisClient/Proto/voice.grpc.swift      (grpc-swift)

import Foundation
import GRPC
import NIOCore
import NIOPosix
import SwiftProtobuf
import os.log

// MARK: - Public Event Types

/// All downstream events the app can receive from the Jarvis voice backend.
/// Delivered on the MainActor via the `onEvent` callback.
public enum VoiceEvent {
    case statusChanged(JarvisStatusEvent.State, message: String)
    case transcriptReceived(text: String, isFinal: Bool, confidence: Float)
    case replyReceived(replyText: String, intent: String, requiresConfirmation: Bool)
    case audioReplyReceived(data: Data, text: String, isFinalChunk: Bool)
    case hudActionReceived(JarvisHUDAction)
    case streamError(VoiceServiceError)
    case streamClosed
}

/// Errors surfaced by GRPCVoiceService.
public enum VoiceServiceError: Error, LocalizedError {
    case notConnected
    case sessionCapacityExceeded
    case streamSetupFailed(underlying: Error)
    case sendFailed(underlying: Error)
    case reconnectLimitReached(attempts: Int)
    case invalidConfiguration(String)

    public var errorDescription: String? {
        switch self {
        case .notConnected:
            return "Voice stream is not connected."
        case .sessionCapacityExceeded:
            return "Jarvis backend has reached its session limit."
        case .streamSetupFailed(let err):
            return "Stream setup failed: \(err.localizedDescription)"
        case .sendFailed(let err):
            return "Failed to send audio: \(err.localizedDescription)"
        case .reconnectLimitReached(let n):
            return "Reconnect failed after \(n) attempts."
        case .invalidConfiguration(let msg):
            return "Invalid configuration: \(msg)"
        }
    }
}

// MARK: - Configuration

/// All tuneable parameters for GRPCVoiceService.
public struct VoiceServiceConfiguration {
    /// gRPC server host, e.g. "jarvis.yourdomain.com" or "localhost".
    public var host: String

    /// gRPC server port. Typically 443 (TLS) or 50059 (dev plaintext).
    public var port: Int

    /// Use TLS. Always true in production; false for local docker-compose dev.
    public var useTLS: Bool

    /// BCP-47 language code forwarded to the backend STT engine.
    public var languageCode: String

    /// Optional voice ID for TTS reply synthesis.
    public var voiceID: String

    /// NLP context tags forwarded to nlp-service ParseIntent per utterance.
    public var contextTags: [String]

    /// Maximum number of automatic reconnect attempts on stream failure.
    public var maxReconnectAttempts: Int

    /// Base delay for exponential backoff reconnect (doubles each attempt).
    public var reconnectBaseDelay: TimeInterval

    /// NIO thread count for the gRPC channel's event loop group.
    public var nioThreads: Int

    public static var development: VoiceServiceConfiguration {
        VoiceServiceConfiguration(
            host: "localhost",
            port: 50059,
            useTLS: false,
            languageCode: "en-US",
            voiceID: "",
            contextTags: [],
            maxReconnectAttempts: 5,
            reconnectBaseDelay: 0.5,
            nioThreads: 1
        )
    }

    public static var production: VoiceServiceConfiguration {
        VoiceServiceConfiguration(
            host: "voice.jarvis.yourdomain.com",
            port: 443,
            useTLS: true,
            languageCode: "en-US",
            voiceID: "",
            contextTags: [],
            maxReconnectAttempts: 8,
            reconnectBaseDelay: 1.0,
            nioThreads: 2
        )
    }
}

// MARK: - GRPCVoiceService

/// Manages the bidirectional gRPC stream for VoiceService.Converse.
///
/// Usage:
/// ```swift
/// let service = GRPCVoiceService(configuration: .development) { event in
///     // handle VoiceEvent on the main actor
/// }
/// await service.connect(userID: "tony", sessionID: UUID().uuidString)
/// await service.sendAudioChunk(data: pcmData, capturedAt: .now, isWakeWordFrame: false)
/// await service.sendEndOfSpeech()
/// await service.disconnect()
/// ```
@MainActor
public final class GRPCVoiceService: ObservableObject {

    // MARK: - Published State

    @Published public private(set) var isConnected: Bool = false
    @Published public private(set) var currentState: JarvisStatusEvent.State = .idle
    @Published public private(set) var reconnectAttempt: Int = 0

    // MARK: - Private

    private let config: VoiceServiceConfiguration
    private let onEvent: (VoiceEvent) -> Void
    private let log = Logger(subsystem: "com.jarvis.client", category: "GRPCVoiceService")

    /// NIO event loop group shared across reconnects.
    private var group: MultiThreadedEventLoopGroup?

    /// gRPC channel — recreated on reconnect.
    private var channel: GRPCChannel?

    /// The active bidirectional call handle.
    private var call: BidirectionalStreamingCall<JarvisConverseRequest, JarvisConverseResponse>?

    /// Active session metadata set on connect.
    private var sessionID: String = ""
    private var userID: String = ""

    /// Sequence counter for outgoing AudioChunks.
    private var sequenceNum: Int64 = 0

    /// Guards against concurrent connect/disconnect calls.
    private var isConnecting: Bool = false

    // MARK: - Init

    public init(
        configuration: VoiceServiceConfiguration,
        onEvent: @escaping (VoiceEvent) -> Void
    ) {
        self.config = configuration
        self.onEvent = onEvent
    }

    deinit {
        // Best-effort shutdown — channel and group are reference types
        // so no await needed here; teardown happens asynchronously.
        try? channel?.close().wait()
        try? group?.syncShutdownGracefully()
    }
}

// MARK: - Public Interface

extension GRPCVoiceService {

    /// Opens the gRPC channel, dials the server, and sends the StreamConfig
    /// as the very first message. Safe to call again after a disconnect.
    public func connect(userID: String, sessionID: String) async {
        guard !isConnecting && !isConnected else { return }
        isConnecting = true
        defer { isConnecting = false }

        self.userID    = userID
        self.sessionID = sessionID
        self.sequenceNum = 0

        do {
            try await openChannelAndStream()
            isConnected = true
            reconnectAttempt = 0
            log.info("Connected — session: \(sessionID)")
        } catch {
            log.error("Connection failed: \(error.localizedDescription)")
            onEvent(.streamError(.streamSetupFailed(underlying: error)))
        }
    }

    /// Sends a raw PCM audio chunk upstream.
    /// Call this from AudioCaptureEngine's frame callback.
    public func sendAudioChunk(
        data: Data,
        capturedAt: Date = .now,
        isWakeWordFrame: Bool = false
    ) async {
        sequenceNum += 1
        let chunk = JarvisAudioChunk.with {
            $0.data             = data
            $0.sequenceNum      = sequenceNum
            $0.capturedAtMs     = Int64(capturedAt.timeIntervalSince1970 * 1000)
            $0.isWakeWordFrame  = isWakeWordFrame
        }
        await send(.with { $0.audio = chunk })
    }

    /// Signals end-of-speech. Triggers the STT → NLP → TTS pipeline on the server.
    public func sendEndOfSpeech() async {
        await sendControlEvent(.endOfSpeech)
    }

    /// Cancels the current in-flight pipeline without closing the stream.
    public func sendCancel() async {
        await sendControlEvent(.cancel)
    }

    /// Sends a keep-alive ping to prevent stream timeout during silence.
    public func sendKeepAlive() async {
        await sendControlEvent(.keepAlive)
    }

    /// Signals the start of a new conversation turn (e.g. PTT re-pressed).
    public func sendNewTurn() async {
        await sendControlEvent(.newTurn)
    }

    /// Gracefully closes the stream and channel.
    public func disconnect() async {
        log.info("Disconnecting — session: \(sessionID)")
        await teardown()
        onEvent(.streamClosed)
    }
}

// MARK: - Internal: Channel + Stream Setup

private extension GRPCVoiceService {

    func openChannelAndStream() async throws {
        // ── 1. NIO Event Loop Group ─────────────────────────────────────
        let elg = MultiThreadedEventLoopGroup(numberOfThreads: config.nioThreads)
        self.group = elg

        // ── 2. gRPC Channel ─────────────────────────────────────────────
        var channelBuilder = ClientConnection
            .usingPlatformAppropriateTLS(for: elg)

        if !config.useTLS {
            channelBuilder = ClientConnection.insecure(group: elg)
        }

        let channel = channelBuilder
            .withBackgroundActivityLogger(makeGRPCLogger())
            .connect(host: config.host, port: config.port)

        self.channel = channel

        // ── 3. Stub ─────────────────────────────────────────────────────
        let stub = JarvisVoiceServiceNIOClient(channel: channel)

        // ── 4. Open bidirectional stream ─────────────────────────────────
        let call = stub.converse(
            callOptions: makeCallOptions(),
            handler: { [weak self] response in
                // Responses arrive on a NIO thread — hop to MainActor.
                Task { @MainActor [weak self] in
                    self?.handleResponse(response)
                }
            }
        )
        self.call = call

        // ── 5. Send StreamConfig as first message ────────────────────────
        let configMsg = JarvisConverseRequest.with {
            $0.config = makeStreamConfig()
        }
        try await call.sendMessage(configMsg).get()

        // ── 6. Observe stream completion for reconnect logic ─────────────
        Task { [weak self] in
            do {
                // Awaits until the server closes the stream or an error occurs.
                _ = try await call.status.get()
                await MainActor.run { [weak self] in
                    guard let self else { return }
                    self.isConnected = false
                    self.log.info("Stream closed cleanly")
                    self.onEvent(.streamClosed)
                }
            } catch {
                await MainActor.run { [weak self] in
                    guard let self else { return }
                    self.isConnected = false
                    self.log.error("Stream error: \(error.localizedDescription)")
                    self.onEvent(.streamError(.streamSetupFailed(underlying: error)))
                    Task { await self.attemptReconnect() }
                }
            }
        }
    }

    // MARK: StreamConfig factory

    func makeStreamConfig() -> JarvisStreamConfig {
        JarvisStreamConfig.with {
            $0.meta = JarvisRequestMeta.with {
                $0.requestID = UUID().uuidString
                $0.userID    = userID
                $0.sessionID = sessionID
                $0.source    = "voice"
            }
            $0.clientInfo = JarvisClientInfo.with {
                $0.platform    = "ios"
                $0.appVersion  = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "unknown"
                $0.deviceModel = UIDevice.current.model
                $0.osVersion   = UIDevice.current.systemVersion
            }
            $0.audioConfig = JarvisAudioConfig.with {
                $0.encoding        = .pcm16Bit
                $0.sampleRateHz    = 16000
                $0.channelCount    = 1
                $0.frameDurationMs = 20
            }
            $0.languageCode = config.languageCode
            $0.voiceID      = config.voiceID
            $0.contextTags  = config.contextTags
        }
    }
}

// MARK: - Internal: Response Handling

private extension GRPCVoiceService {

    /// Dispatches a VoiceResponse to the appropriate VoiceEvent.
    /// Always called on the MainActor.
    func handleResponse(_ response: JarvisConverseResponse) {
        switch response.payload {

        case .status(let s):
            currentState = s.state
            onEvent(.statusChanged(s.state, message: s.message))

        case .transcript(let t):
            onEvent(.transcriptReceived(
                text:       t.text,
                isFinal:    t.isFinal,
                confidence: t.confidence
            ))

        case .reply(let r):
            onEvent(.replyReceived(
                replyText:            r.replyText,
                intent:               r.intent,
                requiresConfirmation: r.requiresConfirmation
            ))

        case .audioReply(let a):
            onEvent(.audioReplyReceived(
                data:         a.data,
                text:         a.text,
                isFinalChunk: a.isFinalChunk
            ))

        case .action(let a):
            onEvent(.hudActionReceived(a))

        case .none:
            log.warning("Received VoiceResponse with no payload")
        }
    }
}

// MARK: - Internal: Send Helpers

private extension GRPCVoiceService {

    func send(_ request: JarvisConverseRequest) async {
        guard let call else {
            onEvent(.streamError(.notConnected))
            return
        }
        do {
            try await call.sendMessage(request).get()
        } catch {
            log.error("sendMessage failed: \(error.localizedDescription)")
            onEvent(.streamError(.sendFailed(underlying: error)))
        }
    }

    func sendControlEvent(_ type: JarvisControlEvent.TypeEnum) async {
        let event = JarvisConverseRequest.with {
            $0.event = JarvisControlEvent.with { $0.type = type }
        }
        await send(event)
    }
}

// MARK: - Internal: Reconnect

private extension GRPCVoiceService {

    /// Exponential backoff reconnect. Preserves the original userID/sessionID
    /// so the server-side session store can stitch the stream back together.
    func attemptReconnect() async {
        guard reconnectAttempt < config.maxReconnectAttempts else {
            log.error("Reconnect limit reached (\(config.maxReconnectAttempts) attempts)")
            onEvent(.streamError(.reconnectLimitReached(attempts: config.maxReconnectAttempts)))
            return
        }

        reconnectAttempt += 1
        let delay = config.reconnectBaseDelay * pow(2.0, Double(reconnectAttempt - 1))
        log.info("Reconnect attempt \(reconnectAttempt) in \(String(format: "%.1f", delay))s")

        try? await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))

        await teardown()
        await connect(userID: userID, sessionID: sessionID)
    }
}

// MARK: - Internal: Teardown

private extension GRPCVoiceService {

    func teardown() async {
        // Half-close the send side (tells the server the client is done sending).
        try? await call?.sendEnd().get()
        call = nil

        // Close the channel and shut down NIO.
        try? await channel?.close().get()
        channel = nil

        try? group?.syncShutdownGracefully()
        group = nil

        isConnected = false
    }
}

// MARK: - Internal: gRPC Utilities

private extension GRPCVoiceService {

    func makeCallOptions() -> CallOptions {
        var options = CallOptions()
        // Auth token injected here — replace with your token provider.
        // options.customMetadata.add(name: "authorization", value: "Bearer \(token)")
        options.timeLimit = .none   // streaming — no deadline
        return options
    }

    func makeGRPCLogger() -> Logger {
        Logger(subsystem: "com.jarvis.client", category: "GRPC")
    }
}
