// AudioCaptureEngineTest.kt
// Jarvis Android Client — unit tests for AudioCaptureEngine
//
// Exercises the pure-Kotlin logic that doesn't touch Android hardware:
//   - Frame geometry (bytes per frame, samples per frame)
//   - RMS computation via reflection of the private computeRms method
//   - AudioFrame data-class equality and content-equality semantics

package com.jarvis.client.audio

import org.junit.Assert.*
import org.junit.Test
import kotlinx.coroutines.test.TestScope

class AudioCaptureEngineTest {

    private val scope  = TestScope()
    private val engine = AudioCaptureEngine(scope)

    // ── Configuration constants ───────────────────────────────────────────────

    @Test fun sampleRate_is16kHz() {
        assertEquals(16_000, engine.sampleRateHz)
    }

    @Test fun frameDuration_is20ms() {
        assertEquals(20, engine.frameDurationMs)
    }

    @Test fun samplesPerFrame_is320() {
        // 16000 Hz × 0.020 s = 320 samples
        assertEquals(320, engine.samplesPerFrame)
    }

    @Test fun bytesPerFrame_is640() {
        // 320 samples × 2 bytes (Int16) = 640
        assertEquals(640, engine.bytesPerFrame)
    }

    // ── Initial state ─────────────────────────────────────────────────────────

    @Test fun isRunning_initiallyFalse() {
        assertFalse(engine.isRunning.value)
    }

    @Test fun currentRms_initiallyZero() {
        assertEquals(0f, engine.currentRms.value)
    }

    // ── RMS computation ───────────────────────────────────────────────────────

    // Calls the private computeRms method via reflection so we can unit-test it
    // without starting AudioRecord.
    private fun computeRms(pcm: ByteArray): Float {
        val method = AudioCaptureEngine::class.java
            .getDeclaredMethod("computeRms", ByteArray::class.java)
        method.isAccessible = true
        return method.invoke(engine, pcm) as Float
    }

    @Test fun rms_ofSilence_isZero() {
        val silence = ByteArray(640)   // all zeros = silence
        assertEquals(0f, computeRms(silence), 0.001f)
    }

    @Test fun rms_ofFullScale_isOne() {
        // PCM-16LE full-scale positive: 0x7F 0x7F repeated (≈ Int16.MAX_VALUE)
        val fullScale = ByteArray(640) { if (it % 2 == 0) 0xFF.toByte() else 0x7F.toByte() }
        val rms = computeRms(fullScale)
        assertTrue("RMS of near-full-scale should be > 0.9, was $rms", rms > 0.9f)
    }

    @Test fun rms_isNormalisedToZeroOne() {
        val random = ByteArray(640) { (it % 127).toByte() }
        val rms = computeRms(random)
        assertTrue("RMS should be in [0, 1], was $rms", rms in 0f..1f)
    }

    @Test fun rms_ofEmptyArray_isZero() {
        assertEquals(0f, computeRms(ByteArray(0)), 0.001f)
    }

    // ── AudioFrame ────────────────────────────────────────────────────────────

    @Test fun audioFrame_equalityBySequenceAndContent() {
        val pcm = ByteArray(640) { it.toByte() }
        val f1  = AudioFrame(pcm.copyOf(), 1000L, 0.5f, 1L)
        val f2  = AudioFrame(pcm.copyOf(), 1000L, 0.5f, 1L)
        assertEquals(f1, f2)
    }

    @Test fun audioFrame_differentSequence_notEqual() {
        val pcm = ByteArray(640)
        val f1  = AudioFrame(pcm.copyOf(), 1000L, 0f, 1L)
        val f2  = AudioFrame(pcm.copyOf(), 1000L, 0f, 2L)
        assertNotEquals(f1, f2)
    }

    @Test fun audioFrame_differentContent_notEqual() {
        val f1 = AudioFrame(ByteArray(640) { 0 }, 1000L, 0f, 1L)
        val f2 = AudioFrame(ByteArray(640) { 1 }, 1000L, 0f, 1L)
        assertNotEquals(f1, f2)
    }
}
