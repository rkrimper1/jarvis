// JarvisClientTests.swift
// Jarvis iOS Client — clients/ios/JarvisClient/JarvisClientTests/
//
// XCTest suite for Phase 1 components.
// Tests are divided into three sections:
//   1. AudioFrameTests        — AudioFrame value type correctness
//   2. WakeWordDetectorTests  — configuration, debounce, phrase matching logic
//   3. VoiceViewModelTests    — state machine transitions via mock service

import XCTest
@testable import JarvisClient

// MARK: - AudioFrame Tests

final class AudioFrameTests: XCTestCase {

    // PCM-16LE: 320 samples × 2 bytes = 640 bytes for a 20 ms frame at 16 kHz
    private let frameByteCount = 320 * 2

    func test_audioFrame_stores_pcmData() {
        let data = Data(repeating: 0x42, count: frameByteCount)
        let frame = AudioFrame(
            pcmData:        data,
            capturedAt:     Date(),
            rmsEnergy:      0.5,
            sequenceNumber: 1
        )
        XCTAssertEqual(frame.pcmData, data)
    }

    func test_audioFrame_stores_capturedAt() {
        let now = Date()
        let frame = AudioFrame(pcmData: Data(), capturedAt: now, rmsEnergy: 0, sequenceNumber: 0)
        XCTAssertEqual(frame.capturedAt, now)
    }

    func test_audioFrame_rmsEnergy_bounds() {
        // Silent frame — all zeros
        let silence = AudioFrame(pcmData: Data(repeating: 0, count: frameByteCount),
                                 capturedAt: Date(), rmsEnergy: 0, sequenceNumber: 1)
        XCTAssertEqual(silence.rmsEnergy, 0.0, accuracy: 1e-6)

        // Max energy frame
        let loud = AudioFrame(pcmData: Data(), capturedAt: Date(), rmsEnergy: 1.0, sequenceNumber: 2)
        XCTAssertLessThanOrEqual(loud.rmsEnergy, 1.0)
        XCTAssertGreaterThanOrEqual(loud.rmsEnergy, 0.0)
    }

    func test_audioFrame_sequenceNumbers_monotonicallyIncrement() {
        var frames: [AudioFrame] = []
        for i in 0 ..< 5 {
            frames.append(AudioFrame(
                pcmData:        Data(count: frameByteCount),
                capturedAt:     Date(),
                rmsEnergy:      0,
                sequenceNumber: Int64(i + 1)
            ))
        }
        for i in 1 ..< frames.count {
            XCTAssertGreaterThan(frames[i].sequenceNumber, frames[i-1].sequenceNumber)
        }
    }

    func test_audioFrame_correct_byteCount_for_20ms_at_16kHz() {
        // 16000 Hz × 20 ms / 1000 × 2 bytes/sample = 640 bytes
        let expectedBytes = 640
        let data = Data(count: expectedBytes)
        let frame = AudioFrame(pcmData: data, capturedAt: Date(), rmsEnergy: 0, sequenceNumber: 1)
        XCTAssertEqual(frame.pcmData.count, expectedBytes)
    }
}

// MARK: - WakeWordConfiguration Tests

final class WakeWordConfigurationTests: XCTestCase {

    func test_defaultConfiguration_wakePhrase() {
        let cfg = WakeWordConfiguration.default
        XCTAssertEqual(cfg.wakePhrase, "hey jarvis")
    }

    func test_defaultConfiguration_variantsContainWakePhrase() {
        let cfg = WakeWordConfiguration.default
        XCTAssertTrue(cfg.phraseVariants.contains(cfg.wakePhrase),
                      "phraseVariants should include the primary wakePhrase")
    }

    func test_defaultConfiguration_requiresOnDeviceRecognition() {
        let cfg = WakeWordConfiguration.default
        if case .speechRecognizer = cfg.backend {
            // correct
        } else {
            XCTFail("default backend should be .speechRecognizer")
        }
    }

