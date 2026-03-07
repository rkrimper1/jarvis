// WakeWordDetector.swift
// Jarvis iOS Client — clients/ios/JarvisClient/JarvisClient/Services/
//
// On-device "Hey Jarvis" wake word detection.
//
// Architecture:
//   Phase 1 — SFSpeechRecognizer (ships today, zero extra dependencies)
//     Runs a rolling local recognition request on the 16 kHz PCM stream
//     delivered by AudioCaptureEngine. When a final or high-confidence
//     partial transcript contains the wake phrase, it fires the callback
//     and signals GRPCVoiceService to open the Converse stream.
//
//   Phase 2 — CoreML model (drop-in swap, same public API)
//     Export openWakeWord or a custom model to .mlmodel, set
//     WakeWordConfiguration.backend = .coreML(modelURL: ...) and the
//     detector swaps the recogniser under the hood without any call-site
//     changes. The CoreML path stub is included but guarded so the file
//     compiles today.
//
// Threading:
//   - Public API is @MainActor
//   - SFSpeechRecognizer callbacks arrive on an internal queue →
//     hopped to MainActor before any state mutation
//   - AudioCaptureEngine delivers AudioFrame on MainActor via its
//     onFrame callback — feed() is therefore also @MainActor

import Foundation
import Speech
import AVFoundation
import os.log

// MARK: - Public Types

/// Which recognition backend to use.
public enum WakeWordBackend {
    /// Phase 1: Apple on-device SFSpeechRecognizer (default).
    case speechRecognizer
    /// Phase 2: Custom CoreML model (e.g. openWakeWord exported to .mlmodel).
    case coreML(modelURL: URL)
}

/// Detection sensitivity trade-off.
public enum WakeWordSensitivity {
    /// Fewer false positives, may miss softer utterances.
    case low
    /// Balanced default.
    case medium
    /// More triggers, higher false-positive rate.
    case high

    /// Minimum SFSpeechRecognitionResult confidence to accept as a trigger.
    var confidenceThreshold: Float {
        switch self {
        case .low:    return 0.85
        case .medium: return 0.70
        case .high:   return 0.50
        }
    }
}

/// All tuneable parameters for WakeWordDetector.
public struct WakeWordConfiguration {
    /// The phrase to listen for. Case-insensitive.
    public var wakePhrase: String

    /// Additional accepted variants (e.g. ["hey jarvis", "jarvis"]).
    public var phraseVariants: [String]

    /// Which recognition backend to use.
    public var backend: WakeWordBackend

    /// Detection confidence sensitivity.
    public var sensitivity: WakeWordSensitivity

    /// Rolling audio window fed to the recogniser (seconds).
    /// Shorter = lower latency; longer = more context for accuracy.
    public var recognitionWindowSec: Double

    /// Minimum seconds between consecutive detections (debounce).
    public var debounceSec: Double

    /// BCP-47 locale for the speech recogniser.
    public var locale: Locale

    public static var `default`: WakeWordConfiguration {
        WakeWordConfiguration(
            wakePhrase:           "hey jarvis",
            phraseVariants:       ["jarvis", "hey jarvis", "okay jarvis"],
            backend:              .speechRecognizer,
            sensitivity:          .medium,
            recognitionWindowSec: 3.0,
            debounceSec:          1.5,
            locale:               Locale(identifier: "en-US")
        )
    }
}

/// Outcome of a single detection attempt.
public struct WakeWordDetection {
    /// The phrase variant that matched.
    public let matchedPhrase: String
    /// Confidence from the recogniser [0, 1].
    public let confidence: Float
    /// The full transcript the recogniser returned.
    public let fullTranscript: String
    /// The AudioFrame that triggered the detection.
    public let triggerFrame: AudioFrame
    /// Wall-clock detection time.
    public let detectedAt: Date
}

/// Errors WakeWordDetector can surface.
public enum WakeWordDetectorError: Error, LocalizedError {
    case speechPermissionDenied
    case recognizerUnavailable
    case recognizerStartFailed(underlying: Error)
    case coreMLModelLoadFailed(url: URL, underlying: Error)
    case alreadyRunning
    case notRunning

