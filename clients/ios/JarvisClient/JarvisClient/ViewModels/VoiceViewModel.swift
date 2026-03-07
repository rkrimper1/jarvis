// VoiceViewModel.swift
// Jarvis iOS Client — clients/ios/JarvisClient/JarvisClient/ViewModels/
//
// The single source of truth for all voice interaction state.
// Owns and coordinates:
//   AudioCaptureEngine  → microphone frames
//   WakeWordDetector    → "Hey Jarvis" trigger
//   GRPCVoiceService    → bidirectional backend stream
//
// State machine:
//   idle ──[wake word]──▶ listening ──[end of speech / VAD]──▶ processing
//     ▲                                                              │
//     └────────────── idle ◀── speaking ◀───────────────────────────┘
//
// SwiftUI views observe @Published properties directly.
// No business logic lives in views.

import Foundation
import SwiftUI
import AVFoundation
import os.log

// MARK: - Voice State

/// Mirrors StatusEvent.State from voice.proto — drives HUD rendering.
public enum VoiceState: Equatable {
    case idle
    case listening
    case processing
    case speaking
    case error(String)

    var displayLabel: String {
        switch self {
        case .idle:          return "Standby"
        case .listening:     return "Listening…"
        case .processing:    return "Processing…"
        case .speaking:      return "Speaking…"
        case .error(let m):  return "Error: \(m)"
        }
    }

    var isActive: Bool {
        self == .listening || self == .processing || self == .speaking
    }
}

// MARK: - HUD Action Model

/// Decoded client-side representation of a HUDAction proto message.
public struct HUDActionModel: Identifiable {
    public let id = UUID()
    public let type: HUDActionType
    public let payloadJSON: String
    public let severity: HUDSeverity
    public let receivedAt: Date
}

public enum HUDActionType: String {
    case openApp         = "TYPE_OPEN_APP"
    case showCard        = "TYPE_SHOW_CARD"
    case setTimer        = "TYPE_SET_TIMER"
    case navigate        = "TYPE_NAVIGATE"
    case dismissHUD      = "TYPE_DISMISS_HUD"
    case dispatchAgent   = "TYPE_DISPATCH_AGENT"
    case hardwareCmd     = "TYPE_HARDWARE_CMD"
    case securityProtocol = "TYPE_SECURITY_PROTOCOL"
    case unknown         = "TYPE_UNKNOWN"
}

public enum HUDSeverity {
    case info, warning, critical, emergency

    var color: Color {
        switch self {
        case .info:      return .blue
        case .warning:   return .yellow
        case .critical:  return .orange
        case .emergency: return .red
        }
    }
}

// MARK: - Transcript Model

public struct TranscriptLine: Identifiable {
    public let id = UUID()
    public let text: String
    public let isFinal: Bool
    public let confidence: Float
    public let timestamp: Date
    public let source: TranscriptSource
}

public enum TranscriptSource {
    case user       // STT from microphone
    case jarvis     // NLP reply text
}

// MARK: - VoiceViewModel

@MainActor
public final class VoiceViewModel: ObservableObject {

    // MARK: - Published: Core state

    /// Current voice pipeline state — primary driver of HUD rendering.
    @Published public private(set) var voiceState: VoiceState = .idle

    /// True when the gRPC stream is connected to the backend.
    @Published public private(set) var isConnected: Bool = false

    /// True when AudioCaptureEngine is running (mic active).
    @Published public private(set) var isMicActive: Bool = false

    // MARK: - Published: Audio

    /// Live RMS energy from the mic — drives the waveform view [0, 1].
    @Published public private(set) var micRMS: Float = 0.0

    // MARK: - Published: Transcript

    /// The current in-progress (possibly partial) user utterance.
    @Published public private(set) var liveTranscript: String = ""

    /// Full conversation history shown in the HUD transcript view.
    @Published public private(set) var transcriptHistory: [TranscriptLine] = []

    // MARK: - Published: Reply

    /// The most recent Jarvis reply text.
    @Published public private(set) var lastReply: String = ""

    /// True when Jarvis needs confirmation before acting.
    @Published public private(set) var requiresConfirmation: Bool = false

    /// The intent label from the last NLP response.
    @Published public private(set) var lastIntent: String = ""

    // MARK: - Published: HUD Actions

