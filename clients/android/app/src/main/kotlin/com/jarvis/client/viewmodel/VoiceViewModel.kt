// VoiceViewModel.kt
// Jarvis Android Client — viewmodel/
//
// Single source of truth for all voice interaction state.
// Owns and coordinates:
//   AudioCaptureEngine → microphone frames
//   WakeWordDetector   → "Hey Jarvis" trigger
//   GRPCVoiceService   → bidirectional backend stream
//   TtsPlayer          → AudioTrack playback of TTS audio chunks
//
// State machine:
//   idle ──[wake word]──▶ listening ──[end of speech / VAD]──▶ processing
//     ▲                                                              │
//     └────────────── idle ◀── speaking ◀───────────────────────────┘
//
// Compose UI observes StateFlow / SharedFlow properties directly.

package com.jarvis.client.viewmodel

import android.app.Application
import android.content.pm.PackageManager
import androidx.core.content.ContextCompat
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.jarvis.client.audio.AudioCaptureEngine
import com.jarvis.client.audio.AudioFrame
import com.jarvis.client.grpc.*
import com.jarvis.client.wakeword.WakeWordDetector
import com.jarvis.client.wakeword.WakeWordConfiguration
import jarvis.voice.Voice.HUDAction
import jarvis.voice.Voice.StatusEvent
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import android.util.Log
import android.Manifest

private const val TAG = "VoiceViewModel"

// ── Voice State ───────────────────────────────────────────────────────────────

enum class VoiceState {
    IDLE, LISTENING, PROCESSING, SPEAKING, ERROR;

    val isActive: Boolean get() = this == LISTENING || this == PROCESSING || this == SPEAKING

    val displayLabel: String get() = when (this) {
        IDLE       -> "Standby"
        LISTENING  -> "Listening…"
        PROCESSING -> "Processing…"
        SPEAKING   -> "Speaking…"
        ERROR      -> "Error"
    }
}

// ── HUD Action Models ─────────────────────────────────────────────────────────

enum class HudActionType(val protoValue: Int) {
    UNKNOWN(0),
    OPEN_APP(1),
    SHOW_CARD(2),
    SET_TIMER(3),
    NAVIGATE(4),
    DISMISS_HUD(5),
    DISPATCH_AGENT(6),
    HARDWARE_CMD(7),
    SECURITY_PROTOCOL(8);

    companion object {
        fun from(value: Int) = entries.firstOrNull { it.protoValue == value } ?: UNKNOWN
    }
}

enum class HudSeverity { INFO, WARNING, CRITICAL, EMERGENCY }

data class HudActionModel(
    val id:          String = java.util.UUID.randomUUID().toString(),
    val type:        HudActionType,
    val payloadJson: String,
    val severity:    HudSeverity,
    val receivedAt:  Long = System.currentTimeMillis(),
)

// ── Transcript ────────────────────────────────────────────────────────────────

enum class TranscriptSource { USER, JARVIS }

data class TranscriptLine(
    val id:         String = java.util.UUID.randomUUID().toString(),
    val text:       String,
    val isFinal:    Boolean,
    val confidence: Float,
    val timestamp:  Long = System.currentTimeMillis(),
    val source:     TranscriptSource,
)

// ── VoiceViewModel ────────────────────────────────────────────────────────────

