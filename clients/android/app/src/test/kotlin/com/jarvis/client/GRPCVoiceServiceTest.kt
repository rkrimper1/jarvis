// GRPCVoiceServiceTest.kt
// Jarvis Android Client — unit tests for GRPCVoiceService
//
// Tests the pure-Kotlin logic: configuration presets, VoiceEvent model,
// VoiceServiceError sealed hierarchy, and initial StateFlow values.
// Actual gRPC I/O is not tested here — that belongs in integration tests
// that can reach a live or mock server.

package com.jarvis.client.grpc

import kotlinx.coroutines.test.*
import org.junit.Assert.*
import org.junit.Test

class GRPCVoiceServiceTest {

    private val testScope  = TestScope()
    private val devConfig  = VoiceServiceConfiguration.Development
    private val prodConfig = VoiceServiceConfiguration.Production

    // ── Configuration presets ─────────────────────────────────────────────────

    @Test fun devConfig_host_isLocalhost() {
        assertEquals("localhost", devConfig.host)
    }

    @Test fun devConfig_port_is50059() {
        assertEquals(50059, devConfig.port)
    }

    @Test fun devConfig_tls_isDisabled() {
        assertFalse(devConfig.useTls)
    }

    @Test fun prodConfig_port_is443() {
        assertEquals(443, prodConfig.port)
    }

    @Test fun prodConfig_tls_isEnabled() {
        assertTrue(prodConfig.useTls)
    }

    @Test fun prodConfig_maxReconnectAttempts_greaterThanDev() {
        assertTrue(prodConfig.maxReconnectAttempts >= devConfig.maxReconnectAttempts)
    }

    @Test fun devConfig_languageCode_default_enUS() {
        assertEquals("en-US", devConfig.languageCode)
    }

    @Test fun devConfig_contextTags_defaultEmpty() {
        assertTrue(devConfig.contextTags.isEmpty())
    }

    @Test fun devConfig_reconnectBaseDelay_isPositive() {
        assertTrue(devConfig.reconnectBaseDelaySec > 0.0)
    }

    // ── Initial service state ─────────────────────────────────────────────────

    @Test fun service_isConnected_initiallyFalse() {
        val svc = GRPCVoiceService(devConfig, testScope)
        assertFalse(svc.isConnected.value)
    }

    @Test fun service_reconnectAttempt_initiallyZero() {
        val svc = GRPCVoiceService(devConfig, testScope)
        assertEquals(0, svc.reconnectAttempt.value)
    }

    // ── VoiceEvent sealed class ───────────────────────────────────────────────

    @Test fun voiceEvent_statusChanged_storesFields() {
        val event = VoiceEvent.StatusChanged(
            state   = jarvis.voice.Voice.StatusEvent.State.STATE_LISTENING,
            message = "listening",
        )
        assertEquals(jarvis.voice.Voice.StatusEvent.State.STATE_LISTENING, event.state)
        assertEquals("listening", event.message)
    }

    @Test fun voiceEvent_transcriptReceived_storesFields() {
        val event = VoiceEvent.TranscriptReceived(
            text       = "turn off lights",
            isFinal    = true,
            confidence = 0.95f,
        )
        assertEquals("turn off lights", event.text)
        assertTrue(event.isFinal)
        assertEquals(0.95f, event.confidence, 0.001f)
    }

    @Test fun voiceEvent_replyReceived_storesFields() {
        val event = VoiceEvent.ReplyReceived(
            replyText            = "Turning off the lights.",
            intent               = "INTENT_SYSTEM_CONTROL",
            requiresConfirmation = false,
        )
        assertEquals("Turning off the lights.", event.replyText)
        assertFalse(event.requiresConfirmation)
    }

    @Test fun voiceEvent_audioReply_byteArrayEquality() {
        val data = ByteArray(64) { it.toByte() }
        val a = VoiceEvent.AudioReplyReceived(data.copyOf(), "text", isFinalChunk = true)
        val b = VoiceEvent.AudioReplyReceived(data.copyOf(), "text", isFinalChunk = true)
        assertEquals(a, b)
    }

    @Test fun voiceEvent_streamClosed_isSingleton() {
        val a = VoiceEvent.StreamClosed
        val b = VoiceEvent.StreamClosed
        assertSame(a, b)
    }

    // ── VoiceServiceError sealed class ────────────────────────────────────────

    @Test fun error_notConnected_isSingleton() {
        val a = VoiceServiceError.NotConnected
        val b = VoiceServiceError.NotConnected
        assertSame(a, b)
    }

    @Test fun error_sessionCapacityExceeded_isSingleton() {
        val a = VoiceServiceError.SessionCapacityExceeded
        val b = VoiceServiceError.SessionCapacityExceeded
        assertSame(a, b)
    }

    @Test fun error_streamSetupFailed_wrapsThrowable() {
        val cause = RuntimeException("connection refused")
        val error = VoiceServiceError.StreamSetupFailed(cause)
        assertEquals(cause, error.cause)
    }

    @Test fun error_sendFailed_wrapsThrowable() {
        val cause = RuntimeException("broken pipe")
        val error = VoiceServiceError.SendFailed(cause)
        assertEquals(cause, error.cause)
    }

    @Test fun error_reconnectLimitReached_storesAttempts() {
        val error = VoiceServiceError.ReconnectLimitReached(attempts = 5)
        assertEquals(5, error.attempts)
    }

    @Test fun error_invalidConfiguration_storesDetail() {
        val error = VoiceServiceError.InvalidConfiguration("host must not be empty")
        assertEquals("host must not be empty", error.detail)
    }
}
