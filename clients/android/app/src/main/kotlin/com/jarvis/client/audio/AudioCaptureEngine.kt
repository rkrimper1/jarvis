// AudioCaptureEngine.kt
// Jarvis Android Client — audio/
//
// Owns the full AudioRecord capture pipeline:
//   Microphone → AudioRecord → PCM-16LE → chunked 20 ms frames
//   → SharedFlow<AudioFrame> → GRPCVoiceService + WakeWordDetector
//
// Design constraints:
//   - 16 kHz mono PCM-16LE — matches AudioConfig in voice.proto
//   - 20 ms frames (320 samples) — matches StreamConfig.audio_config.frame_duration_ms
//   - Capture loop runs on a dedicated IO dispatcher thread
//   - Public API is main-thread safe via StateFlow / SharedFlow
//   - Mirrors iOS AudioCaptureEngine public surface exactly
//
// Threading:
//   start() / stop() / pause() / resume() are called from the ViewModel
//   (Dispatchers.Main). The capture loop is launched on Dispatchers.IO.
//   AudioFrame emissions arrive on the IO thread; collectors hop to Main
//   via their own launch context (see VoiceViewModel).

package com.jarvis.client.audio

import android.media.AudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
import android.util.Log
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlin.math.sqrt

private const val TAG = "AudioCaptureEngine"

// ── Public Types ──────────────────────────────────────────────────────────────

/** A single captured audio frame ready for upstream transmission. */
data class AudioFrame(
    /** Raw PCM-16LE bytes at 16 kHz mono. */
    val pcmData:        ByteArray,
    /** Wall-clock capture time in Unix milliseconds. */
    val capturedAtMs:   Long,
    /** RMS energy [0, 1] — used by WakeWordDetector and VAD. */
    val rmsEnergy:      Float,
    /** Monotonic frame index within the current capture session. */
    val sequenceNumber: Long,
) {
    // ByteArray equality by content, not reference.
    override fun equals(other: Any?): Boolean {
        if (this === other) return true
        if (other !is AudioFrame) return false
        return sequenceNumber == other.sequenceNumber &&
               capturedAtMs == other.capturedAtMs &&
               pcmData.contentEquals(other.pcmData)
    }
    override fun hashCode() = sequenceNumber.hashCode()
}

sealed class AudioCaptureError : Exception() {
    data object PermissionDenied    : AudioCaptureError()
    data object AlreadyRunning      : AudioCaptureError()
    data object NotRunning          : AudioCaptureError()
    data object AudioRecordInitFailed : AudioCaptureError()
    class StartFailed(cause: Throwable) : AudioCaptureError()
}

// ── AudioCaptureEngine ────────────────────────────────────────────────────────

/**
 * Manages the AudioRecord microphone capture pipeline.
 *
 * Usage from VoiceViewModel:
 * ```kotlin
 * engine.frames.onEach { frame ->
 *     wakeWordDetector.feed(frame)
 *     if (isStreaming) grpcService.sendAudioChunk(frame)
 * }.launchIn(viewModelScope)
 *
 * engine.start()
 * // ... later ...
 * engine.stop()
 * ```
 */