    /// Pending HUD actions the view layer should render or execute.
    @Published public private(set) var pendingActions: [HUDActionModel] = []

    // MARK: - Published: TTS Audio

    /// Accumulated TTS audio data for the current reply (plays when isFinalChunk).
    @Published public private(set) var ttsAudioData: Data? = nil

    // MARK: - Published: Errors

    /// Most recent non-fatal error message for the HUD error badge.
    @Published public private(set) var lastErrorMessage: String? = nil

    // MARK: - Dependencies

    private let grpcService: GRPCVoiceService
    private let audioEngine: AudioCaptureEngine
    private let wakeWordDetector: WakeWordDetector
    private let ttsPlayer: TTSAudioPlayer

    // MARK: - Identity

    private let userID: String
    private let sessionID: String

    // MARK: - Internal state

    /// True after wake word fires, before end-of-speech — gates audio streaming.
    private var isStreaming: Bool = false

    /// Accumulates TTS chunks until isFinalChunk.
    private var ttsBuffer: Data = Data()

    private let log = Logger(subsystem: "com.jarvis.client", category: "VoiceViewModel")

    // MARK: - Init

    public init(
        userID: String,
        sessionID: String = UUID().uuidString,
        grpcConfiguration: VoiceServiceConfiguration = .development
    ) {
        self.userID    = userID
        self.sessionID = sessionID

        self.ttsPlayer = TTSAudioPlayer()

        self.grpcService = GRPCVoiceService(
            configuration: grpcConfiguration,
            onEvent: { _ in }   // replaced below after init
        )

        self.audioEngine     = AudioCaptureEngine()
        self.wakeWordDetector = WakeWordDetector(configuration: .default)

        wireCallbacks()
    }
}

// MARK: - Public Interface

extension VoiceViewModel {

    /// Starts the full pipeline: mic → wake word → gRPC stream.
    public func start() async {
        log.info("VoiceViewModel starting — user: \(self.userID) session: \(self.sessionID)")

        do {
            // 1. Start wake word detector first (needs speech permission).
            try await wakeWordDetector.start()

            // 2. Start mic capture.
            try await audioEngine.start()
            isMicActive = true

            // 3. Connect gRPC stream (StreamConfig sent immediately).
            await grpcService.connect(userID: userID, sessionID: sessionID)

            voiceState = .idle
            log.info("Pipeline ready — waiting for wake word")

        } catch let err as AudioCaptureError {
            handleLocalError("Mic error: \(err.localizedDescription)")
        } catch let err as WakeWordDetectorError {
            handleLocalError("Wake word error: \(err.localizedDescription)")
        } catch {
            handleLocalError(error.localizedDescription)
        }
    }

    /// Stops the pipeline gracefully.
    public func stop() async {
        log.info("VoiceViewModel stopping")

        wakeWordDetector.stop()
        await audioEngine.stop()
        await grpcService.disconnect()

        isStreaming  = false
        isMicActive  = false
        isConnected  = false
        voiceState   = .idle
        micRMS       = 0
        liveTranscript = ""
    }

    /// Manually signals end-of-speech (e.g. push-to-talk button release).
    public func endSpeech() async {
        guard isStreaming else { return }
        isStreaming = false
        await grpcService.sendEndOfSpeech()
        log.debug("Manual end-of-speech sent")
    }

    /// Cancels the current in-flight pipeline.
    public func cancel() async {
        isStreaming = false
        voiceState = .idle
        liveTranscript = ""
        await grpcService.sendCancel()
    }

    /// Confirms a pending action when requiresConfirmation == true.
    public func confirm() async {
        requiresConfirmation = false
        await grpcService.sendNewTurn()
    }

    /// Dismisses the top pending HUD action.
    public func dismissTopAction() {
        guard !pendingActions.isEmpty else { return }
        pendingActions.removeFirst()
    }

    /// Clears transcript history.
    public func clearHistory() {
        transcriptHistory.removeAll()
    }
}

// MARK: - Private: Callback Wiring

private extension VoiceViewModel {

    func wireCallbacks() {
        wireAudioEngine()
        wireWakeWordDetector()
        wireGRPCService()
    }

    // MARK: AudioCaptureEngine

