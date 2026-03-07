// AudioCaptureEngine.swift
// Jarvis iOS Client — clients/ios/JarvisClient/JarvisClient/Services/
//
// Owns the full AVAudioEngine capture pipeline:
//   Microphone → AVAudioInputNode → format conversion → PCM tap
//   → chunked frames → GRPCVoiceService.sendAudioChunk()
//
// Design constraints:
//   - 16 kHz mono PCM-16LE output — matches AudioConfig in voice.proto
//   - 20 ms frames — matches StreamConfig.audio_config.frame_duration_ms
//   - All AVAudioEngine work happens on a dedicated serial queue
//   - Public API is @MainActor — callers never touch AVAudio internals
//   - Plays well with WakeWordDetector: exposes a shared frame tap
//   - Interruption + route-change handling (AirPods swap, phone call, etc.)

import AVFoundation
import Accelerate
import os.log

// MARK: - Public Types

/// A single captured audio frame ready to send upstream.
public struct AudioFrame {
    /// Raw PCM-16LE bytes at 16 kHz mono.
    public let pcmData: Data
    /// Wall-clock time the frame was captured (for AudioChunk.captured_at_ms).
    public let capturedAt: Date
    /// RMS energy [0, 1] — used by WakeWordDetector and VAD.
    public let rmsEnergy: Float
    /// Frame sequence number within the current capture session.
    public let sequenceNumber: Int64
}

/// Errors the engine can surface.
public enum AudioCaptureError: Error, LocalizedError {
    case permissionDenied
    case engineStartFailed(underlying: Error)
    case formatConversionFailed
    case alreadyRunning
    case notRunning

    public var errorDescription: String? {
        switch self {
        case .permissionDenied:        return "Microphone permission denied."
        case .engineStartFailed(let e): return "Audio engine failed to start: \(e.localizedDescription)"
        case .formatConversionFailed:  return "PCM format conversion failed."
        case .alreadyRunning:          return "Audio capture is already running."
        case .notRunning:              return "Audio capture is not running."
        }
    }
}

// MARK: - AudioCaptureEngine

/// Manages the AVAudioEngine microphone capture pipeline.
///
/// Typical usage from VoiceViewModel:
/// ```swift
/// let engine = AudioCaptureEngine()
/// engine.onFrame = { frame in
///     await grpcService.sendAudioChunk(
///         data: frame.pcmData,
///         capturedAt: frame.capturedAt,
///         isWakeWordFrame: false
///     )
/// }
/// try await engine.start()
/// // ... later ...
/// await engine.stop()
/// ```
@MainActor
public final class AudioCaptureEngine: ObservableObject {

    // MARK: - Published state

    @Published public private(set) var isRunning: Bool = false
    @Published public private(set) var currentRMS: Float = 0.0

    // MARK: - Callbacks

    /// Called on the MainActor for every captured 20 ms PCM frame.
    /// Wire this to GRPCVoiceService and WakeWordDetector.
    public var onFrame: ((AudioFrame) async -> Void)?

    /// Called on the MainActor when a non-recoverable error occurs.
    public var onError: ((AudioCaptureError) -> Void)?

    // MARK: - Configuration

    /// Target output sample rate. Must match StreamConfig.audio_config.sample_rate_hz.
    public let targetSampleRate: Double = 16_000
    /// Output channel count. Mono = 1.
    public let targetChannels: AVAudioChannelCount = 1
    /// Frame duration in milliseconds. Must match StreamConfig.audio_config.frame_duration_ms.
    public let frameDurationMs: Int = 20

    // MARK: - Private

    private let engine = AVAudioEngine()
    private let audioQueue = DispatchSerialQueue(label: "com.jarvis.audio", qos: .userInteractive)
    private let log = Logger(subsystem: "com.jarvis.client", category: "AudioCaptureEngine")

    /// Converter from the hardware input format to our target 16 kHz mono PCM format.
    private var converter: AVAudioConverter?
    /// Target AVAudioFormat: 16 kHz, mono, Float32 (converted to Int16 for wire).
    private var targetFormat: AVAudioFormat?
    /// Accumulates converted samples until we have exactly one 20 ms frame.
    private var frameBuffer: [Int16] = []
    /// Expected number of Int16 samples per 20 ms frame at 16 kHz.
    private var samplesPerFrame: Int = 0
    /// Monotonic frame counter reset on each start().
    private var sequenceNumber: Int64 = 0

    // MARK: - Init / Deinit

    public init() {}

    deinit {
        engine.stop()
        removeNotificationObservers()
    }
}

// MARK: - Public Interface

extension AudioCaptureEngine {

    /// Requests microphone permission, configures the AVAudioSession,
    /// installs the tap, and starts the engine.
    public func start() async throws {
        guard !isRunning else { throw AudioCaptureError.alreadyRunning }

        try await requestMicrophonePermission()
        try configureAudioSession()
        try setupEngine()

        do {
            try engine.start()
        } catch {
            throw AudioCaptureError.engineStartFailed(underlying: error)
        }

        sequenceNumber = 0
        frameBuffer.removeAll(keepingCapacity: true)
        isRunning = true
        log.info("AudioCaptureEngine started — \(Int(self.targetSampleRate)) Hz mono \(self.frameDurationMs) ms frames")
    }

