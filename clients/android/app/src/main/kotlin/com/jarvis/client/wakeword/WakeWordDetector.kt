// WakeWordDetector.kt
// Jarvis Android Client — wakeword/
//
// On-device "Hey Jarvis" wake word detection.
//
// Architecture — two backends, same public API:
//
//   Backend.SPEECH_RECOGNIZER (default):
//     Uses Android SpeechRecognizer with continuous listening.
//     Zero extra dependencies — works on any Android 8+ device.
//     Rolling recognition restarts on each final result so the detector
//     stays active between detections.
//
//   Backend.ON_DEVICE_KEYWORD (future):
//     Placeholder for a TFLite / ML Kit keyword model.
//     Same public API — no call-site changes required.
//
// Threading:
//   SpeechRecognizer must be created and used on a Looper thread (Main).
//   All public methods are safe to call from Main.
//   AudioFrame feeding is also done from the ViewModel on Main after
//   collecting on Dispatchers.Main.

package com.jarvis.client.wakeword

import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.speech.RecognitionListener
import android.speech.RecognizerIntent
import android.speech.SpeechRecognizer
import android.util.Log
import com.jarvis.client.audio.AudioFrame
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*

private const val TAG = "WakeWordDetector"

// ── Public Types ──────────────────────────────────────────────────────────────

enum class WakeWordBackend {
    /** Android SpeechRecognizer — no extra deps. */
    SPEECH_RECOGNIZER,
    /** TFLite keyword model — set modelAssetPath in WakeWordConfiguration. */
    ON_DEVICE_KEYWORD,
}

enum class WakeWordSensitivity {
    LOW,     // fewer false positives
    MEDIUM,  // balanced default
    HIGH;    // more triggers

    /** Minimum SpeechRecognizer confidence to accept (0–1). */
    val confidenceThreshold: Float get() = when (this) {
        LOW    -> 0.85f
        MEDIUM -> 0.70f
        HIGH   -> 0.50f
    }

    /** Minimum probability from an on-device keyword model to trigger. */
    val keywordThreshold: Float get() = when (this) {
        LOW    -> 0.90f
        MEDIUM -> 0.70f
        HIGH   -> 0.50f
    }
}

data class WakeWordConfiguration(
    val wakePhrase:           String              = "hey jarvis",
    val phraseVariants:       List<String>        = listOf("jarvis", "hey jarvis", "okay jarvis"),
    val backend:              WakeWordBackend     = WakeWordBackend.SPEECH_RECOGNIZER,
    val sensitivity:          WakeWordSensitivity = WakeWordSensitivity.MEDIUM,
    val debounceSec:          Double              = 1.5,
    val languageTag:          String              = "en-US",
    /** Asset path for on-device keyword model (backend = ON_DEVICE_KEYWORD). */
    val modelAssetPath:       String?             = null,
) {
    /** All accepted phrases, normalised to lowercase. */
    val acceptedPhrases: List<String> by lazy {
        (listOf(wakePhrase) + phraseVariants).map { it.lowercase().trim() }
    }

    companion object {
        val Default = WakeWordConfiguration()
    }
}

data class WakeWordDetection(
    val matchedPhrase:  String,
    val confidence:     Float,
    val fullTranscript: String,
    val triggerFrame:   AudioFrame?,
    val detectedAt:     Long = System.currentTimeMillis(),
)

sealed class WakeWordError : Exception() {
    data object SpeechRecognizerUnavailable : WakeWordError()
    data object AlreadyRunning              : WakeWordError()
    data object NotRunning                  : WakeWordError()
    class ModelLoadFailed(path: String?, cause: Throwable? = null) : WakeWordError()
}

// ── WakeWordDetector ──────────────────────────────────────────────────────────

/**
 * Listens to AudioCaptureEngine frames and emits [detections] when the
 * wake phrase is recognised.
 *
 * Usage from VoiceViewModel:
 * ```kotlin
 * detector.detections.onEach { detection ->
 *     isStreaming = true
 *     grpcService.sendAudioChunk(detection.triggerFrame, isWakeWordFrame = true)
 * }.launchIn(viewModelScope)
 *
 * detector.start()
 * // In AudioCaptureEngine frame collector:
 * detector.feed(frame)
 * ```
 *
 * Note: [start] and all public methods must be called from the Main thread
 * because Android SpeechRecognizer requires a Main Looper.
 */