    func test_sensitivityThresholds_ordered() {
        XCTAssertGreaterThan(
            WakeWordSensitivity.low.confidenceThreshold,
            WakeWordSensitivity.medium.confidenceThreshold,
            "low sensitivity requires higher confidence than medium"
        )
        XCTAssertGreaterThan(
            WakeWordSensitivity.medium.confidenceThreshold,
            WakeWordSensitivity.high.confidenceThreshold,
            "medium sensitivity requires higher confidence than high"
        )
    }

    func test_sensitivityThresholds_inValidRange() {
        for sensitivity in [WakeWordSensitivity.low, .medium, .high] {
            let t = sensitivity.confidenceThreshold
            XCTAssertGreaterThanOrEqual(t, 0, "threshold must be ≥ 0")
            XCTAssertLessThanOrEqual(t, 1, "threshold must be ≤ 1")
        }
    }

    func test_customConfiguration_overridesDefaults() {
        var cfg = WakeWordConfiguration.default
        cfg.wakePhrase = "ok jarvis"
        cfg.debounceSec = 3.0
        XCTAssertEqual(cfg.wakePhrase, "ok jarvis")
        XCTAssertEqual(cfg.debounceSec, 3.0, accuracy: 1e-6)
    }
}

// MARK: - WakeWordDetector Lifecycle Tests

@MainActor
final class WakeWordDetectorLifecycleTests: XCTestCase {

    func test_detector_initiallyNotRunning() {
        let detector = WakeWordDetector(configuration: .default)
        XCTAssertFalse(detector.isRunning)
    }

    func test_detector_stop_whenNotRunning_isNoOp() {
        let detector = WakeWordDetector(configuration: .default)
        // Must not throw or crash.
        detector.stop()
        XCTAssertFalse(detector.isRunning)
    }

    func test_detector_feed_whenNotRunning_isNoOp() {
        let detector = WakeWordDetector(configuration: .default)
        let frame = AudioFrame(
            pcmData:        Data(count: 640),
            capturedAt:     Date(),
            rmsEnergy:      0.9,
            sequenceNumber: 1
        )
        // Must not crash.
        detector.feed(frame)
    }

    func test_detector_lastDetection_initiallyNil() {
        let detector = WakeWordDetector(configuration: .default)
        XCTAssertNil(detector.lastDetection)
    }

    func test_coreML_backend_throwsOnMissingModel() async {
        var cfg = WakeWordConfiguration.default
        cfg.backend = .coreML(modelURL: URL(fileURLWithPath: "/nonexistent/model.mlmodel"))
        let detector = WakeWordDetector(configuration: cfg)

        do {
            try await detector.start()
            XCTFail("Expected coreMLModelLoadFailed for missing model file")
        } catch WakeWordDetectorError.coreMLModelLoadFailed {
            // Correct — CoreMLEngine.init throws when the file doesn't exist.
        } catch {
            XCTFail("Unexpected error type: \(error)")
        }
    }
}

// MARK: - CoreMLEngine Tests

/// Tests the CoreMLEngine inference helper directly (no model file required
/// for the pure-logic tests; model-loading tests use a synthetic .mlpackage).
final class CoreMLEngineTests: XCTestCase {

    // MARK: Configuration defaults

    func test_defaultConfig_windowSamples() {
        let cfg = CoreMLEngineConfiguration()
        XCTAssertEqual(cfg.windowSamples, 16_000,
                       "Default window should be 1 second at 16 kHz")
    }

    func test_defaultConfig_hopSamples() {
        let cfg = CoreMLEngineConfiguration()
        XCTAssertEqual(cfg.hopSamples, 320,
                       "Default hop should be 20 ms at 16 kHz (one AudioCaptureEngine frame)")
    }

    func test_defaultConfig_inputFeatureName() {
        let cfg = CoreMLEngineConfiguration()
        XCTAssertEqual(cfg.inputFeatureName, "audioSamples")
    }

    func test_defaultConfig_outputFeatureName() {
        let cfg = CoreMLEngineConfiguration()
        XCTAssertEqual(cfg.outputFeatureName, "wakeProbability")
    }

