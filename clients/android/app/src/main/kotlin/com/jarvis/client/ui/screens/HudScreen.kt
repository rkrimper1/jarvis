// HudScreen.kt
// Jarvis Android Client — ui/screens/
//
// Primary Compose screen — the always-on HUD overlay.
// Mirrors iOS HUDView structure and state-machine colour logic exactly.

package com.jarvis.client.ui.screens

import androidx.compose.animation.*
import androidx.compose.animation.core.*
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.jarvis.client.ui.components.WaveformView
import com.jarvis.client.ui.theme.JarvisColors
import com.jarvis.client.viewmodel.*

@Composable
fun HudScreen(
    voiceState:        VoiceState,
    isConnected:       Boolean,
    micRms:            Float,
    liveTranscript:    String,
    transcriptHistory: List<TranscriptLine>,
    lastReply:         String,
    pendingActions:    List<HudActionModel>,
    requiresConfirmation: Boolean,
    lastErrorMessage:  String?,
    hasMicPermission:  Boolean,
    onStart:           () -> Unit,
    onStop:            () -> Unit,
    onEndSpeech:       () -> Unit,
    onCancel:          () -> Unit,
    onConfirm:         () -> Unit,
    onDismissAction:   () -> Unit,
    onClearHistory:    () -> Unit,
    modifier:          Modifier = Modifier,
) {
    val stateColor = voiceState.hudColor

    Column(
        modifier = modifier
            .fillMaxSize()
            .background(JarvisColors.Background)
            .padding(horizontal = 20.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {

        // ── Header row ───────────────────────────────────────────────────────
        Row(
            modifier            = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment   = Alignment.CenterVertically,
        ) {
            Text(
                text       = "JARVIS",
                color      = JarvisColors.Primary,
                fontSize   = 22.sp,
                fontWeight = FontWeight.Bold,
                fontFamily = FontFamily.Monospace,
                letterSpacing = 4.sp,
            )
            ConnectionBadge(isConnected = isConnected)
        }

        // ── Waveform ─────────────────────────────────────────────────────────
        WaveformView(
            rmsEnergy = micRms,
            isActive  = voiceState.isActive,
            modifier  = Modifier
                .fillMaxWidth()
                .height(64.dp),
            barColor  = stateColor,
        )

        // ── State label ───────────────────────────────────────────────────────
        AnimatedContent(
            targetState    = voiceState.displayLabel,
            transitionSpec = { fadeIn() togetherWith fadeOut() },
            label          = "state_label",
        ) { label ->
            Text(
                text      = label,
                color     = stateColor,
                fontSize  = 14.sp,
                fontWeight = FontWeight.Medium,
                modifier  = Modifier.fillMaxWidth(),
                textAlign = TextAlign.Center,
            )
        }

        // ── Live transcript ───────────────────────────────────────────────────
        AnimatedVisibility(
            visible = liveTranscript.isNotBlank(),
            enter   = fadeIn() + expandVertically(),
            exit    = fadeOut() + shrinkVertically(),
        ) {
            Text(
                text     = liveTranscript,
                color    = JarvisColors.TextSecondary,
                fontSize = 13.sp,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(8.dp))
                    .background(JarvisColors.SurfaceVariant)
                    .padding(10.dp),
            )
        }

        // ── Last reply ────────────────────────────────────────────────────────
        AnimatedVisibility(
            visible = lastReply.isNotBlank() && voiceState != VoiceState.LISTENING,
        ) {
            Text(
                text      = lastReply,
                color     = JarvisColors.TextPrimary,
                fontSize  = 15.sp,
                fontWeight = FontWeight.Medium,
                modifier  = Modifier.fillMaxWidth(),
                textAlign = TextAlign.Center,
            )
        }

        // ── Confirmation prompt ───────────────────────────────────────────────
        AnimatedVisibility(visible = requiresConfirmation) {
            Row(
                modifier              = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Button(
                    onClick  = onConfirm,
                    modifier = Modifier.weight(1f),
                    colors   = ButtonDefaults.buttonColors(containerColor = JarvisColors.Primary),
                ) { Text("Confirm") }
                OutlinedButton(
                    onClick  = onCancel,
                    modifier = Modifier.weight(1f),
                ) { Text("Cancel") }
            }
        }

        // ── Pending HUD actions ───────────────────────────────────────────────
        if (pendingActions.isNotEmpty()) {
            HudActionCard(
                action    = pendingActions.first(),
                onDismiss = onDismissAction,
            )
        }

        // ── Error banner ──────────────────────────────────────────────────────
        AnimatedVisibility(visible = voiceState == VoiceState.ERROR) {
            Surface(
                color  = JarvisColors.Error.copy(alpha = 0.15f),
                shape  = RoundedCornerShape(8.dp),
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(
                    text     = lastErrorMessage ?: "An error occurred",
                    color    = JarvisColors.Error,
                    fontSize = 12.sp,
                    modifier = Modifier.padding(10.dp),
                )
            }
        }

        // ── Transcript history ────────────────────────────────────────────────
        val listState = rememberLazyListState()
        LaunchedEffect(transcriptHistory.size) {
            if (transcriptHistory.isNotEmpty()) {
                listState.animateScrollToItem(transcriptHistory.size - 1)
            }
        }

        LazyColumn(
            state    = listState,
            modifier = Modifier.weight(1f),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            items(transcriptHistory, key = { it.id }) { line ->
                TranscriptRow(line = line)
            }
        }

        // ── Control buttons ───────────────────────────────────────────────────
        if (!hasMicPermission) {
            Text(
                text      = "Microphone permission required",
                color     = JarvisColors.Error,
                fontSize  = 12.sp,
                textAlign = TextAlign.Center,
                modifier  = Modifier.fillMaxWidth(),
            )
        } else {
            ControlBar(
                voiceState  = voiceState,
                onStart     = onStart,
                onStop      = onStop,
                onEndSpeech = onEndSpeech,
                onCancel    = onCancel,
            )
        }
    }
}

// ── Sub-components ────────────────────────────────────────────────────────────

@Composable
private fun ConnectionBadge(isConnected: Boolean) {
    val color  = if (isConnected) JarvisColors.Speaking else JarvisColors.TextMuted
    val label  = if (isConnected) "LIVE" else "OFFLINE"
    Surface(
        color = color.copy(alpha = 0.15f),
        shape = RoundedCornerShape(4.dp),
    ) {
        Text(
            text      = label,
            color     = color,
            fontSize  = 10.sp,
            fontWeight = FontWeight.Bold,
            letterSpacing = 1.sp,
            modifier  = Modifier.padding(horizontal = 8.dp, vertical = 4.dp),
        )
    }
}

@Composable
private fun TranscriptRow(line: TranscriptLine) {
    val align = if (line.source == TranscriptSource.USER) Alignment.End else Alignment.Start
    val bgColor = if (line.source == TranscriptSource.USER)
        JarvisColors.Primary.copy(alpha = 0.12f)
    else
        JarvisColors.SurfaceVariant

    Column(
        modifier          = Modifier.fillMaxWidth(),
        horizontalAlignment = align,
    ) {
        Surface(
            color = bgColor,
            shape = RoundedCornerShape(10.dp),
            modifier = Modifier.widthIn(max = 280.dp),
        ) {
            Text(
                text     = line.text,
                color    = JarvisColors.TextPrimary,
                fontSize = 13.sp,
                modifier = Modifier.padding(horizontal = 12.dp, vertical = 8.dp),
            )
        }
    }
}

@Composable
private fun HudActionCard(action: HudActionModel, onDismiss: () -> Unit) {
    val severityColor = when (action.severity) {
        HudSeverity.INFO      -> JarvisColors.Primary
        HudSeverity.WARNING   -> JarvisColors.Processing
        HudSeverity.CRITICAL  -> Color(0xFFFF6B35)
        HudSeverity.EMERGENCY -> JarvisColors.Error
    }
    Surface(
        color  = severityColor.copy(alpha = 0.12f),
        shape  = RoundedCornerShape(8.dp),
        modifier = Modifier.fillMaxWidth(),
    ) {
        Row(
            modifier = Modifier.padding(12.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text      = action.type.name.replace('_', ' '),
                    color     = severityColor,
                    fontSize  = 11.sp,
                    fontWeight = FontWeight.Bold,
                    letterSpacing = 0.5.sp,
                )
                if (action.payloadJson.isNotBlank()) {
                    Text(
                        text     = action.payloadJson,
                        color    = JarvisColors.TextSecondary,
                        fontSize = 11.sp,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
            TextButton(onClick = onDismiss) {
                Text("×", color = severityColor, fontSize = 18.sp)
            }
        }
    }
}

@Composable
private fun ControlBar(
    voiceState:  VoiceState,
    onStart:     () -> Unit,
    onStop:      () -> Unit,
    onEndSpeech: () -> Unit,
    onCancel:    () -> Unit,
) {
    Row(
        modifier              = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        when (voiceState) {
            VoiceState.IDLE, VoiceState.ERROR -> {
                Button(
                    onClick  = onStart,
                    modifier = Modifier.weight(1f),
                    colors   = ButtonDefaults.buttonColors(containerColor = JarvisColors.Primary),
                ) { Text("Start", color = JarvisColors.OnPrimary) }
            }
            VoiceState.LISTENING -> {
                Button(
                    onClick  = onEndSpeech,
                    modifier = Modifier.weight(1f),
                    colors   = ButtonDefaults.buttonColors(containerColor = JarvisColors.Listening),
                ) { Text("Done", color = JarvisColors.OnPrimary) }
                OutlinedButton(
                    onClick  = onCancel,
                    modifier = Modifier.weight(1f),
                ) { Text("Cancel") }
            }
            VoiceState.PROCESSING, VoiceState.SPEAKING -> {
                OutlinedButton(
                    onClick  = onCancel,
                    modifier = Modifier.weight(1f),
                ) { Text("Cancel") }
                OutlinedButton(
                    onClick  = onStop,
                    modifier = Modifier.weight(1f),
                ) { Text("Stop") }
            }
        }
    }
}

// ── Extension: state → colour ─────────────────────────────────────────────────

val VoiceState.hudColor: Color get() = when (this) {
    VoiceState.IDLE       -> JarvisColors.TextMuted
    VoiceState.LISTENING  -> JarvisColors.Listening
    VoiceState.PROCESSING -> JarvisColors.Processing
    VoiceState.SPEAKING   -> JarvisColors.Speaking
    VoiceState.ERROR      -> JarvisColors.Error
}