    public var errorDescription: String? {
        switch self {
        case .speechPermissionDenied:
            return "Speech recognition permission denied."
        case .recognizerUnavailable:
            return "On-device speech recognizer is unavailable for the requested locale."
        case .recognizerStartFailed(let e):
            return "Speech recognizer failed to start: \(e.localizedDescription)"
        case .coreMLModelLoadFailed(let url, let e):
            return "Failed to load CoreML model at \(url.lastPathComponent): \(e.localizedDescription)"
        case .alreadyRunning:
            return "Wake word detector is already running."
        case .notRunning:
            return "Wake word detector is not running."
        }
    }
}

// MARK: - WakeWordDetector

/// Listens to AudioCaptureEngine frames and fires onDetection when the
/// wake phrase is recognised.
///
/// Usage from VoiceViewModel:
/// ```swift
/// let detector = WakeWordDetector(configuration: .default)
/// detector.onDetection = { detection in
///     // mark the trigger frame, transition HUD to LISTENING
///     await grpcService.sendAudioChunk(
///         data: detection.triggerFrame.pcmData,
///         capturedAt: detection.triggerFrame.capturedAt,
///         isWakeWordFrame: true          // ← tells server to skip VAD gate
///     )
/// }
/// try await detector.start()
///
/// // In AudioCaptureEngine.onFrame:
/// await detector.feed(frame)
/// ```
@MainActor
public final class WakeWordDetector: ObservableObject {

    // MARK: - Published state

    @Published public private(set) var isRunning: Bool = false
    @Published public private(set) var lastDetection: WakeWordDetection?

    // MARK: - Callbacks

    /// Fired on MainActor when the wake phrase is detected.
    public var onDetection: ((WakeWordDetection) async -> Void)?

    /// Fired on MainActor for non-fatal state changes (e.g. recogniser reset).
    public var onStateChange: ((String) -> Void)?

    /// Fired on MainActor when a non-recoverable error occurs.
    public var onError: ((WakeWordDetectorError) -> Void)?

    // MARK: - Configuration

    public let configuration: WakeWordConfiguration

    // MARK: - Private: SFSpeech

    private var recognizer: SFSpeechRecognizer?
    private var recognitionRequest: SFSpeechAudioBufferRecognitionRequest?
    private var recognitionTask: SFSpeechRecognitionTask?

    /// Rolling AVAudioPCMBuffer fed into the recognition request.
    private var audioFormat: AVAudioFormat?

    // MARK: - Private: State

    private var lastDetectionTime: Date = .distantPast
    private let log = Logger(subsystem: "com.jarvis.client", category: "WakeWordDetector")

    /// Normalised accepted phrases (lowercased, whitespace-trimmed).
    private var acceptedPhrases: [String] = []

    // MARK: - Init

    public init(configuration: WakeWordConfiguration = .default) {
        self.configuration = configuration
        self.acceptedPhrases = ([configuration.wakePhrase] + configuration.phraseVariants)
            .map { $0.lowercased().trimmingCharacters(in: .whitespaces) }
    }
}

// MARK: - Public Interface

extension WakeWordDetector {

    /// Requests speech permission and starts the recognition pipeline.
    public func start() async throws {
        guard !isRunning else { throw WakeWordDetectorError.alreadyRunning }

        switch configuration.backend {
        case .speechRecognizer:
            try await startSpeechRecognizer()
        case .coreML(let url):
            try await startCoreML(modelURL: url)
        }

        isRunning = true
        log.info("WakeWordDetector started — phrase: \"\(self.configuration.wakePhrase)\"")
    }

    /// Stops detection and tears down the recognition pipeline.
    public func stop() {
        guard isRunning else { return }
        teardownSpeechRecognizer()
        isRunning = false
        log.info("WakeWordDetector stopped")
    }

    /// Feed an AudioFrame from AudioCaptureEngine into the detector.
    /// Call this inside AudioCaptureEngine.onFrame alongside sendAudioChunk.
    public func feed(_ frame: AudioFrame) {
        guard isRunning else { return }

        switch configuration.backend {
        case .speechRecognizer:
            feedSpeechRecognizer(frame: frame)
        case .coreML:
            feedCoreML(frame: frame)
        }
    }
}

// MARK: - Private: SFSpeechRecognizer path

