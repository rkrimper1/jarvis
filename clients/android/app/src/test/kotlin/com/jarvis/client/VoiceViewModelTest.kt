// VoiceViewModelTest.kt
// Jarvis Android Client — unit tests for ViewModel types
//
// Tests VoiceState, TranscriptLine, and HudActionModel — all pure-Kotlin
// data types that require no Android framework. VoiceViewModel itself
// requires Application context and is exercised in instrumented tests.

package com.jarvis.client.viewmodel

import org.junit.Assert.*
import org.junit.Test

class VoiceStateTest {

    @Test fun idle_isNotActive() {
        assertFalse(VoiceState.IDLE.isActive)
    }

    @Test fun error_isNotActive() {
        assertFalse(VoiceState.ERROR.isActive)
    }

    @Test fun listening_isActive() {
        assertTrue(VoiceState.LISTENING.isActive)
    }

    @Test fun processing_isActive() {
        assertTrue(VoiceState.PROCESSING.isActive)
    }

    @Test fun speaking_isActive() {
        assertTrue(VoiceState.SPEAKING.isActive)
    }

    @Test fun idle_displayLabel_isStandby() {
        assertEquals("Standby", VoiceState.IDLE.displayLabel)
    }

    @Test fun listening_displayLabel() {
        assertEquals("Listening…", VoiceState.LISTENING.displayLabel)
    }

    @Test fun processing_displayLabel() {
        assertEquals("Processing…", VoiceState.PROCESSING.displayLabel)
    }

    @Test fun speaking_displayLabel() {
        assertEquals("Speaking…", VoiceState.SPEAKING.displayLabel)
    }

    @Test fun error_displayLabel() {
        assertEquals("Error", VoiceState.ERROR.displayLabel)
    }

    @Test fun voiceState_hasExactlyFiveValues() {
        assertEquals(5, VoiceState.entries.size)
    }
}

class TranscriptLineTest {

    @Test fun userLine_hasCorrectSource() {
        val line = TranscriptLine(
            text       = "Hey Jarvis",
            isFinal    = true,
            confidence = 0.9f,
            source     = TranscriptSource.USER,
        )
        assertEquals(TranscriptSource.USER, line.source)
    }

    @Test fun jarvisLine_hasCorrectSource() {
        val line = TranscriptLine(
            text       = "Turning off the lights.",
            isFinal    = true,
            confidence = 1f,
            source     = TranscriptSource.JARVIS,
        )
        assertEquals(TranscriptSource.JARVIS, line.source)
    }

    @Test fun transcriptLine_id_isUnique() {
        val a = TranscriptLine("hello", true, 1f, TranscriptSource.USER)
        val b = TranscriptLine("hello", true, 1f, TranscriptSource.USER)
        assertNotEquals(a.id, b.id)
    }

    @Test fun transcriptLine_timestamp_isRecent() {
        val before = System.currentTimeMillis()
        val line   = TranscriptLine("hi", false, 0f, TranscriptSource.USER)
        val after  = System.currentTimeMillis()
        assertTrue(line.timestamp in before..after)
    }
}

class HudActionModelTest {

    @Test fun hudActionModel_id_isUnique() {
        val a = HudActionModel(type = HudActionType.SHOW_CARD, payloadJson = "{}", severity = HudSeverity.INFO)
        val b = HudActionModel(type = HudActionType.SHOW_CARD, payloadJson = "{}", severity = HudSeverity.INFO)
        assertNotEquals(a.id, b.id)
    }

    @Test fun hudActionType_from_knownValue() {
        assertEquals(HudActionType.OPEN_APP,          HudActionType.from(1))
        assertEquals(HudActionType.SHOW_CARD,         HudActionType.from(2))
        assertEquals(HudActionType.SET_TIMER,         HudActionType.from(3))
        assertEquals(HudActionType.NAVIGATE,          HudActionType.from(4))
        assertEquals(HudActionType.DISMISS_HUD,       HudActionType.from(5))
        assertEquals(HudActionType.DISPATCH_AGENT,    HudActionType.from(6))
        assertEquals(HudActionType.HARDWARE_CMD,      HudActionType.from(7))
        assertEquals(HudActionType.SECURITY_PROTOCOL, HudActionType.from(8))
    }

    @Test fun hudActionType_from_unknownValue_returnsUnknown() {
        assertEquals(HudActionType.UNKNOWN, HudActionType.from(999))
        assertEquals(HudActionType.UNKNOWN, HudActionType.from(-1))
    }

    @Test fun hudActionType_from_zero_returnsUnknown() {
        assertEquals(HudActionType.UNKNOWN, HudActionType.from(0))
    }

    @Test fun hudSeverity_hasExactlyFourValues() {
        assertEquals(4, HudSeverity.entries.size)
    }

    @Test fun hudActionModel_receivedAt_isRecent() {
        val before = System.currentTimeMillis()
        val model  = HudActionModel(type = HudActionType.SHOW_CARD, payloadJson = "{}", severity = HudSeverity.INFO)
        val after  = System.currentTimeMillis()
        assertTrue(model.receivedAt in before..after)
    }
}
