// GRPCVoiceService.kt
// Jarvis Android Client — grpc/
//
// Manages the full lifecycle of the VoiceService.Converse bidirectional gRPC
// stream. Single point of contact between the Android app and the Jarvis
// voice backend — AudioCaptureEngine feeds audio in, VoiceViewModel
// consumes events out via StateFlow / SharedFlow.
//
// Threading:
//   connect/disconnect/send* must be called from a coroutine (any dispatcher).
//   gRPC callbacks arrive on gRPC executor threads and are forwarded via flows.
//   VoiceViewModel collects on Dispatchers.Main.
//
// Reconnect strategy:
//   Exponential backoff, capped at maxReconnectAttempts. The original
//   userID + sessionID are preserved so the server session store can stitch
//   the stream back together (especially valuable with the Redis backend).

package com.jarvis.client.grpc

import android.os.Build
import android.util.Log
import com.jarvis.client.audio.AudioFrame
import io.grpc.ManagedChannel
import io.grpc.ManagedChannelBuilder
import io.grpc.Status
import io.grpc.stub.StreamObserver
import jarvis.voice.Voice.*
import jarvis.common.Common.*
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import java.util.UUID
import java.util.concurrent.TimeUnit
import kotlin.math.pow

private const val TAG = "GRPCVoiceService"

// ── Public Event Types ────────────────────────────────────────────────────────

/** All downstream events the app can receive from the Jarvis voice backend. */
sealed class VoiceEvent {
    data class StatusChanged(val state: StatusEvent.State, val message: String) : VoiceEvent()
    data class TranscriptReceived(val text: String, val isFinal: Boolean, val confidence: Float) : VoiceEvent()
    data class ReplyReceived(val replyText: String, val intent: String, val requiresConfirmation: Boolean) : VoiceEvent()
    data class AudioReplyReceived(val data: ByteArray, val text: String, val isFinalChunk: Boolean) : VoiceEvent() {
        override fun equals(other: Any?) = other is AudioReplyReceived && data.contentEquals(other.data)
        override fun hashCode() = data.contentHashCode()
    }
    data class HudActionReceived(val action: HUDAction) : VoiceEvent()
    data class StreamError(val error: VoiceServiceError) : VoiceEvent()
    data object StreamClosed : VoiceEvent()
}

sealed class VoiceServiceError : Exception() {
    data object NotConnected : VoiceServiceError()
    data object SessionCapacityExceeded : VoiceServiceError()
    class StreamSetupFailed(cause: Throwable) : VoiceServiceError()
    class SendFailed(cause: Throwable) : VoiceServiceError()
    data class ReconnectLimitReached(val attempts: Int) : VoiceServiceError()
    data class InvalidConfiguration(val detail: String) : VoiceServiceError()
}

// ── Configuration ─────────────────────────────────────────────────────────────

data class VoiceServiceConfiguration(
    val host:                 String  = "localhost",
    val port:                 Int     = 50059,
    val useTls:               Boolean = false,
    val languageCode:         String  = "en-US",
    val voiceId:              String  = "",
    val contextTags:          List<String> = emptyList(),
    val maxReconnectAttempts: Int     = 5,
    val reconnectBaseDelaySec: Double = 0.5,
    val appVersion:           String  = "1.0.0",
) {
    companion object {
        val Development = VoiceServiceConfiguration()
        val Production  = VoiceServiceConfiguration(
            host                  = "voice.jarvis.yourdomain.com",
            port                  = 443,
            useTls                = true,
            maxReconnectAttempts  = 8,
            reconnectBaseDelaySec = 1.0,
        )
    }
}

// ── GRPCVoiceService ──────────────────────────────────────────────────────────