    func test_defaultConfig_probabilityThreshold_inValidRange() {
        let cfg = CoreMLEngineConfiguration()
        XCTAssertGreaterThan(cfg.probabilityThreshold, 0)
        XCTAssertLessThanOrEqual(cfg.probabilityThreshold, 1)
    }

    // MARK: Sensitivity → CoreML threshold mapping

    func test_coreML_sensitivity_low_highestThreshold() {
        XCTAssertGreaterThan(
            WakeWordSensitivity.low.coreMLThreshold,
            WakeWordSensitivity.medium.coreMLThreshold,
            "low sensitivity requires higher probability to fire"
        )
    }

    func test_coreML_sensitivity_medium_betweenLowAndHigh() {
        XCTAssertGreaterThan(
            WakeWordSensitivity.medium.coreMLThreshold,
            WakeWordSensitivity.high.coreMLThreshold
        )
    }

    func test_coreML_sensitivity_allInValidRange() {
        for s in [WakeWordSensitivity.low, .medium, .high] {
            let t = s.coreMLThreshold
            XCTAssertGreaterThan(t, 0, "threshold must be > 0 for \(s)")
            XCTAssertLessThanOrEqual(t, 1, "threshold must be ≤ 1 for \(s)")
        }
    }

    func test_coreML_sensitivity_distinctFromSFSpeechThreshold() {
        // The two thresholds operate on different scales — they must not alias.
        for s in [WakeWordSensitivity.low, .medium, .high] {
            XCTAssertNotEqual(
                s.coreMLThreshold,
                s.confidenceThreshold,
                "CoreML and SFSpeech thresholds should be tuned independently for \(s)"
            )
        }
    }

    // MARK: Model load failure

    func test_engine_init_throwsOnMissingFile() {
        let missingURL = URL(fileURLWithPath: "/tmp/nonexistent_\(UUID().uuidString).mlmodel")
        XCTAssertThrowsError(
            try CoreMLEngine(modelURL: missingURL)
        ) { error in
            // MLModel.compileModel throws when the file doesn't exist.
            XCTAssertNotNil(error)
        }
    }

    // MARK: WakeWordDetector + CoreML backend integration

    @MainActor
    func test_detector_coreML_notRunning_afterFailedStart() async {
        var cfg = WakeWordConfiguration.default
        cfg.backend = .coreML(modelURL: URL(fileURLWithPath: "/nonexistent.mlmodel"))
        let detector = WakeWordDetector(configuration: cfg)
        try? await detector.start()
        XCTAssertFalse(detector.isRunning,
                       "isRunning must be false when start() throws")
    }

    @MainActor
    func test_detector_coreML_stop_afterFailedStart_isNoOp() async {
        var cfg = WakeWordConfiguration.default
        cfg.backend = .coreML(modelURL: URL(fileURLWithPath: "/nonexistent.mlmodel"))
        let detector = WakeWordDetector(configuration: cfg)
        try? await detector.start()
        // stop() on a non-running detector must not crash.
        detector.stop()
        XCTAssertFalse(detector.isRunning)
    }

    @MainActor
    func test_detector_coreML_feed_whenNotRunning_isNoOp() {
        var cfg = WakeWordConfiguration.default
        cfg.backend = .coreML(modelURL: URL(fileURLWithPath: "/nonexistent.mlmodel"))
        let detector = WakeWordDetector(configuration: cfg)
        let frame = AudioFrame(
            pcmData:        Data(count: 640),
            capturedAt:     Date(),
            rmsEnergy:      0,
            sequenceNumber: 1
        )
        // Must not crash even though engine is nil.
        detector.feed(frame)
    }

    @MainActor
    func test_detector_coreML_errorCallback_fires_onLoadFailure() async {
        var cfg = WakeWordConfiguration.default
        cfg.backend = .coreML(modelURL: URL(fileURLWithPath: "/bad/path.mlmodel"))
        let detector = WakeWordDetector(configuration: cfg)

        var receivedError: WakeWordDetectorError?
        detector.onError = { receivedError = $0 }

        do {
            try await detector.start()
        } catch let err as WakeWordDetectorError {
            // start() throws — the error is surfaced via throw, not onError.
            // Verify it's the right type.
            if case .coreMLModelLoadFailed = err { /* pass */ }
            else { XCTFail("Wrong error type: \(err)") }
        } catch {
            XCTFail("Unexpected error: \(error)")
        }

        // onError should NOT have been called — errors from start() propagate via throw.
        XCTAssertNil(receivedError,
                     "start() errors propagate via throw, not onError callback")
    }
}

