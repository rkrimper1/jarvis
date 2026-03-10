// Theme.kt
// Jarvis Android Client — ui/theme/

package com.jarvis.client.ui.theme

import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

// ── Jarvis Colour Palette ─────────────────────────────────────────────────────

object JarvisColors {
    val Background      = Color(0xFF0A0A0F)   // near-black
    val Surface         = Color(0xFF12121A)
    val SurfaceVariant  = Color(0xFF1C1C2E)

    val Primary         = Color(0xFF00D4FF)   // Jarvis cyan
    val PrimaryVariant  = Color(0xFF0099BB)
    val OnPrimary       = Color(0xFF001F26)

    val Listening       = Color(0xFF00D4FF)   // cyan
    val Processing      = Color(0xFFFFB300)   // amber
    val Speaking        = Color(0xFF4CAF50)   // green
    val Error           = Color(0xFFFF5252)   // red

    val TextPrimary     = Color(0xFFE8F4FF)
    val TextSecondary   = Color(0xFF8AAABB)
    val TextMuted       = Color(0xFF4A6070)

    val WaveformIdle    = Color(0xFF2A3A4A)
    val WaveformActive  = Primary
    val Divider         = Color(0xFF1E2A35)
}

private val JarvisDarkColorScheme = darkColorScheme(
    primary          = JarvisColors.Primary,
    onPrimary        = JarvisColors.OnPrimary,
    primaryContainer = JarvisColors.PrimaryVariant,
    background       = JarvisColors.Background,
    surface          = JarvisColors.Surface,
    surfaceVariant   = JarvisColors.SurfaceVariant,
    onBackground     = JarvisColors.TextPrimary,
    onSurface        = JarvisColors.TextPrimary,
    error            = JarvisColors.Error,
)

@Composable
fun JarvisTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = JarvisDarkColorScheme,
        typography  = Typography(),
        content     = content,
    )
}