    func wireAudioEngine() {
        audioEngine.onFrame = { [weak self] frame in
            guard let self else { return }

            // Always update the waveform RMS.
            self.micRMS = frame.rmsEnergy

            // Feed every frame into the wake word detector.
            self.wakeWordDetector.feed(frame)

            // Only stream audio upstream after the wake word has fired.
            guard self.isStreaming else { return }

            await self.grpcService.sendAudioChunk(
                data:            frame.pcmData,
                capturedAt:      frame.capturedAt,
                isWakeWordFrame: false
            )
        }

        audioEngine.onError = { [weak self] error in
            self?.handleLocalError(error.localizedDescription)
        }
    }

    // MARK: WakeWordDetector

    func wireWakeWordDetector() {
        wakeWordDetector.onDetection = { [weak self] detection in
            guard let self else { return }
            guard self.voiceState == .idle else { return }  // ignore re-fires while active

            self.log.info("Wake word: \"\(detection.matchedPhrase)\" — opening stream")
            self.isStreaming   = true
            self.liveTranscript = ""
            self.voiceState    = .listening

            // Pause TTS playback if Jarvis is mid-sentence.
            self.ttsPlayer.stop()

            // Send the trigger frame marked as wake word — server skips VAD gate.
            await self.grpcService.sendAudioChunk(
                data:            detection.triggerFrame.pcmData,
                capturedAt:      detection.triggerFrame.capturedAt,
                isWakeWordFrame: true
            )
        }

        wakeWordDetector.onError = { [weak self] error in
            self?.handleLocalError(error.localizedDescription)
        }
    }

    // MARK: GRPCVoiceService

    func wireGRPCService() {
        // Re-create the service wiring now that self exists.
        // GRPCVoiceService stores the closure at init — we use a forwarding
        // pattern so VoiceViewModel is the single handler.
        grpcService.onEvent = { [weak self] event in
            guard let self else { return }
            self.handleVoiceEvent(event)
        }

        // Mirror GRPCVoiceService.isConnected into our own published property.
        grpcService.$isConnected
            .receive(on: RunLoop.main)
            .assign(to: &$isConnected)
    }
}

// MARK: - Private: VoiceEvent Handler

private extension VoiceViewModel {

    func handleVoiceEvent(_ event: VoiceEvent) {
        switch event {

        case .statusChanged(let state, let message):
            handleStatusChange(state, message: message)

        case .transcriptReceived(let text, let isFinal, let confidence):
            handleTranscript(text: text, isFinal: isFinal, confidence: confidence)

        case .replyReceived(let replyText, let intent, let requiresConfirmation):
            handleReply(text: replyText, intent: intent, requiresConfirmation: requiresConfirmation)

        case .audioReplyReceived(let data, let text, let isFinalChunk):
            handleAudioReply(data: data, text: text, isFinalChunk: isFinalChunk)

        case .hudActionReceived(let action):
            handleHUDAction(action)

        case .streamError(let error):
            handleLocalError(error.localizedDescription)

        case .streamClosed:
            voiceState  = .idle
            isStreaming = false
            log.info("Stream closed")
        }
    }

    // MARK: Status

    func handleStatusChange(_ state: JarvisStatusEvent.State, message: String) {
        log.debug("Status → \(String(describing: state))")
        switch state {
        case .idle:
            voiceState  = .idle
            isStreaming = false
            // Resume mic for next wake word.
            try? audioEngine.resume()

        case .listening:
            voiceState = .listening

        case .processing:
            voiceState  = .processing
            isStreaming = false   // stop sending audio — utterance is committed
            liveTranscript = ""  // clear partial; final arrives via transcript event

        case .speaking:
            voiceState = .speaking
            // Pause mic to avoid capturing TTS output.
            audioEngine.pause()

        case .error:
            voiceState  = .error(message.isEmpty ? "Unknown error" : message)
            isStreaming = false
            lastErrorMessage = message

        case .ended:
            voiceState  = .idle
            isStreaming = false

        default:
            break
        }
    }

    // MARK: Transcript

    func handleTranscript(text: String, isFinal: Bool, confidence: Float) {
        liveTranscript = isFinal ? "" : text

        if isFinal && !text.isEmpty {
            let line = TranscriptLine(
                text:       text,
                isFinal:    true,
                confidence: confidence,
                timestamp:  Date(),
                source:     .user
            )
            appendTranscript(line)
            log.info("Final transcript: \"\(text)\" (conf: \(String(format: "%.2f", confidence)))")
        }
    }