    /// Removes the tap and stops the engine gracefully.
    public func stop() async {
        guard isRunning else { return }
        audioQueue.sync {
            self.engine.inputNode.removeTap(onBus: 0)
        }
        engine.stop()
        isRunning = false
        currentRMS = 0.0
        log.info("AudioCaptureEngine stopped")
    }

    /// Pauses capture without tearing down the engine (e.g. during TTS playback).
    public func pause() {
        guard isRunning else { return }
        engine.pause()
        log.info("AudioCaptureEngine paused")
    }

    /// Resumes a paused engine.
    public func resume() throws {
        guard isRunning else { return }
        do {
            try engine.start()
            log.info("AudioCaptureEngine resumed")
        } catch {
            throw AudioCaptureError.engineStartFailed(underlying: error)
        }
    }
}

// MARK: - Private: Permission

private extension AudioCaptureEngine {

    func requestMicrophonePermission() async throws {
        let status = AVCaptureDevice.authorizationStatus(for: .audio)
        switch status {
        case .authorized:
            return
        case .notDetermined:
            let granted = await AVCaptureDevice.requestAccess(for: .audio)
            if !granted { throw AudioCaptureError.permissionDenied }
        case .denied, .restricted:
            throw AudioCaptureError.permissionDenied
        @unknown default:
            throw AudioCaptureError.permissionDenied
        }
    }
}

// MARK: - Private: AVAudioSession

private extension AudioCaptureEngine {

    func configureAudioSession() throws {
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(
            .playAndRecord,
            mode: .voiceChat,           // optimises AGC + echo cancellation
            options: [
                .defaultToSpeaker,
                .allowBluetooth,        // AirPods / BT headsets
                .allowBluetoothA2DP
            ]
        )
        try session.setPreferredSampleRate(targetSampleRate)
        try session.setPreferredIOBufferDuration(Double(frameDurationMs) / 1000.0)
        try session.setActive(true)

        addNotificationObservers()
    }
}

// MARK: - Private: Engine + Tap

private extension AudioCaptureEngine {

    func setupEngine() throws {
        let inputNode = engine.inputNode
        let hwFormat  = inputNode.outputFormat(forBus: 0)

        guard let target = AVAudioFormat(
            commonFormat: .pcmFormatFloat32,
            sampleRate: targetSampleRate,
            channels: targetChannels,
            interleaved: false
        ) else {
            throw AudioCaptureError.formatConversionFailed
        }
        self.targetFormat = target

        guard let conv = AVAudioConverter(from: hwFormat, to: target) else {
            throw AudioCaptureError.formatConversionFailed
        }
        self.converter = conv

        // samples per frame = sample_rate × frame_duration_ms / 1000
        samplesPerFrame = Int(targetSampleRate) * frameDurationMs / 1000   // 320

        // Tap buffer size: match hardware I/O buffer duration.
        // AVAudioEngine will deliver buffers close to this size but not guaranteed.
        let tapBufferSize = AVAudioFrameCount(hwFormat.sampleRate * Double(frameDurationMs) / 1000.0)

        inputNode.installTap(
            onBus: 0,
            bufferSize: tapBufferSize,
            format: hwFormat
        ) { [weak self] buffer, time in
            // This closure runs on AVAudioEngine's internal render thread.
            // We do all work synchronously on audioQueue to stay off the main thread.
            self?.audioQueue.sync {
                self?.processTapBuffer(buffer, time: time)
            }
        }

        engine.prepare()
        log.debug("Engine configured: hw=\(hwFormat.sampleRate)Hz → target=\(self.targetSampleRate)Hz, tapSize=\(tapBufferSize)")
    }

    // MARK: Tap callback (on audioQueue)

    func processTapBuffer(_ buffer: AVAudioPCMBuffer, time: AVAudioTime) {
        guard
            let converter,
            let targetFormat,
            let converted = convert(buffer: buffer, using: converter, to: targetFormat)
        else {
            log.error("Format conversion failed — dropping buffer")
            return
        }

        // Flatten Float32 channels to Int16 samples and append to accumulator.
        let int16Samples = toInt16(converted)
        frameBuffer.append(contentsOf: int16Samples)

        // Drain complete 20 ms frames from the accumulator.
        while frameBuffer.count >= samplesPerFrame {
            let frameSamples = Array(frameBuffer.prefix(samplesPerFrame))
            frameBuffer.removeFirst(samplesPerFrame)

            let rms    = computeRMS(frameSamples)
            let pcm    = int16ToData(frameSamples)
            let now    = Date()

            sequenceNumber += 1
            let frame = AudioFrame(
                pcmData:        pcm,
                capturedAt:     now,
                rmsEnergy:      rms,
                sequenceNumber: sequenceNumber
            )

            // Hop to MainActor to call the async onFrame callback.
            let capturedCallback = onFrame
            Task { @MainActor [weak self] in
                self?.currentRMS = rms
                await capturedCallback?(frame)
            }
        }
    }
}

