// WaveformView.kt
// Jarvis Android Client — ui/components/

package com.jarvis.client.ui.components

import androidx.compose.animation.core.*
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.jarvis.client.ui.theme.JarvisColors
import kotlin.math.*

/**
 * Animated bar waveform visualiser driven by live microphone RMS energy.
 *
 * Mirrors iOS WaveformView exactly:
 *   - 12 vertical bars with idle breathing animation
 *   - Bars scale with rmsEnergy when active
 *   - Smooth spring transitions between heights
 *
 * @param rmsEnergy  Live RMS [0, 1] from AudioCaptureEngine.
 * @param isActive   True when the pipeline is listening/processing/speaking.
 * @param barColor   Bar colour — defaults to Jarvis cyan.
 * @param barCount   Number of bars. Default: 12.
 * @param barWidth   Width of each bar in dp. Default: 4.
 */
@Composable
fun WaveformView(
    rmsEnergy: Float,
    isActive:  Boolean,
    modifier:  Modifier   = Modifier,
    barColor:  Color      = JarvisColors.WaveformActive,
    idleColor: Color      = JarvisColors.WaveformIdle,
    barCount:  Int        = 12,
    barWidth:  Dp         = 4.dp,
) {
    // Idle breathing animation — pulses slowly when not active.
    val infiniteTransition = rememberInfiniteTransition(label = "waveform_idle")
    val breathPhase by infiniteTransition.animateFloat(
        initialValue = 0f,
        targetValue  = (2 * PI).toFloat(),
        animationSpec = infiniteRepeatable(
            animation  = tween(durationMillis = 2_000, easing = LinearEasing),
            repeatMode = RepeatMode.Restart,
        ),
        label = "breath",
    )

    // Animate the target height toward the live RMS value.
    val targetRms by animateFloatAsState(
        targetValue   = if (isActive) rmsEnergy.coerceIn(0f, 1f) else 0f,
        animationSpec = spring(dampingRatio = Spring.DampingRatioMediumBouncy),
        label         = "rms",
    )

    Canvas(modifier = modifier) {
        val totalWidth   = size.width
        val totalHeight  = size.height
        val barWidthPx   = barWidth.toPx()
        val gap          = (totalWidth - barCount * barWidthPx) / (barCount - 1).coerceAtLeast(1)
        val centerY      = totalHeight / 2f

        for (i in 0 until barCount) {
            val x = i * (barWidthPx + gap) + barWidthPx / 2f

            // Height: active → RMS-driven; idle → slow sine breathing.
            val heightFraction: Float = if (isActive) {
                // Each bar gets a slightly different phase offset for visual variety.
                val phaseOffset = (i.toFloat() / barCount) * PI.toFloat()
                val modulated   = targetRms * (0.6f + 0.4f * sin(breathPhase + phaseOffset))
                0.08f + modulated * 0.92f
            } else {
                // Idle breathing: low-amplitude sine across all bars.
                val wave = sin(breathPhase + (i.toFloat() / barCount) * 2 * PI.toFloat())
                0.05f + 0.08f * ((wave + 1f) / 2f)
            }

            val halfHeight = (heightFraction * totalHeight / 2f).coerceAtLeast(barWidthPx)
            val color      = if (isActive) barColor else idleColor

            drawLine(
                color       = color,
                start       = Offset(x, centerY - halfHeight),
                end         = Offset(x, centerY + halfHeight),
                strokeWidth = barWidthPx,
                cap         = StrokeCap.Round,
            )
        }
    }
}