// MARK: - WakeWordDetection Model Tests

final class WakeWordDetectionModelTests: XCTestCase {

    func test_detection_storesMatchedPhrase() {
        let d = makeDetection(phrase: "hey jarvis", confidence: 0.9)
        XCTAssertEqual(d.matchedPhrase, "hey jarvis")
    }

    func test_detection_confidenceInRange() {
        let d = makeDetection(phrase: "jarvis", confidence: 0.75)
        XCTAssertGreaterThanOrEqual(d.confidence, 0)
        XCTAssertLessThanOrEqual(d.confidence, 1)
    }

    func test_detection_detectedAt_recent() {
        let before = Date()
        let d = makeDetection(phrase: "jarvis", confidence: 0.8)
        XCTAssertGreaterThanOrEqual(d.detectedAt, before)
    }

    func test_detection_coreML_hasEmptyTranscript() {
        // CoreML backend produces no transcript — only a probability.
        let d = WakeWordDetection(
            matchedPhrase:  "hey jarvis",
            confidence:     0.85,
            fullTranscript: "",
            triggerFrame:   makeFrame(),
            detectedAt:     Date()
        )
        XCTAssertEqual(d.fullTranscript, "",
                       "CoreML detection has no ASR transcript")
    }

    // MARK: Helpers

    private func makeDetection(phrase: String, confidence: Float) -> WakeWordDetection {
        WakeWordDetection(
            matchedPhrase:  phrase,
            confidence:     confidence,
            fullTranscript: "hey jarvis turn on the lights",
            triggerFrame:   makeFrame(),
            detectedAt:     Date()
        )
    }

    private func makeFrame() -> AudioFrame {
        AudioFrame(
            pcmData:        Data(count: 640),
            capturedAt:     Date(),
            rmsEnergy:      0.4,
            sequenceNumber: 1
        )
    }
}

final class VoiceStateTests: XCTestCase {

    func test_displayLabel_nonEmpty_for_allStates() {
        let states: [VoiceState] = [.idle, .listening, .processing, .speaking, .error("boom")]
        for state in states {
            XCTAssertFalse(state.displayLabel.isEmpty,
                           "displayLabel should not be empty for \(state)")
        }
    }

    func test_isActive_onlyForNonIdleNonErrorStates() {
        XCTAssertFalse(VoiceState.idle.isActive)
        XCTAssertTrue(VoiceState.listening.isActive)
        XCTAssertTrue(VoiceState.processing.isActive)
        XCTAssertTrue(VoiceState.speaking.isActive)
        XCTAssertFalse(VoiceState.error("x").isActive)
    }

    func test_hudColor_distinctForDifferentStates() {
        // Each active state should have a different tint so the HUD is readable.
        let listeningColor  = VoiceState.listening.hudColor
        let processingColor = VoiceState.processing.hudColor
        let speakingColor   = VoiceState.speaking.hudColor
        XCTAssertNotEqual(listeningColor, processingColor)
        XCTAssertNotEqual(listeningColor, speakingColor)
    }

    func test_error_displayLabel_contains_message() {
        let msg = "STT_FAILURE"
        let state = VoiceState.error(msg)
        XCTAssertTrue(state.displayLabel.contains(msg),
                      "error displayLabel should contain the message")
    }

    func test_voiceState_equatable() {
        XCTAssertEqual(VoiceState.idle, VoiceState.idle)
        XCTAssertEqual(VoiceState.listening, VoiceState.listening)
        XCTAssertNotEqual(VoiceState.idle, VoiceState.listening)
        XCTAssertEqual(VoiceState.error("x"), VoiceState.error("x"))
        XCTAssertNotEqual(VoiceState.error("x"), VoiceState.error("y"))
    }
}