private extension WakeWordDetector {

    func startSpeechRecognizer() async throws {
        try await requestSpeechPermission()

        let recognizer = SFSpeechRecognizer(locale: configuration.locale)
        guard let recognizer, recognizer.isAvailable else {
            throw WakeWordDetectorError.recognizerUnavailable
        }
        recognizer.supportsOnDeviceRecognition = true   // never phone home
        self.recognizer = recognizer

        // 16 kHz mono Float32 — matches AudioCaptureEngine output format
        guard let format = AVAudioFormat(
            commonFormat: .pcmFormatFloat32,
            sampleRate: 16_000,
            channels: 1,
            interleaved: false
        ) else {
            throw WakeWordDetectorError.recognizerStartFailed(
                underlying: NSError(domain: "WakeWord", code: -1,
                    userInfo: [NSLocalizedDescriptionKey: "Failed to create AVAudioFormat"])
            )
        }
        self.audioFormat = format

        startNewRecognitionRequest(recognizer: recognizer, format: format)
    }

    func startNewRecognitionRequest(recognizer: SFSpeechRecognizer, format: AVAudioFormat) {
        let request = SFSpeechAudioBufferRecognitionRequest()
        request.shouldReportPartialResults       = true
        request.requiresOnDeviceRecognition      = true
        request.taskHint                         = .dictation  // continuous listening
        // Provide the phrase as a hint to improve recognition accuracy.
        request.contextualStrings                = acceptedPhrases
        self.recognitionRequest = request

        recognitionTask = recognizer.recognitionTask(with: request) { [weak self] result, error in
            // Arrives on SFSpeech's internal queue → hop to MainActor.
            Task { @MainActor [weak self] in
                self?.handleRecognitionResult(result, error: error,
                                             recognizer: recognizer, format: format)
            }
        }

        log.debug("Recognition request started")
    }

    // MARK: Feed PCM frames into the recognition request

    func feedSpeechRecognizer(frame: AudioFrame) {
        guard
            let request = recognitionRequest,
            let format  = audioFormat
        else { return }

        // Unpack PCM-16LE Data back to Float32 AVAudioPCMBuffer for SF.
        guard let buffer = pcmDataToBuffer(frame.pcmData, format: format) else { return }
        request.append(buffer)
    }

    // MARK: Result handler (MainActor)

    func handleRecognitionResult(
        _ result: SFSpeechRecognitionResult?,
        error: Error?,
        recognizer: SFSpeechRecognizer,
        format: AVAudioFormat
    ) {
        // If the task ended (final result or error) restart for continuous listening.
        if let error {
            let nsErr = error as NSError
            // Code 301 = "No speech detected" — normal silence, restart quietly.
            let isSilence = nsErr.domain == "kAFAssistantErrorDomain" && nsErr.code == 301
            if !isSilence {
                log.warning("Recognizer error (\(nsErr.code)): \(nsErr.localizedDescription) — restarting")
                onStateChange?("Recogniser restarted after error")
            }
            teardownSpeechRecognizer()
            startNewRecognitionRequest(recognizer: recognizer, format: format)
            return
        }

        guard let result else { return }

        let transcript   = result.bestTranscription.formattedString.lowercased()
        let confidence   = result.bestTranscription.segments.first?.confidence ?? 0

        // Check every accepted phrase against the rolling transcript.
        guard let matched = acceptedPhrases.first(where: { transcript.contains($0) }) else {
            // Restart after a final non-matching result to keep the window fresh.
            if result.isFinal {
                teardownSpeechRecognizer()
                startNewRecognitionRequest(recognizer: recognizer, format: format)
            }
            return
        }

        // Confidence gate.
        guard confidence >= configuration.sensitivity.confidenceThreshold else {
            log.debug("Wake phrase seen but confidence \(confidence) < threshold \(self.configuration.sensitivity.confidenceThreshold)")
            return
        }

        // Debounce — prevent rapid re-fires.
        let now = Date()
        guard now.timeIntervalSince(lastDetectionTime) >= configuration.debounceSec else {
            log.debug("Wake phrase debounced")
            return
        }
        lastDetectionTime = now

        log.info("Wake word detected — \"\(matched)\" (confidence \(String(format: "%.2f", confidence)))")

        // We don't have the exact trigger frame here (the recogniser buffers audio
        // internally), so we create a synthetic one. The isWakeWordFrame flag on
        // the *next* real AudioFrame from the tap is what matters to the server.
        let detection = WakeWordDetection(
            matchedPhrase:  matched,
            confidence:     confidence,
            fullTranscript: transcript,
            triggerFrame:   makeSyntheticFrame(),
            detectedAt:     now
        )

        lastDetection = detection

        // Restart recognition immediately so we're ready for the next wake.
        teardownSpeechRecognizer()
        startNewRecognitionRequest(recognizer: recognizer, format: format)

        Task { await onDetection?(detection) }
    }

