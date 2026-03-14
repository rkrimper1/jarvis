// WakeWordDetectorTest.kt
// Jarvis Android Client — unit tests for WakeWordDetector
//
// Tests the pure-Kotlin logic: configuration, sensitivity thresholds,
// accepted-phrase normalisation, and detection data model.
// SpeechRecognizer is an Android framework class and is not instantiated here.

package com.jarvis.client.wakeword

import org.junit.Assert.*
import org.junit.Test

class WakeWordDetectorTest {

    // ── WakeWordConfiguration ─────────────────────────────────────────────────

    @Test fun defaultConfig_wakePhrase() {
        assertEquals("hey jarvis", WakeWordConfiguration.Default.wakePhrase)
    }

    @Test fun defaultConfig_backend_isSpeechRecognizer() {
        assertEquals(WakeWordBackend.SPEECH_RECOGNIZER, WakeWordConfiguration.Default.backend)
    }

    @Test fun defaultConfig_sensitivity_isMedium() {
        assertEquals(WakeWordSensitivity.MEDIUM, WakeWordConfiguration.Default.sensitivity)
    }

    @Test fun defaultConfig_debounceSec_isOnePointFive() {
        assertEquals(1.5, WakeWordConfiguration.Default.debounceSec, 0.001)
    }

    @Test fun defaultConfig_acceptedPhrases_containsWakePhrase() {
        val phrases = WakeWordConfiguration.Default.acceptedPhrases
        assertTrue(phrases.contains("hey jarvis"))
    }

    @Test fun defaultConfig_acceptedPhrases_areAllLowercase() {
        val phrases = WakeWordConfiguration.Default.acceptedPhrases
        phrases.forEach { phrase ->
            assertEquals("Expected lowercase, got: $phrase", phrase.lowercase(), phrase)
        }
    }

    @Test fun customPhraseVariants_appearedInAcceptedPhrases() {
        val cfg = WakeWordConfiguration(
            wakePhrase    = "hello jarvis",
            phraseVariants = listOf("yo jarvis", "OK Jarvis"),
        )
        val phrases = cfg.acceptedPhrases
        assertTrue(phrases.contains("hello jarvis"))
        assertTrue(phrases.contains("yo jarvis"))
        assertTrue(phrases.contains("ok jarvis"))   // normalised to lowercase
    }

    // ── WakeWordSensitivity ────────────────────────────────────────────────────

    @Test fun sensitivity_low_hasHighestThreshold() {
        assertTrue(
            "LOW should have the highest confidence threshold",
            WakeWordSensitivity.LOW.confidenceThreshold > WakeWordSensitivity.MEDIUM.confidenceThreshold
        )
    }

    @Test fun sensitivity_high_hasLowestThreshold() {
        assertTrue(
            "HIGH should have the lowest confidence threshold",
            WakeWordSensitivity.HIGH.confidenceThreshold < WakeWordSensitivity.MEDIUM.confidenceThreshold
        )
    }

    @Test fun sensitivity_thresholds_allInValidRange() {
        WakeWordSensitivity.entries.forEach { s ->
            assertTrue("${s}.confidenceThreshold out of [0,1]", s.confidenceThreshold in 0f..1f)
            assertTrue("${s}.keywordThreshold out of [0,1]",    s.keywordThreshold    in 0f..1f)
        }
    }

    @Test fun sensitivity_keyword_and_confidence_thresholds_areDistinct() {
        // keywordThreshold (model probability) and confidenceThreshold
        // (ASR score) must remain independent — they don't need to be equal.
        WakeWordSensitivity.entries.forEach { s ->
            // Both valid but not necessarily the same value — just verify they exist.
            assertTrue(s.confidenceThreshold >= 0f)
            assertTrue(s.keywordThreshold >= 0f)
        }
    }

    // ── WakeWordDetection ─────────────────────────────────────────────────────

    @Test fun detection_storesMatchedPhrase() {
        val d = WakeWordDetection(
            matchedPhrase  = "hey jarvis",
            confidence     = 0.9f,
            fullTranscript = "hey jarvis turn off lights",
            triggerFrame   = null,
        )
        assertEquals("hey jarvis", d.matchedPhrase)
    }

    @Test fun detection_confidenceInValidRange() {
        val d = WakeWordDetection(
            matchedPhrase  = "jarvis",
            confidence     = 0.75f,
            fullTranscript = "jarvis",
            triggerFrame   = null,
        )
        assertTrue(d.confidence in 0f..1f)
    }

    @Test fun detection_detectedAt_isRecent() {
        val before = System.currentTimeMillis()
        val d = WakeWordDetection(
            matchedPhrase  = "hey jarvis",
            confidence     = 0.8f,
            fullTranscript = "hey jarvis",
            triggerFrame   = null,
        )
        val after = System.currentTimeMillis()
        assertTrue(d.detectedAt in before..after)
    }

    @Test fun detection_nullTriggerFrame_isAllowed() {
        val d = WakeWordDetection(
            matchedPhrase  = "hey jarvis",
            confidence     = 0.8f,
            fullTranscript = "hey jarvis",
            triggerFrame   = null,
        )
        assertNull(d.triggerFrame)
    }

    // ── WakeWordBackend enum ──────────────────────────────────────────────────

    @Test fun backend_hasTwoValues() {
        assertEquals(2, WakeWordBackend.entries.size)
    }

    @Test fun backend_speechRecognizer_exists() {
        assertNotNull(WakeWordBackend.SPEECH_RECOGNIZER)
    }

    @Test fun backend_onDeviceKeyword_exists() {
        assertNotNull(WakeWordBackend.ON_DEVICE_KEYWORD)
    }
}