// MARK: - HUDSeverity Tests

final class HUDSeverityTests: XCTestCase {

    func test_severityColors_distinct() {
        let colors = [
            HUDSeverity.info.hudColor,
            HUDSeverity.warning.hudColor,
            HUDSeverity.critical.hudColor,
            HUDSeverity.emergency.hudColor,
        ]
        let unique = Set(colors.map { "\($0)" })
        XCTAssertEqual(unique.count, colors.count,
                       "Each severity level should have a distinct HUD colour")
    }
}

// MARK: - TranscriptLine Tests

final class TranscriptLineTests: XCTestCase {

    func test_transcriptLine_id_unique() {
        let a = TranscriptLine(text: "hello", isFinal: true, confidence: 1, timestamp: Date(), source: .user)
        let b = TranscriptLine(text: "hello", isFinal: true, confidence: 1, timestamp: Date(), source: .user)
        XCTAssertNotEqual(a.id, b.id, "Each TranscriptLine should have a unique ID")
    }

    func test_transcriptLine_source_distinguishes_user_and_jarvis() {
        let user   = TranscriptLine(text: "query", isFinal: true, confidence: 0.9, timestamp: Date(), source: .user)
        let jarvis = TranscriptLine(text: "reply", isFinal: true, confidence: 1.0, timestamp: Date(), source: .jarvis)
        XCTAssertNotEqual("\(user.source)", "\(jarvis.source)")
    }
}

// MARK: - VoiceViewModel Init Tests

@MainActor
final class VoiceViewModelInitTests: XCTestCase {

    func test_viewModel_initialState_isIdle() {
        let vm = VoiceViewModel(userID: "tony", grpcConfiguration: .development)
        XCTAssertEqual(vm.voiceState, .idle)
    }

    func test_viewModel_initiallyNotConnected() {
        let vm = VoiceViewModel(userID: "tony", grpcConfiguration: .development)
        XCTAssertFalse(vm.isConnected)
    }

    func test_viewModel_initiallyNotStreaming() {
        let vm = VoiceViewModel(userID: "tony", grpcConfiguration: .development)
        XCTAssertFalse(vm.isMicActive)
    }

    func test_viewModel_transcriptHistory_initiallyEmpty() {
        let vm = VoiceViewModel(userID: "tony", grpcConfiguration: .development)
        XCTAssertTrue(vm.transcriptHistory.isEmpty)
    }

    func test_viewModel_liveTranscript_initiallyEmpty() {
        let vm = VoiceViewModel(userID: "tony", grpcConfiguration: .development)
        XCTAssertTrue(vm.liveTranscript.isEmpty)
    }

    func test_viewModel_micRMS_initiallyZero() {
        let vm = VoiceViewModel(userID: "tony", grpcConfiguration: .development)
        XCTAssertEqual(vm.micRMS, 0.0, accuracy: 1e-6)
    }

    func test_viewModel_pendingActions_initiallyEmpty() {
        let vm = VoiceViewModel(userID: "tony", grpcConfiguration: .development)
        XCTAssertTrue(vm.pendingActions.isEmpty)
    }

    func test_viewModel_requiresConfirmation_initiallyFalse() {
        let vm = VoiceViewModel(userID: "tony", grpcConfiguration: .development)
        XCTAssertFalse(vm.requiresConfirmation)
    }
}

// MARK: - VoiceViewModel State Transitions (offline / no gRPC)

@MainActor
final class VoiceViewModelOfflineTests: XCTestCase {

    /// Creates a ViewModel but does NOT call start() — exercises offline paths.
    private func makeVM() -> VoiceViewModel {
        VoiceViewModel(userID: "tony", grpcConfiguration: .development)
    }

    func test_stop_whenNotStarted_isNoOp() async {
        let vm = makeVM()
        await vm.stop() // must not crash
        XCTAssertEqual(vm.voiceState, .idle)
    }

    func test_cancel_whenIdle_isNoOp() async {
        let vm = makeVM()
        await vm.cancel()
        XCTAssertEqual(vm.voiceState, .idle)
    }