class WakeWordDetector(
    private val context: Context,
    val configuration:   WakeWordConfiguration = WakeWordConfiguration.Default,
) {

    // ── State ─────────────────────────────────────────────────────────────────

    private val _isRunning = MutableStateFlow(false)
    val isRunning: StateFlow<Boolean> = _isRunning.asStateFlow()

    private val _lastDetection = MutableStateFlow<WakeWordDetection?>(null)
    val lastDetection: StateFlow<WakeWordDetection?> = _lastDetection.asStateFlow()

    /** Emits one [WakeWordDetection] each time the wake phrase is heard. */
    private val _detections = MutableSharedFlow<WakeWordDetection>(extraBufferCapacity = 4)
    val detections: SharedFlow<WakeWordDetection> = _detections.asSharedFlow()

    /** Emits non-fatal errors (e.g. recogniser restart after silence). */
    private val _errors = MutableSharedFlow<WakeWordError>(extraBufferCapacity = 4)
    val errors: SharedFlow<WakeWordError> = _errors.asSharedFlow()

    // ── Private ───────────────────────────────────────────────────────────────

    private var recognizer:       SpeechRecognizer? = null
    private var lastDetectionTime: Long = Long.MIN_VALUE
    private var lastFrame:         AudioFrame? = null

    // ── Public Interface ──────────────────────────────────────────────────────

    /**
     * Starts the detection pipeline. Must be called on the Main thread.
     */
    fun start() {
        if (_isRunning.value) return

        when (configuration.backend) {
            WakeWordBackend.SPEECH_RECOGNIZER -> startSpeechRecognizer()
            WakeWordBackend.ON_DEVICE_KEYWORD -> startKeywordModel()
        }

        _isRunning.value = true
        Log.i(TAG, "Started — phrase: \"${configuration.wakePhrase}\"")
    }

    /** Stops detection and releases the SpeechRecognizer. */
    fun stop() {
        if (!_isRunning.value) return
        teardownSpeechRecognizer()
        _isRunning.value = false
        Log.i(TAG, "Stopped")
    }

    /**
     * Feed an [AudioFrame] from [AudioCaptureEngine] into the detector.
     * For SPEECH_RECOGNIZER backend, the recogniser processes its own audio
     * internally — this just keeps [lastFrame] current for the detection event.
     * For ON_DEVICE_KEYWORD backend, this is where PCM would be fed to the model.
     */
    fun feed(frame: AudioFrame) {
        if (!_isRunning.value) return
        lastFrame = frame
        if (configuration.backend == WakeWordBackend.ON_DEVICE_KEYWORD) {
            feedKeywordModel(frame)
        }
    }

    // ── Private: SpeechRecognizer backend ─────────────────────────────────────

    private fun startSpeechRecognizer() {
        if (!SpeechRecognizer.isRecognitionAvailable(context)) {
            Log.e(TAG, "SpeechRecognizer unavailable on this device")
            _errors.tryEmit(WakeWordError.SpeechRecognizerUnavailable)
            return
        }

        recognizer = SpeechRecognizer.createSpeechRecognizer(context).apply {
            setRecognitionListener(recognitionListener)
        }
        launchRecognitionRequest()
    }

    private fun launchRecognitionRequest() {
        val intent = Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
            putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL, RecognizerIntent.LANGUAGE_MODEL_FREE_FORM)
            putExtra(RecognizerIntent.EXTRA_LANGUAGE, configuration.languageTag)
            putExtra(RecognizerIntent.EXTRA_PARTIAL_RESULTS, true)
            putExtra(RecognizerIntent.EXTRA_MAX_RESULTS, 1)
            putExtra(RecognizerIntent.EXTRA_PREFER_OFFLINE, true)
            // Hint the recogniser with our accepted phrases for improved accuracy.
            putStringArrayListExtra(
                RecognizerIntent.EXTRA_BIASING_STRINGS,
                ArrayList(configuration.acceptedPhrases),
            )
        }
        recognizer?.startListening(intent)
        Log.d(TAG, "Recognition request started")
    }

    private fun teardownSpeechRecognizer() {
        recognizer?.destroy()
        recognizer = null
    }

    private val recognitionListener = object : RecognitionListener {

        override fun onReadyForSpeech(params: Bundle?) {
            Log.d(TAG, "Ready for speech")
        }

        override fun onPartialResults(partialResults: Bundle?) {
            val results = partialResults
                ?.getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION)
                ?: return
            val transcript = results.firstOrNull()?.lowercase() ?: return
            checkTranscript(transcript, confidence = configuration.sensitivity.confidenceThreshold)
        }

        override fun onResults(results: Bundle?) {
            val texts   = results?.getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION) ?: emptyList()
            val scores  = results?.getFloatArray(SpeechRecognizer.CONFIDENCE_SCORES)

            val transcript = texts.firstOrNull()?.lowercase() ?: ""
            val confidence = scores?.firstOrNull() ?: 0f

            checkTranscript(transcript, confidence)

            // Restart for continuous listening.
            if (_isRunning.value) launchRecognitionRequest()
        }

        override fun onError(error: Int) {
            // Error 7 = ERROR_NO_MATCH (normal silence) — restart quietly.
            // Error 6 = ERROR_SPEECH_TIMEOUT — also normal, restart.
            val isSilence = error == SpeechRecognizer.ERROR_NO_MATCH ||
                            error == SpeechRecognizer.ERROR_SPEECH_TIMEOUT
            if (!isSilence) {
                Log.w(TAG, "SpeechRecognizer error $error — restarting")
            }
            if (_isRunning.value) launchRecognitionRequest()
        }

        override fun onBeginningOfSpeech()   {}
        override fun onRmsChanged(rmsdB: Float) {}
        override fun onBufferReceived(buffer: ByteArray?) {}
        override fun onEndOfSpeech()          {}
        override fun onEvent(eventType: Int, params: Bundle?) {}
    }

    // ── Private: Detection logic ──────────────────────────────────────────────

    private fun checkTranscript(transcript: String, confidence: Float) {
        val matched = configuration.acceptedPhrases.firstOrNull { transcript.contains(it) }
            ?: return

        // Confidence gate.
        if (confidence < configuration.sensitivity.confidenceThreshold) {
            Log.d(TAG, "Wake phrase seen but confidence $confidence < threshold — ignoring")
            return
        }

        // Debounce.
        val now = System.currentTimeMillis()
        val debounceMs = (configuration.debounceSec * 1000).toLong()
        if (now - lastDetectionTime < debounceMs) {
            Log.d(TAG, "Wake phrase debounced")
            return
        }
        lastDetectionTime = now

        Log.i(TAG, "Wake word detected — \"$matched\" (confidence ${"%.2f".format(confidence)})")

        val detection = WakeWordDetection(
            matchedPhrase  = matched,
            confidence     = confidence,
            fullTranscript = transcript,
            triggerFrame   = lastFrame,
            detectedAt     = now,
        )

        _lastDetection.value = detection
        _detections.tryEmit(detection)
    }

    // ── Private: On-device keyword model (Phase 2 stub) ───────────────────────

    private fun startKeywordModel() {
        // Phase 2: load a TFLite or ML Kit keyword model from configuration.modelAssetPath.
        // For now, emit an error so the call-site knows this backend isn't wired.
        Log.w(TAG, "ON_DEVICE_KEYWORD backend not yet implemented")
        _errors.tryEmit(
            WakeWordError.ModelLoadFailed(
                path  = configuration.modelAssetPath,
                cause = UnsupportedOperationException("ON_DEVICE_KEYWORD backend is not yet implemented"),
            )
        )
        // Fall back to SPEECH_RECOGNIZER so the app remains functional.
        startSpeechRecognizer()
    }

    private fun feedKeywordModel(frame: AudioFrame) {
        // Phase 2: pass frame.pcmData to the TFLite inference runner,
        // read the wake probability, apply sensitivity.keywordThreshold.
        @Suppress("UNUSED_VARIABLE") val _ = frame
    }
}