class AudioCaptureEngine(
    private val scope: CoroutineScope,
) {

    // ── Public config ─────────────────────────────────────────────────────────

    val sampleRateHz:    Int = 16_000
    val channelConfig:   Int = AudioFormat.CHANNEL_IN_MONO
    val audioFormat:     Int = AudioFormat.ENCODING_PCM_16BIT
    val frameDurationMs: Int = 20

    /** Number of Int16 samples per 20 ms frame: 16000 × 0.020 = 320. */
    val samplesPerFrame: Int = sampleRateHz * frameDurationMs / 1000  // 320

    /** Bytes per frame: 320 samples × 2 bytes/sample = 640 bytes. */
    val bytesPerFrame: Int = samplesPerFrame * 2  // 640

    // ── State ─────────────────────────────────────────────────────────────────

    private val _isRunning = MutableStateFlow(false)
    val isRunning: StateFlow<Boolean> = _isRunning.asStateFlow()

    private val _currentRms = MutableStateFlow(0f)
    val currentRms: StateFlow<Float> = _currentRms.asStateFlow()

    /** Hot SharedFlow: every 20 ms AudioFrame captured from the mic. */
    private val _frames = MutableSharedFlow<AudioFrame>(
        extraBufferCapacity = 64,
        onBufferOverflow = kotlinx.coroutines.channels.BufferOverflow.DROP_OLDEST,
    )
    val frames: SharedFlow<AudioFrame> = _frames.asSharedFlow()

    // ── Private ───────────────────────────────────────────────────────────────

    private var audioRecord:   AudioRecord? = null
    private var captureJob:    Job?         = null
    @Volatile private var paused = false
    private var sequenceNumber = 0L

    // ── Public Interface ──────────────────────────────────────────────────────

    /**
     * Initialises AudioRecord and starts the capture loop.
     * Must be called after RECORD_AUDIO permission is granted.
     */
    @Throws(AudioCaptureError::class)
    fun start() {
        if (_isRunning.value) throw AudioCaptureError.AlreadyRunning

        val minBuf = AudioRecord.getMinBufferSize(sampleRateHz, channelConfig, audioFormat)
        if (minBuf == AudioRecord.ERROR_BAD_VALUE || minBuf == AudioRecord.ERROR) {
            throw AudioCaptureError.AudioRecordInitFailed
        }

        // Use at least 2× the frame size so the OS never blocks us on a read.
        val bufSize = maxOf(minBuf, bytesPerFrame * 4)

        val record = AudioRecord(
            MediaRecorder.AudioSource.VOICE_RECOGNITION,  // applies AGC + noise suppression
            sampleRateHz,
            channelConfig,
            audioFormat,
            bufSize,
        )

        if (record.state != AudioRecord.STATE_INITIALIZED) {
            record.release()
            throw AudioCaptureError.AudioRecordInitFailed
        }

        audioRecord = record
        sequenceNumber = 0
        paused = false

        record.startRecording()
        _isRunning.value = true

        captureJob = scope.launch(Dispatchers.IO) {
            captureLoop(record)
        }

        Log.i(TAG, "Started — ${sampleRateHz} Hz mono ${frameDurationMs} ms frames")
    }

    /** Stops the capture loop and releases AudioRecord. */
    fun stop() {
        if (!_isRunning.value) return
        captureJob?.cancel()
        captureJob = null
        audioRecord?.let { rec ->
            if (rec.recordingState == AudioRecord.RECORDSTATE_RECORDING) {
                rec.stop()
            }
            rec.release()
        }
        audioRecord = null
        _isRunning.value = false
        _currentRms.value = 0f
        Log.i(TAG, "Stopped")
    }

    /** Pauses PCM emission without releasing AudioRecord (used during TTS playback). */
    fun pause() {
        paused = true
        Log.i(TAG, "Paused")
    }

    /** Resumes emission after pause(). */
    fun resume() {
        paused = false
        Log.i(TAG, "Resumed")
    }

    // ── Private: Capture Loop ─────────────────────────────────────────────────

    /**
     * Reads from AudioRecord continuously, accumulates samples into 20 ms frames,
     * computes RMS, and emits each complete frame on [_frames].
     */
    private suspend fun captureLoop(record: AudioRecord) {
        val readBuf  = ByteArray(bytesPerFrame)
        val accumBuf = ArrayDeque<Byte>(bytesPerFrame * 2)

        while (isActive) {
            val read = record.read(readBuf, 0, readBuf.size)
            if (read <= 0) continue

            if (paused) continue

            accumBuf.addAll(readBuf.take(read))

            // Drain complete 20 ms frames from the accumulator.
            while (accumBuf.size >= bytesPerFrame) {
                val frameBytes = ByteArray(bytesPerFrame) { accumBuf.removeFirst() }
                val rms        = computeRms(frameBytes)
                val now        = System.currentTimeMillis()

                sequenceNumber++
                val frame = AudioFrame(
                    pcmData        = frameBytes,
                    capturedAtMs   = now,
                    rmsEnergy      = rms,
                    sequenceNumber = sequenceNumber,
                )

                _currentRms.value = rms
                _frames.tryEmit(frame)
            }
        }
    }

    // ── Private: DSP ─────────────────────────────────────────────────────────

    /**
     * Computes normalised RMS energy [0, 1] from a PCM-16LE byte array.
     * Interprets pairs of bytes as little-endian Int16 samples.
     */
    private fun computeRms(pcm: ByteArray): Float {
        val sampleCount = pcm.size / 2
        if (sampleCount == 0) return 0f

        var sumSq = 0.0
        for (i in 0 until sampleCount) {
            // Little-endian Int16
            val lo   = pcm[i * 2].toInt() and 0xFF
            val hi   = pcm[i * 2 + 1].toInt()
            val s    = ((hi shl 8) or lo).toShort()
            val norm = s.toFloat() / Short.MAX_VALUE
            sumSq   += (norm * norm).toDouble()
        }
        return sqrt(sumSq / sampleCount).toFloat().coerceIn(0f, 1f)
    }
}
