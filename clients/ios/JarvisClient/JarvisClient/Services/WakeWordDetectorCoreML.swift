// WakeWordDetectorCoreML.swift
// Jarvis iOS Client — clients/ios/JarvisClient/JarvisClient/Services/
//
// CoreML inference engine for Phase 2 wake word detection.
//
// Architecture:
//   This file owns everything needed to run a CoreML wake word model against
//   a 16 kHz PCM-16LE audio stream. WakeWordDetector.swift calls into it via
//   the internal CoreMLEngine type — the public WakeWordDetector API is
//   unchanged.
//
// Model contract:
//   The model must accept a single input named "audioSamples" of shape
//   [1, windowSamples] as a Float32 MLMultiArray, and produce a Float32
//   output named "wakeProbability" (scalar or [1,1] tensor).
//
//   Compatible models:
//     • openWakeWord (https://github.com/dscripka/openWakeWord) exported via
//       the coreml extra: `oww export --format coreml`
//     • Any custom model trained with CreateML AudioClassifier and renamed
//       output to "wakeProbability"
//
// Threading:
//   CoreMLEngine.infer() is called on a dedicated serial DispatchQueue so
//   MLModel prediction never blocks the MainActor or the audio tap thread.
//   Results are hopped back to MainActor before the detection callback fires.

import Foundation
import CoreML
import AVFoundation
import os.log

// MARK: - CoreMLEngineConfiguration

/// Tuning parameters for the CoreML inference loop.
struct CoreMLEngineConfiguration {
    /// Number of 16 kHz samples fed to the model per inference call.
    /// openWakeWord default: 16000 (1 second). Adjust to match your model.
    var windowSamples: Int = 16_000

    /// Number of samples to advance the window each frame (hop size).
    /// 320 = 20 ms at 16 kHz — matches AudioCaptureEngine frame size.
    var hopSamples: Int = 320

    /// Probability output must exceed this threshold to trigger detection.
    /// Overridden at runtime by WakeWordSensitivity.coreMLThreshold.
    var probabilityThreshold: Float = 0.5

    /// Name of the model input feature.
    var inputFeatureName: String = "audioSamples"

    /// Name of the model output feature containing the wake probability.
    var outputFeatureName: String = "wakeProbability"
}

// MARK: - CoreMLEngine

/// Runs a compiled MLModel against a rolling audio window.
/// Thread-safe: infer() dispatches to a private serial queue.
final class CoreMLEngine {

    // MARK: - Public state (read on any thread after init)

    let configuration: CoreMLEngineConfiguration

    // MARK: - Private

    private let model: MLModel
    private let inferenceQueue = DispatchQueue(
        label: "com.jarvis.wakeword.coreml",
        qos: .userInteractive
    )

    /// Rolling PCM-32 sample buffer (Float32, normalised [-1, 1]).
    private var sampleWindow: [Float] = []

    private let log = Logger(subsystem: "com.jarvis.client", category: "CoreMLEngine")

    // MARK: - Init

    /// Compiles and loads the MLModel at the given URL.
    /// Must be called off MainActor (async compilation can take 100–500 ms).
    init(modelURL: URL, configuration: CoreMLEngineConfiguration = .init()) throws {
        // MLModel.compileModel(at:) is synchronous but CPU-bound — call on background.
        let compiledURL = try MLModel.compileModel(at: modelURL)
        let modelConfig = MLModelConfiguration()
        modelConfig.computeUnits = .cpuAndNeuralEngine  // prefer ANE for battery
        self.model = try MLModel(contentsOf: compiledURL, configuration: modelConfig)
        self.configuration = configuration
        // Pre-fill the window with silence so the first real frames have context.
        sampleWindow = [Float](repeating: 0, count: configuration.windowSamples)
        log.info("CoreML model loaded — input '\(configuration.inputFeatureName)' window \(configuration.windowSamples) samples")
    }

    // MARK: - Feed