class VoiceViewModel(
    application: Application,
    private val grpcConfig:  VoiceServiceConfiguration = VoiceServiceConfiguration.Development,
    private val wakeConfig:  WakeWordConfiguration     = WakeWordConfiguration.Default,
) : AndroidViewModel(application) {

    // ── Published State ───────────────────────────────────────────────────────

    private val _voiceState = MutableStateFlow(VoiceState.IDLE)
    val voiceState: StateFlow<VoiceState> = _voiceState.asStateFlow()

    private val _isConnected = MutableStateFlow(false)
    val isConnected: StateFlow<Boolean> = _isConnected.asStateFlow()

    private val _isMicActive = MutableStateFlow(false)
    val isMicActive: StateFlow<Boolean> = _isMicActive.asStateFlow()

    private val _micRms = MutableStateFlow(0f)
    val micRms: StateFlow<Float> = _micRms.asStateFlow()

    private val _liveTranscript = MutableStateFlow("")
    val liveTranscript: StateFlow<String> = _liveTranscript.asStateFlow()

    private val _transcriptHistory = MutableStateFlow<List<TranscriptLine>>(emptyList())
    val transcriptHistory: StateFlow<List<TranscriptLine>> = _transcriptHistory.asStateFlow()

    private val _lastReply = MutableStateFlow("")
    val lastReply: StateFlow<String> = _lastReply.asStateFlow()

    private val _lastIntent = MutableStateFlow("")
    val lastIntent: StateFlow<String> = _lastIntent.asStateFlow()

    private val _requiresConfirmation = MutableStateFlow(false)
    val requiresConfirmation: StateFlow<Boolean> = _requiresConfirmation.asStateFlow()

    private val _pendingActions = MutableStateFlow<List<HudActionModel>>(emptyList())
    val pendingActions: StateFlow<List<HudActionModel>> = _pendingActions.asStateFlow()

    private val _lastErrorMessage = MutableStateFlow<String?>(null)
    val lastErrorMessage: StateFlow<String?> = _lastErrorMessage.asStateFlow()

    private val _hasMicPermission = MutableStateFlow(false)
    val hasMicPermission: StateFlow<Boolean> = _hasMicPermission.asStateFlow()

    // ── Internal State ────────────────────────────────────────────────────────

    private var isStreaming = false
    private var ttsBuffer   = ByteArray(0)

    // ── Dependencies ──────────────────────────────────────────────────────────

    private val grpcService     = GRPCVoiceService(grpcConfig, viewModelScope)
    private val audioEngine     = AudioCaptureEngine(viewModelScope)
    private val wakeWordDetector = WakeWordDetector(application, wakeConfig)
    private val ttsPlayer       = TtsPlayer()

    // User / session IDs — set before connecting.
    private var userId:    String = ""
    private var sessionId: String = java.util.UUID.randomUUID().toString()

    // ── Init ──────────────────────────────────────────────────────────────────

    init {
        checkMicPermission()
        wireFlows()
    }

    // ── Public Interface ──────────────────────────────────────────────────────

    /** Call with a user ID before start(). */
    fun setUser(id: String) { userId = id }

    /** Checks/updates RECORD_AUDIO permission state. */
    fun checkMicPermission() {
        val ctx = getApplication<Application>()
        _hasMicPermission.value = ContextCompat.checkSelfPermission(
            ctx, Manifest.permission.RECORD_AUDIO
        ) == PackageManager.PERMISSION_GRANTED
    }

    /** Starts the full pipeline: wake word → mic → gRPC stream. */
    fun start() {
        if (!_hasMicPermission.value) {
            handleError("Microphone permission not granted")
            return
        }
        viewModelScope.launch {
            try {
                wakeWordDetector.start()

                audioEngine.start()
                _isMicActive.value = true

                grpcService.connect(userId, sessionId)

                _voiceState.value = VoiceState.IDLE
                Log.i(TAG, "Pipeline ready — waiting for wake word")
            } catch (e: Exception) {
                handleError("Start failed: ${e.message}")
            }
        }
    }

    /** Stops the pipeline gracefully. */
    fun stop() {
        viewModelScope.launch {
            wakeWordDetector.stop()
            audioEngine.stop()
            grpcService.disconnect()

            isStreaming           = false
            _isMicActive.value    = false
            _isConnected.value    = false
            _voiceState.value     = VoiceState.IDLE
            _micRms.value         = 0f
            _liveTranscript.value = ""
            ttsPlayer.stop()
            Log.i(TAG, "Pipeline stopped")
        }
    }

    /** Manually signals end-of-speech (e.g. push-to-talk button release). */
    fun endSpeech() {
        if (!isStreaming) return
        viewModelScope.launch {
            isStreaming = false
            grpcService.sendEndOfSpeech()
        }
    }

    /** Cancels the current in-flight pipeline. */
    fun cancel() {
        isStreaming = false
        _voiceState.value = VoiceState.IDLE
        _liveTranscript.value = ""
        viewModelScope.launch { grpcService.sendCancel() }
    }

    /** Confirms a pending action when requiresConfirmation == true. */
    fun confirm() {
        _requiresConfirmation.value = false
        viewModelScope.launch { grpcService.sendNewTurn() }
    }

    /** Dismisses the most recent pending HUD action. */
    fun dismissTopAction() {
        val current = _pendingActions.value
        if (current.isNotEmpty()) {
            _pendingActions.value = current.drop(1)
        }
    }

    /** Clears transcript history. */
    fun clearHistory() { _transcriptHistory.value = emptyList() }

    // ── Private: Flow Wiring ──────────────────────────────────────────────────

    private fun wireFlows() {
        wireAudioEngine()
        wireWakeWordDetector()
        wireGrpcEvents()
        wireGrpcConnected()
    }

    private fun wireAudioEngine() {
        // Collect frames on Main — update RMS and forward to wake word + gRPC.
        audioEngine.frames
            .onEach { frame ->
                _micRms.value = frame.rmsEnergy
                wakeWordDetector.feed(frame)
                if (isStreaming) {
                    grpcService.sendAudioChunk(frame, isWakeWordFrame = false)
                }
            }
            .launchIn(viewModelScope)

        // RMS passthrough from engine's own state.
        audioEngine.currentRms
            .onEach { _micRms.value = it }
            .launchIn(viewModelScope)
    }

    private fun wireWakeWordDetector() {
        wakeWordDetector.detections
            .onEach { detection ->
                if (_voiceState.value != VoiceState.IDLE) return@onEach

                Log.i(TAG, "Wake word: \"${detection.matchedPhrase}\" — opening stream")
                isStreaming             = true
                _liveTranscript.value  = ""
                _voiceState.value      = VoiceState.LISTENING
                ttsPlayer.stop()

                // Send the trigger frame as a wake word frame — server skips VAD.
                detection.triggerFrame?.let { frame ->
                    grpcService.sendAudioChunk(frame, isWakeWordFrame = true)
                }
            }
            .launchIn(viewModelScope)

        wakeWordDetector.errors
            .onEach { Log.w(TAG, "WakeWordDetector error: $it") }
            .launchIn(viewModelScope)
    }

    private fun wireGrpcEvents() {
        grpcService.events
            .onEach { event -> handleVoiceEvent(event) }
            .launchIn(viewModelScope)
    }

    private fun wireGrpcConnected() {
        grpcService.isConnected
            .onEach { _isConnected.value = it }
            .launchIn(viewModelScope)
    }

    // ── Private: VoiceEvent Handler ───────────────────────────────────────────

    private fun handleVoiceEvent(event: VoiceEvent) {
        when (event) {
            is VoiceEvent.StatusChanged      -> handleStatusChange(event.state, event.message)
            is VoiceEvent.TranscriptReceived -> handleTranscript(event.text, event.isFinal, event.confidence)
            is VoiceEvent.ReplyReceived      -> handleReply(event.replyText, event.intent, event.requiresConfirmation)
            is VoiceEvent.AudioReplyReceived -> handleAudioReply(event.data, event.text, event.isFinalChunk)
            is VoiceEvent.HudActionReceived  -> handleHudAction(event.action)
            is VoiceEvent.StreamError        -> handleError(event.error.toString())
            is VoiceEvent.StreamClosed       -> {
                _voiceState.value = VoiceState.IDLE
                isStreaming = false
                Log.i(TAG, "Stream closed")
            }
        }
    }

    private fun handleStatusChange(state: StatusEvent.State, message: String) {
        Log.d(TAG, "Status → $state")
        when (state) {
            StatusEvent.State.STATE_IDLE -> {
                _voiceState.value = VoiceState.IDLE
                isStreaming = false
                audioEngine.resume()
            }
            StatusEvent.State.STATE_LISTENING -> {
                _voiceState.value = VoiceState.LISTENING
            }
            StatusEvent.State.STATE_PROCESSING -> {
                _voiceState.value     = VoiceState.PROCESSING
                isStreaming           = false
                _liveTranscript.value = ""
            }
            StatusEvent.State.STATE_SPEAKING -> {
                _voiceState.value = VoiceState.SPEAKING
                // Pause mic during TTS to avoid capturing our own audio.
                audioEngine.pause()
            }
            StatusEvent.State.STATE_ERROR -> {
                _voiceState.value    = VoiceState.ERROR
                isStreaming          = false
                _lastErrorMessage.value = message.ifBlank { "Unknown error" }
            }
            StatusEvent.State.STATE_ENDED -> {
                _voiceState.value = VoiceState.IDLE
                isStreaming = false
            }
            else -> Unit
        }
    }

    private fun handleTranscript(text: String, isFinal: Boolean, confidence: Float) {
        _liveTranscript.value = if (isFinal) "" else text

        if (isFinal && text.isNotBlank()) {
            appendTranscript(TranscriptLine(
                text       = text,
                isFinal    = true,
                confidence = confidence,
                source     = TranscriptSource.USER,
            ))
            Log.i(TAG, "Final transcript: \"$text\" (conf: ${"%.2f".format(confidence)})")
        }
    }

    private fun handleReply(text: String, intent: String, requiresConfirmation: Boolean) {
        _lastReply.value               = text
        _lastIntent.value              = intent
        _requiresConfirmation.value    = requiresConfirmation

        appendTranscript(TranscriptLine(
            text       = text,
            isFinal    = true,
            confidence = 1f,
            source     = TranscriptSource.JARVIS,
        ))
        Log.i(TAG, "Reply: \"$text\" intent=$intent confirm=$requiresConfirmation")
    }

    private fun handleAudioReply(data: ByteArray, text: String, isFinalChunk: Boolean) {
        ttsBuffer += data
        if (isFinalChunk) {
            val audio = ttsBuffer
            ttsBuffer = ByteArray(0)
            ttsPlayer.play(audio)
            Log.d(TAG, "TTS playback started — ${audio.size} bytes")
        }
    }

    private fun handleHudAction(proto: HUDAction) {
        val type     = HudActionType.from(proto.type.number)
        val severity = mapSeverity(proto.severity.number)

        val model = HudActionModel(
            type        = type,
            payloadJson = proto.payloadJson,
            severity    = severity,
        )

        _pendingActions.value = listOf(model) + _pendingActions.value

        // Auto-dismiss non-critical actions after 8 seconds.
        if (severity != HudSeverity.CRITICAL && severity != HudSeverity.EMERGENCY) {
            viewModelScope.launch {
                delay(8_000)
                _pendingActions.value = _pendingActions.value.filterNot { it.id == model.id }
            }
        }

        if (type == HudActionType.DISMISS_HUD) {
            _pendingActions.value = emptyList()
        }

        Log.i(TAG, "HUD action: $type severity=$severity")
    }

    private fun handleError(message: String) {
        Log.e(TAG, "VoiceViewModel error: $message")
        _voiceState.value       = VoiceState.ERROR
        _lastErrorMessage.value = message
        isStreaming             = false
    }

    // ── Private: Helpers ──────────────────────────────────────────────────────

    private fun appendTranscript(line: TranscriptLine) {
        val current = _transcriptHistory.value.toMutableList()
        current.add(line)
        if (current.size > 200) current.removeAt(0)
        _transcriptHistory.value = current
    }

    private fun mapSeverity(protoSeverity: Int): HudSeverity = when (protoSeverity) {
        1    -> HudSeverity.INFO
        2    -> HudSeverity.WARNING
        3    -> HudSeverity.CRITICAL
        4    -> HudSeverity.EMERGENCY
        else -> HudSeverity.INFO
    }

    override fun onCleared() {
        super.onCleared()
        stop()
        ttsPlayer.release()
    }
}