    func teardownSpeechRecognizer() {
        recognitionTask?.cancel()
        recognitionTask = nil
        recognitionRequest?.endAudio()
        recognitionRequest = nil
    }
}

// MARK: - Private: CoreML path (Phase 2 stub)

private extension WakeWordDetector {

    /// Loads and validates a CoreML .mlmodel.
    /// Replace the stub body with MLModel inference when the model is available.
    func startCoreML(modelURL: URL) async throws {
        // Phase 2 implementation:
        //
        //   let compiled = try await MLModel.compileModel(at: modelURL)
        //   let model    = try MLModel(contentsOf: compiled)
        //   self.coreMLModel = model
        //
        // For now throw a clear error so the call-site knows Phase 2 isn't wired.
        log.warning("CoreML backend selected but not yet implemented — falling back not possible")
        throw WakeWordDetectorError.coreMLModelLoadFailed(
            url: modelURL,
            underlying: NSError(
                domain: "WakeWord",
                code: -2,
                userInfo: [NSLocalizedDescriptionKey: "CoreML backend is Phase 2 — not yet implemented"]
            )
        )
    }

    /// Feed a frame into the CoreML inference pipeline.
    func feedCoreML(frame: AudioFrame) {
        // Phase 2: run MLFeatureProvider with frame.pcmData as input,
        // read the wake-word probability output, apply threshold.
        _ = frame   // suppress unused warning
    }
}

// MARK: - Private: Permission

private extension WakeWordDetector {

    func requestSpeechPermission() async throws {
        return try await withCheckedThrowingContinuation { continuation in
            SFSpeechRecognizer.requestAuthorization { status in
                switch status {
                case .authorized:
                    continuation.resume()
                case .denied, .restricted, .notDetermined:
                    continuation.resume(throwing: WakeWordDetectorError.speechPermissionDenied)
                @unknown default:
                    continuation.resume(throwing: WakeWordDetectorError.speechPermissionDenied)
                }
            }
        }
    }
}

// MARK: - Private: Audio Helpers

private extension WakeWordDetector {

    /// Converts PCM-16LE Data back into a Float32 AVAudioPCMBuffer for SFSpeech.
    func pcmDataToBuffer(_ data: Data, format: AVAudioFormat) -> AVAudioPCMBuffer? {
        let sampleCount = data.count / MemoryLayout<Int16>.size
        guard sampleCount > 0,
              let buffer = AVAudioPCMBuffer(
                  pcmFormat: format,
                  frameCapacity: AVAudioFrameCount(sampleCount)
              ),
              let floatData = buffer.floatChannelData?[0]
        else { return nil }

        data.withUnsafeBytes { raw in
            guard let int16Ptr = raw.bindMemory(to: Int16.self).baseAddress else { return }
            let scale = Float(1.0) / Float(Int16.max)
            for i in 0 ..< sampleCount {
                floatData[i] = Float(int16Ptr[i]) * scale
            }
        }

        buffer.frameLength = AVAudioFrameCount(sampleCount)
        return buffer
    }

    /// Creates a zero-energy placeholder AudioFrame for the detection event.
    /// The real audio frames are already flowing through AudioCaptureEngine.
    func makeSyntheticFrame() -> AudioFrame {
        let silentSamples = [Int16](repeating: 0, count: 320)
        let data = silentSamples.withUnsafeBytes { Data($0) }
        return AudioFrame(
            pcmData:        data,
            capturedAt:     Date(),
            rmsEnergy:      0,
            sequenceNumber: -1
        )
    }
}