// MARK: - Private: DSP Helpers

private extension AudioCaptureEngine {

    /// Converts an AVAudioPCMBuffer from the hardware format to the target format.
    func convert(
        buffer: AVAudioPCMBuffer,
        using converter: AVAudioConverter,
        to targetFormat: AVAudioFormat
    ) -> AVAudioPCMBuffer? {
        // Output frame capacity scaled by sample rate ratio.
        let ratio = targetFormat.sampleRate / buffer.format.sampleRate
        let outFrames = AVAudioFrameCount(Double(buffer.frameLength) * ratio)

        guard let output = AVAudioPCMBuffer(pcmFormat: targetFormat, frameCapacity: outFrames) else {
            return nil
        }

        var error: NSError?
        var inputDone = false

        let status = converter.convert(to: output, error: &error) { _, outStatus in
            if inputDone {
                outStatus.pointee = .noDataNow
                return nil
            }
            outStatus.pointee = .haveData
            inputDone = true
            return buffer
        }

        guard status != .error, error == nil else {
            log.error("AVAudioConverter error: \(error?.localizedDescription ?? "unknown")")
            return nil
        }

        return output
    }

    /// Converts Float32 AVAudioPCMBuffer samples to an [Int16] array.
    func toInt16(_ buffer: AVAudioPCMBuffer) -> [Int16] {
        guard
            let floatData = buffer.floatChannelData?[0]
        else { return [] }

        let count = Int(buffer.frameLength)
        var result = [Int16](repeating: 0, count: count)

        // vDSP: scale Float32 [-1,1] → Int16 range, then convert.
        var scale = Float(Int16.max)
        var scaled = [Float](repeating: 0, count: count)
        vDSP_vsmul(floatData, 1, &scale, &scaled, 1, vDSP_Length(count))
        vDSP_vfixr16(&scaled, 1, &result, 1, vDSP_Length(count))

        return result
    }

    /// Packs [Int16] samples into little-endian Data for the wire.
    func int16ToData(_ samples: [Int16]) -> Data {
        samples.withUnsafeBytes { Data($0) }
    }

    /// Computes normalised RMS energy [0, 1] for a frame of Int16 samples.
    func computeRMS(_ samples: [Int16]) -> Float {
        guard !samples.isEmpty else { return 0 }
        let floats = samples.map { Float($0) / Float(Int16.max) }
        var rms: Float = 0
        vDSP_rmsqv(floats, 1, &rms, vDSP_Length(floats.count))
        return rms
    }
}

// MARK: - Private: Interruption & Route Change Handling

private extension AudioCaptureEngine {

    func addNotificationObservers() {
        NotificationCenter.default.addObserver(
            self,
            selector: #selector(handleInterruption(_:)),
            name: AVAudioSession.interruptionNotification,
            object: nil
        )
        NotificationCenter.default.addObserver(
            self,
            selector: #selector(handleRouteChange(_:)),
            name: AVAudioSession.routeChangeNotification,
            object: nil
        )
    }

    func removeNotificationObservers() {
        NotificationCenter.default.removeObserver(self)
    }

    /// Handles phone calls, Siri interruptions, etc.
    @objc func handleInterruption(_ notification: Notification) {
        guard
            let info = notification.userInfo,
            let typeValue = info[AVAudioSessionInterruptionTypeKey] as? UInt,
            let type = AVAudioSession.InterruptionType(rawValue: typeValue)
        else { return }

        switch type {
        case .began:
            log.info("Audio interruption began — pausing engine")
            Task { @MainActor in self.engine.pause() }

        case .ended:
            guard
                let optionsValue = info[AVAudioSessionInterruptionOptionKey] as? UInt,
                AVAudioSession.InterruptionOptions(rawValue: optionsValue).contains(.shouldResume)
            else { return }

            log.info("Audio interruption ended — resuming engine")
            Task { @MainActor in
                do { try self.engine.start() }
                catch { self.onError?(.engineStartFailed(underlying: error)) }
            }

        @unknown default:
            break
        }
    }

    /// Handles AirPods connects/disconnects, headphone jack pull, etc.
    @objc func handleRouteChange(_ notification: Notification) {
        guard
            let info = notification.userInfo,
            let reasonValue = info[AVAudioSessionRouteChangeReasonKey] as? UInt,
            let reason = AVAudioSession.RouteChangeReason(rawValue: reasonValue)
        else { return }

        switch reason {
        case .oldDeviceUnavailable:
            // e.g. headphones unplugged — stop to avoid capturing from wrong source
            log.info("Audio route changed (old device unavailable) — stopping")
            Task { @MainActor in await self.stop() }

        case .newDeviceAvailable:
            // e.g. AirPods connected — reconfigure and restart
            log.info("New audio route available — reconfiguring engine")
            Task { @MainActor in
                await self.stop()
                do { try await self.start() }
                catch { self.onError?(error as? AudioCaptureError ?? .engineStartFailed(underlying: error)) }
            }

        default:
            break
        }
    }
}