// ── TtsPlayer ─────────────────────────────────────────────────────────────────

/**
 * Lightweight AudioTrack wrapper for TTS AudioReply payloads.
 * PCM-16LE at 16 kHz mono — matches the server TTS output format.
 */
private class TtsPlayer {

    private var track: android.media.AudioTrack? = null

    fun play(pcmData: ByteArray) {
        stop()
        if (pcmData.isEmpty()) return

        val minBuf = android.media.AudioTrack.getMinBufferSize(
            16_000,
            android.media.AudioFormat.CHANNEL_OUT_MONO,
            android.media.AudioFormat.ENCODING_PCM_16BIT,
        )
        val bufSize = maxOf(minBuf, pcmData.size)

        track = android.media.AudioTrack(
            android.media.AudioManager.STREAM_MUSIC,
            16_000,
            android.media.AudioFormat.CHANNEL_OUT_MONO,
            android.media.AudioFormat.ENCODING_PCM_16BIT,
            bufSize,
            android.media.AudioTrack.MODE_STATIC,
        ).also { t ->
            t.write(pcmData, 0, pcmData.size)
            t.play()
        }
    }

    fun stop() {
        track?.let { t ->
            if (t.playState == android.media.AudioTrack.PLAYSTATE_PLAYING) t.stop()
            t.flush()
            t.release()
        }
        track = null
    }

    fun release() = stop()
}