    /// Appends a PCM-16LE Data frame to the rolling window.
    /// Returns the wake probability if a full window is available, else nil.
    /// Safe to call from any thread; inference runs on inferenceQueue.
    func feed(pcmData: Data, completion: @escaping (Result<Float, Error>) -> Void) {
        // Convert PCM-16LE → Float32 normalised samples inline (no alloc per call).
        let newSamples = pcm16ToFloat32(pcmData)
        guard !newSamples.isEmpty else { return }

        inferenceQueue.async { [weak self] in
            guard let self else { return }

            // Slide the window: drop oldest, append newest.
            let overflow = self.sampleWindow.count + newSamples.count - self.configuration.windowSamples
            if overflow > 0 {
                self.sampleWindow.removeFirst(min(overflow, self.sampleWindow.count))
            }
            self.sampleWindow.append(contentsOf: newSamples)

            // Only infer when we have a full window.
            guard self.sampleWindow.count >= self.configuration.windowSamples else { return }

            do {
                let probability = try self.infer(samples: self.sampleWindow)
                completion(.success(probability))
            } catch {
                completion(.failure(error))
            }
        }
    }

    // MARK: - Private: Inference

    private func infer(samples: [Float]) throws -> Float {
        // Build MLMultiArray input — shape [1, windowSamples].
        let shape: [NSNumber] = [1, NSNumber(value: configuration.windowSamples)]
        let inputArray = try MLMultiArray(shape: shape, dataType: .float32)

        // Copy samples into the MLMultiArray.
        inputArray.withUnsafeMutableBytes { rawPtr, strides in
            guard let floatPtr = rawPtr.bindMemory(to: Float.self).baseAddress else { return }
            for (i, sample) in samples.prefix(configuration.windowSamples).enumerated() {
                floatPtr[i] = sample
            }
        }

        let input = try MLDictionaryFeatureProvider(
            dictionary: [configuration.inputFeatureName: inputArray]
        )
        let output = try model.prediction(from: input)

        return extractProbability(from: output)
    }

    /// Extracts a scalar Float from the model output feature.
    /// Handles both scalar MLMultiArray and direct Double outputs.
    private func extractProbability(from output: MLFeatureProvider) -> Float {
        guard let feature = output.featureValue(for: configuration.outputFeatureName) else {
            log.warning("Output feature '\(self.configuration.outputFeatureName)' not found — returning 0")
            return 0
        }

        switch feature.type {
        case .multiArray:
            // Works for shape [1], [1,1], [1,2] (index 0 = negative, 1 = positive).
            guard let arr = feature.multiArrayValue, arr.count > 0 else { return 0 }
            // If the model outputs [neg, pos] take index 1; else take index 0.
            let idx = arr.count > 1 ? 1 : 0
            return arr[idx].floatValue

        case .double:
            return Float(feature.doubleValue)

        default:
            log.warning("Unexpected output feature type \(feature.type.rawValue)")
            return 0
        }
    }

    // MARK: - Private: PCM Conversion

    /// Converts a PCM-16LE Data blob to Float32 normalised samples.
    private func pcm16ToFloat32(_ data: Data) -> [Float] {
        let count = data.count / MemoryLayout<Int16>.size
        guard count > 0 else { return [] }
        var samples = [Float](repeating: 0, count: count)
        let scale = Float(1.0) / Float(Int16.max)
        data.withUnsafeBytes { rawPtr in
            guard let int16Ptr = rawPtr.bindMemory(to: Int16.self).baseAddress else { return }
            for i in 0 ..< count {
                samples[i] = Float(int16Ptr[i]) * scale
            }
        }
        return samples
    }
}

// MARK: - WakeWordSensitivity + CoreML threshold

extension WakeWordSensitivity {
    /// Probability threshold used by the CoreML backend.
    /// Intentionally separate from SFSpeech confidenceThreshold — the two
    /// scales are not comparable (SFSpeech is ASR confidence; CoreML is
    /// a raw model probability).
    var coreMLThreshold: Float {
        switch self {
        case .low:    return 0.90   // very conservative
        case .medium: return 0.70   // balanced default
        case .high:   return 0.50   // more triggers
        }
    }
}
