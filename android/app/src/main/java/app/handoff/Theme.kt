package app.handoff

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color

// The web app's design tokens (web/src/index.css), mapped 1:1. hc is the
// accessor: colors Material 3 doesn't carry live here, the scheme maps the rest.
@Immutable
data class HandoffColors(
    val bg: Color, val fg: Color, val card: Color, val popover: Color,
    val muted: Color, val accent: Color, val border: Color, val input: Color,
    val mutedFg: Color, val primary: Color, val primaryFg: Color,
    val today: Color, val unseen: Color, val destructive: Color,
    val violet: Color, val violetFg: Color, val amber: Color, val amberSoft: Color,
)

private val LightHc = HandoffColors(
    bg = Color(0xFFFFFFFF), fg = Color(0xFF1A1A1A), card = Color(0xFFFFFFFF),
    popover = Color(0xFFFFFFFF),
    muted = Color(0x0A0A0A0A), accent = Color(0x0F0A0A0A), border = Color(0x140A0A0A),
    input = Color(0x290A0A0A), mutedFg = Color(0xFF737373),
    primary = Color(0xFF1A1A1A), primaryFg = Color(0xFFFAFAFA),
    today = Color(0xFF3B82F6), unseen = Color(0xFFF99C00), destructive = Color(0xFFDC2626),
    violet = Color(0xFF7C3AED), violetFg = Color(0xFF8B5CF6),
    amber = Color(0xFFF59E0B), amberSoft = Color(0xFFFFF7E0),
)

private val DarkHc = HandoffColors(
    bg = Color(0xFF0A0A0A), fg = Color(0xFFFAFAFA), card = Color(0xFF141414),
    popover = Color(0xFF171717),
    muted = Color(0x0DFAFAFA), accent = Color(0x14FAFAFA), border = Color(0x1AFAFAFA),
    input = Color(0x40FAFAFA), mutedFg = Color(0xFFA3A3A3),
    primary = Color(0xFFFAFAFA), primaryFg = Color(0xFF0A0A0A),
    today = Color(0xFF60A5FA), unseen = Color(0xFFFCBB00), destructive = Color(0xFFF87171),
    violet = Color(0xFFA78BFA), violetFg = Color(0xFF8B5CF6),
    amber = Color(0xFFF59E0B), amberSoft = Color(0x26F59E0B),
)

val LocalHc = staticCompositionLocalOf { LightHc }

/** The app's token colors for the current theme. */
val hc: HandoffColors
    @Composable get() = LocalHc.current

@Composable
fun HandoffTheme(content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    val c = if (dark) DarkHc else LightHc
    val scheme = if (dark) darkColorScheme(
        background = c.bg, onBackground = c.fg,
        surface = c.bg, onSurface = c.fg,
        surfaceVariant = c.card, onSurfaceVariant = c.mutedFg,
        primary = c.primary, onPrimary = c.primaryFg,
        secondary = c.accent, onSecondary = c.fg,
        secondaryContainer = c.accent, onSecondaryContainer = c.fg,
        tertiary = c.violet, onTertiary = c.primaryFg,
        error = c.destructive, outline = c.border, outlineVariant = c.border,
        surfaceContainer = c.card, surfaceContainerHigh = c.popover,
    ) else lightColorScheme(
        background = c.bg, onBackground = c.fg,
        surface = c.bg, onSurface = c.fg,
        surfaceVariant = c.card, onSurfaceVariant = c.mutedFg,
        primary = c.primary, onPrimary = c.primaryFg,
        secondary = c.accent, onSecondary = c.fg,
        secondaryContainer = c.accent, onSecondaryContainer = c.fg,
        tertiary = c.violet, onTertiary = c.primaryFg,
        error = c.destructive, outline = c.border, outlineVariant = c.border,
        surfaceContainer = c.card, surfaceContainerHigh = c.popover,
    )
    CompositionLocalProvider(LocalHc provides c) {
        MaterialTheme(colorScheme = scheme, content = content)
    }
}