    // MARK: Reply

    func handleReply(text: String, intent: String, requiresConfirmation: Bool) {
        lastReply                 = text
        lastIntent                = intent
        self.requiresConfirmation = requiresConfirmation

        let line = TranscriptLine(
            text:       text,
            isFinal:    true,
            confidence: 1.0,
            timestamp:  Date(),
            source:     .jarvis
        )
        appendTranscript(line)
        log.info("Reply: \"\(text)\" intent=\(intent) confirm=\(requiresConfirmation)")
    }

    // MARK: Audio Reply (TTS)

    func handleAudioReply(data: Data, text: String, isFinalChunk: Bool) {
        ttsBuffer.append(data)

        if isFinalChunk {
            let audio = ttsBuffer
            ttsBuffer = Data()
            ttsAudioData = audio
            ttsPlayer.play(audio)
            log.debug("TTS playback started — \(audio.count) bytes")
        }
    }

    // MARK: HUD Action

    func handleHUDAction(_ proto: JarvisHUDAction) {
        let typeStr  = proto.type.description   // e.g. "TYPE_SHOW_CARD"
        let actionType = HUDActionType(rawValue: typeStr) ?? .unknown
        let severity   = mapSeverity(proto.severity)

        let model = HUDActionModel(
            type:        actionType,
            payloadJSON: proto.payloadJson,
            severity:    severity,
            receivedAt:  Date()
        )

        pendingActions.insert(model, at: 0)   // newest first

        // Auto-dismiss non-critical actions after 8 seconds.
        if severity != .critical && severity != .emergency {
            Task {
                try? await Task.sleep(nanoseconds: 8_000_000_000)
                if let idx = pendingActions.firstIndex(where: { $0.id == model.id }) {
                    pendingActions.remove(at: idx)
                }
            }
        }

        // Immediately execute dismissal actions.
        if actionType == .dismissHUD {
            pendingActions.removeAll()
        }

        log.info("HUD action: \(typeStr) severity=\(String(describing: severity))")
    }

    // MARK: Errors

    func handleLocalError(_ message: String) {
        log.error("VoiceViewModel error: \(message)")
        voiceState       = .error(message)
        lastErrorMessage = message
        isStreaming      = false
    }

    // MARK: Helpers

    func appendTranscript(_ line: TranscriptLine) {
        transcriptHistory.append(line)
        // Cap history to avoid unbounded memory growth.
        if transcriptHistory.count > 200 {
            transcriptHistory.removeFirst(transcriptHistory.count - 200)
        }
    }

    func mapSeverity(_ proto: JarvisCommonSeverity) -> HUDSeverity {
        switch proto {
        case .info:      return .info
        case .warning:   return .warning
        case .critical:  return .critical
        case .emergency: return .emergency
        default:         return .info
        }
    }
}

// MARK: - TTSAudioPlayer

/// Lightweight AVAudioPlayer wrapper for TTS AudioReply payloads.
/// Kept private to VoiceViewModel — only VoiceViewModel drives playback.
private final class TTSAudioPlayer: NSObject, AVAudioPlayerDelegate {

    private var player: AVAudioPlayer?
    private let log = Logger(subsystem: "com.jarvis.client", category: "TTSAudioPlayer")

    func play(_ data: Data) {
        stop()
        do {
            player = try AVAudioPlayer(data: data)
            player?.delegate = self
            player?.prepareToPlay()
            player?.play()
        } catch {
            log.error("TTSAudioPlayer failed: \(error.localizedDescription)")
        }
    }

    func stop() {
        player?.stop()
        player = nil
    }

    func audioPlayerDidFinishPlaying(_ player: AVAudioPlayer, successfully flag: Bool) {
        self.player = nil
    }
}

// MARK: - GRPCVoiceService onEvent setter

// GRPCVoiceService stores its callback at init. We expose a settable
// property here so VoiceViewModel can wire itself after both objects exist.
extension GRPCVoiceService {
    /// Settable event callback — used by VoiceViewModel to replace the
    /// no-op closure passed at init time.
    var onEvent: ((VoiceEvent) -> Void)? {
        get { _onEvent }
        set { _onEvent = newValue ?? { _ in } }
    }
}