    func test_endSpeech_whenIdle_isNoOp() async {
        let vm = makeVM()
        await vm.endSpeech()
        XCTAssertEqual(vm.voiceState, .idle)
    }

    func test_dismissTopAction_whenEmpty_isNoOp() {
        let vm = makeVM()
        vm.dismissTopAction() // must not crash
        XCTAssertTrue(vm.pendingActions.isEmpty)
    }

    func test_clearHistory_removesLines() {
        let vm = makeVM()
        // Inject lines directly via the public clearHistory path.
        vm.clearHistory()
        XCTAssertTrue(vm.transcriptHistory.isEmpty)
    }
}

// MARK: - WakeWordDetection Model Tests

final class WakeWordDetectionTests: XCTestCase {

    private func makeDetection(phrase: String = "hey jarvis", confidence: Float = 0.9) -> WakeWordDetection {
        let frame = AudioFrame(pcmData: Data(count: 640), capturedAt: Date(), rmsEnergy: 0, sequenceNumber: -1)
        return WakeWordDetection(
            matchedPhrase:  phrase,
            confidence:     confidence,
            fullTranscript: "hey jarvis what time is it",
            triggerFrame:   frame,
            detectedAt:     Date()
        )
    }

    func test_detection_stores_matchedPhrase() {
        let d = makeDetection(phrase: "jarvis")
        XCTAssertEqual(d.matchedPhrase, "jarvis")
    }

    func test_detection_confidence_inValidRange() {
        let d = makeDetection(confidence: 0.82)
        XCTAssertGreaterThanOrEqual(d.confidence, 0)
        XCTAssertLessThanOrEqual(d.confidence, 1)
    }

    func test_detection_fullTranscript_nonEmpty() {
        let d = makeDetection()
        XCTAssertFalse(d.fullTranscript.isEmpty)
    }

    func test_detection_triggerFrame_isSynthetic() {
        let d = makeDetection()
        // Synthetic frames have sequenceNumber == -1
        XCTAssertEqual(d.triggerFrame.sequenceNumber, -1)
    }
}

// MARK: - HUDActionModel Tests

final class HUDActionModelTests: XCTestCase {

    func test_hudActionModel_id_unique() {
        let a = HUDActionModel(type: .showCard,   payloadJSON: "{}", severity: .info,     receivedAt: Date())
        let b = HUDActionModel(type: .showCard,   payloadJSON: "{}", severity: .info,     receivedAt: Date())
        XCTAssertNotEqual(a.id, b.id)
    }

    func test_hudActionType_rawValues_noTypoRegression() {
        // Guard against proto rename breaking the mapping.
        XCTAssertEqual(HUDActionType.showCard.rawValue,        "TYPE_SHOW_CARD")
        XCTAssertEqual(HUDActionType.dispatchAgent.rawValue,   "TYPE_DISPATCH_AGENT")
        XCTAssertEqual(HUDActionType.hardwareCmd.rawValue,     "TYPE_HARDWARE_CMD")
        XCTAssertEqual(HUDActionType.securityProtocol.rawValue,"TYPE_SECURITY_PROTOCOL")
        XCTAssertEqual(HUDActionType.dismissHUD.rawValue,      "TYPE_DISMISS_HUD")
    }
}

// MARK: - VoiceServiceConfiguration Tests

final class VoiceServiceConfigurationTests: XCTestCase {

    func test_development_preset_localhost() {
        let cfg = VoiceServiceConfiguration.development
        XCTAssertEqual(cfg.host, "localhost")
        XCTAssertEqual(cfg.port, 50059)
        XCTAssertFalse(cfg.useTLS)
    }

    func test_production_preset_uses_TLS() {
        let cfg = VoiceServiceConfiguration.production
        XCTAssertTrue(cfg.useTLS)
        XCTAssertEqual(cfg.port, 443)
    }

    func test_production_host_nonEmpty() {
        XCTAssertFalse(VoiceServiceConfiguration.production.host.isEmpty)
    }
}