class GRPCVoiceService(
    private val config: VoiceServiceConfiguration,
    private val scope:  CoroutineScope,
) {

    // ── State ─────────────────────────────────────────────────────────────────

    private val _isConnected = MutableStateFlow(false)
    val isConnected: StateFlow<Boolean> = _isConnected.asStateFlow()

    private val _currentState = MutableStateFlow(StatusEvent.State.STATE_IDLE)
    val currentState: StateFlow<StatusEvent.State> = _currentState.asStateFlow()

    private val _reconnectAttempt = MutableStateFlow(0)
    val reconnectAttempt: StateFlow<Int> = _reconnectAttempt.asStateFlow()

    /** Hot flow of all downstream VoiceEvent messages. */
    private val _events = MutableSharedFlow<VoiceEvent>(extraBufferCapacity = 32)
    val events: SharedFlow<VoiceEvent> = _events.asSharedFlow()

    // ── Private ───────────────────────────────────────────────────────────────

    private var channel:        ManagedChannel? = null
    private var requestStream:  StreamObserver<ConverseRequest>? = null
    private var sessionId:      String = ""
    private var userId:         String = ""
    private var sequenceNum:    Long   = 0L

    @Volatile private var isConnecting = false

    // ── Public Interface ──────────────────────────────────────────────────────

    /** Opens the gRPC channel and sends the StreamConfig first message. */
    suspend fun connect(userId: String, sessionId: String) {
        if (isConnecting || _isConnected.value) return
        isConnecting = true
        try {
            this.userId    = userId
            this.sessionId = sessionId
            this.sequenceNum = 0

            openChannelAndStream()
            _isConnected.value    = true
            _reconnectAttempt.value = 0
            Log.i(TAG, "Connected — session: $sessionId")
        } catch (e: Exception) {
            Log.e(TAG, "Connection failed: ${e.message}")
            emit(VoiceEvent.StreamError(VoiceServiceError.StreamSetupFailed(e)))
        } finally {
            isConnecting = false
        }
    }

    /** Sends a raw PCM audio chunk upstream. */
    suspend fun sendAudioChunk(frame: AudioFrame, isWakeWordFrame: Boolean = false) {
        sequenceNum++
        val chunk = audioChunk {
            data            = com.google.protobuf.ByteString.copyFrom(frame.pcmData)
            sequenceNum     = this@GRPCVoiceService.sequenceNum
            capturedAtMs    = frame.capturedAtMs
            isWakeWordFrame = isWakeWordFrame
        }
        send(converseRequest { audio = chunk })
    }

    /** Signals end-of-speech — triggers STT → NLP → TTS on the server. */
    suspend fun sendEndOfSpeech()  = sendControlEvent(ControlEvent.Type.TYPE_END_OF_SPEECH)

    /** Cancels the current in-flight pipeline without closing the stream. */
    suspend fun sendCancel()       = sendControlEvent(ControlEvent.Type.TYPE_CANCEL)

    /** Sends a keep-alive ping to prevent idle stream timeout. */
    suspend fun sendKeepAlive()    = sendControlEvent(ControlEvent.Type.TYPE_KEEP_ALIVE)

    /** Signals the start of a new conversation turn. */
    suspend fun sendNewTurn()      = sendControlEvent(ControlEvent.Type.TYPE_NEW_TURN)

    /** Gracefully closes the stream and channel. */
    suspend fun disconnect() {
        Log.i(TAG, "Disconnecting — session: $sessionId")
        teardown()
        emit(VoiceEvent.StreamClosed)
    }

    // ── Private: Channel + Stream Setup ──────────────────────────────────────

    private fun openChannelAndStream() {
        val ch = buildChannel()
        channel = ch

        // Response observer — dispatches incoming messages to [_events].
        val responseObserver = object : StreamObserver<ConverseResponse> {
            override fun onNext(response: ConverseResponse) {
                handleResponse(response)
            }
            override fun onError(t: Throwable) {
                val grpcStatus = Status.fromThrowable(t)
                Log.e(TAG, "Stream error: ${grpcStatus.code} — ${grpcStatus.description}")
                _isConnected.value = false
                emit(VoiceEvent.StreamError(VoiceServiceError.StreamSetupFailed(t)))
                scope.launch { attemptReconnect() }
            }
            override fun onCompleted() {
                Log.i(TAG, "Stream completed by server")
                _isConnected.value = false
                emit(VoiceEvent.StreamClosed)
            }
        }

        // Bidirectional streaming requires the Java async stub — gRPC-Kotlin's
        // coroutine stub exposes server/client streaming separately, not bidi.
        val asyncStub = jarvis.voice.VoiceServiceGrpc.newStub(ch)
        requestStream = asyncStub.converse(responseObserver)

        // Send StreamConfig as the very first message.
        requestStream?.onNext(converseRequest { config = makeStreamConfig() })
            ?: throw IllegalStateException("requestStream is null after creation")
    }

    private fun buildChannel(): ManagedChannel {
        val builder = if (config.useTls) {
            ManagedChannelBuilder.forAddress(config.host, config.port)
        } else {
            ManagedChannelBuilder.forAddress(config.host, config.port)
                .usePlaintext()
        }
        return builder
            .keepAliveTime(30, TimeUnit.SECONDS)
            .keepAliveTimeout(10, TimeUnit.SECONDS)
            .build()
    }

    private fun makeStreamConfig(): StreamConfig = streamConfig {
        meta = requestMeta {
            requestId = UUID.randomUUID().toString()
            userId    = this@GRPCVoiceService.userId
            sessionId = this@GRPCVoiceService.sessionId
            source    = "voice"
        }
        clientInfo = clientInfo {
            platform    = "android"
            appVersion  = config.appVersion
            deviceModel = "${Build.MANUFACTURER} ${Build.MODEL}"
            osVersion   = Build.VERSION.RELEASE
        }
        audioConfig = audioConfig {
            encoding       = AudioEncoding.AUDIO_ENCODING_PCM_16BIT
            sampleRateHz   = 16_000
            channelCount   = 1
            frameDurationMs = 20
        }
        languageCode = config.languageCode
        voiceId      = config.voiceId
        contextTags.addAll(config.contextTags)
    }

    // ── Private: Response Handling ─────────────────────────────────────────────

    private fun handleResponse(response: ConverseResponse) {
        when (response.payloadCase) {
            ConverseResponse.PayloadCase.STATUS -> {
                val s = response.status
                _currentState.value = s.state
                emit(VoiceEvent.StatusChanged(s.state, s.message))
            }
            ConverseResponse.PayloadCase.TRANSCRIPT -> {
                val t = response.transcript
                emit(VoiceEvent.TranscriptReceived(t.text, t.isFinal, t.confidence))
            }
            ConverseResponse.PayloadCase.REPLY -> {
                val r = response.reply
                emit(VoiceEvent.ReplyReceived(r.replyText, r.intent, r.requiresConfirmation))
            }
            ConverseResponse.PayloadCase.AUDIO_REPLY -> {
                val a = response.audioReply
                emit(VoiceEvent.AudioReplyReceived(a.data.toByteArray(), a.text, a.isFinalChunk))
            }
            ConverseResponse.PayloadCase.ACTION -> {
                emit(VoiceEvent.HudActionReceived(response.action))
            }
            else -> Log.w(TAG, "Received ConverseResponse with no payload")
        }
    }

    // ── Private: Send Helpers ─────────────────────────────────────────────────

    private suspend fun send(request: ConverseRequest) {
        val stream = requestStream
        if (stream == null) {
            emit(VoiceEvent.StreamError(VoiceServiceError.NotConnected))
            return
        }
        try {
            withContext(Dispatchers.IO) { stream.onNext(request) }
        } catch (e: Exception) {
            Log.e(TAG, "sendMessage failed: ${e.message}")
            emit(VoiceEvent.StreamError(VoiceServiceError.SendFailed(e)))
        }
    }

    private suspend fun sendControlEvent(type: ControlEvent.Type) {
        send(converseRequest {
            event = controlEvent { this.type = type }
        })
    }

    // ── Private: Reconnect ────────────────────────────────────────────────────

    private suspend fun attemptReconnect() {
        val attempt = _reconnectAttempt.value + 1
        if (attempt > config.maxReconnectAttempts) {
            Log.e(TAG, "Reconnect limit reached (${config.maxReconnectAttempts} attempts)")
            emit(VoiceEvent.StreamError(VoiceServiceError.ReconnectLimitReached(config.maxReconnectAttempts)))
            return
        }
        _reconnectAttempt.value = attempt

        val delayMs = (config.reconnectBaseDelaySec * 1_000 * 2.0.pow(attempt - 1)).toLong()
        Log.i(TAG, "Reconnect attempt $attempt in ${delayMs}ms")
        delay(delayMs)

        teardown()
        connect(userId, sessionId)
    }

    // ── Private: Teardown ─────────────────────────────────────────────────────

    private suspend fun teardown() {
        withContext(Dispatchers.IO) {
            try { requestStream?.onCompleted() } catch (_: Exception) {}
            requestStream = null
            try {
                channel?.shutdown()
                channel?.awaitTermination(3, TimeUnit.SECONDS)
            } catch (_: Exception) {}
            channel = null
        }
        _isConnected.value = false
    }

    // ── Private: Flow helper ─────────────────────────────────────────────────

    private fun emit(event: VoiceEvent) {
        _events.tryEmit(event)
    }
}

// ── Protobuf DSL builders ─────────────────────────────────────────────────────
// These top-level functions let us write `converseRequest { ... }` instead of
// `ConverseRequest.newBuilder().apply { ... }.build()`.

private fun converseRequest(block: ConverseRequest.Builder.() -> Unit): ConverseRequest =
    ConverseRequest.newBuilder().apply(block).build()

private fun streamConfig(block: StreamConfig.Builder.() -> Unit): StreamConfig =
    StreamConfig.newBuilder().apply(block).build()

private fun requestMeta(block: RequestMeta.Builder.() -> Unit): RequestMeta =
    RequestMeta.newBuilder().apply(block).build()

private fun clientInfo(block: ClientInfo.Builder.() -> Unit): ClientInfo =
    ClientInfo.newBuilder().apply(block).build()

private fun audioConfig(block: AudioConfig.Builder.() -> Unit): AudioConfig =
    AudioConfig.newBuilder().apply(block).build()

private fun audioChunk(block: AudioChunk.Builder.() -> Unit): AudioChunk =
    AudioChunk.newBuilder().apply(block).build()

private fun controlEvent(block: ControlEvent.Builder.() -> Unit): ControlEvent =
    ControlEvent.newBuilder().apply(block).build()
