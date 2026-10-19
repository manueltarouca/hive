package xyz.matm.hive.ui

import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import xyz.matm.hive.R
import kotlin.math.cos
import kotlin.math.sin

/** hive's tokens (BRAND.md): Night is Tomorrow Night, Day is paper; honey is the one accent. */
data class Palette(
    val bg: Color,
    val surface: Color,
    val raised: Color,
    val line: Color,
    val text: Color,
    val muted: Color,
    val honey: Color,
    val onHoney: Color,
    val coral: Color,
    val sage: Color,
    val night: Boolean,
)

val NightPalette = Palette(
    bg = Color(0xFF1D1F21), surface = Color(0xFF282C34), raised = Color(0xFF373B41), line = Color(0xFF373B41),
    text = Color(0xFFC5C8C6), muted = Color(0xFF969896), honey = Color(0xFFF2A93B), onHoney = Color(0xFF1D1F21),
    coral = Color(0xFFCC6666), sage = Color(0xFFB5BD68), night = true,
)

val DayPalette = Palette(
    bg = Color(0xFFF6F2EA), surface = Color(0xFFFDFBF7), raised = Color(0xFFEDE7DC), line = Color(0xFFE4DDD0),
    text = Color(0xFF231F1A), muted = Color(0xFF8C8478), honey = Color(0xFFD98A12), onHoney = Color(0xFFFDFBF7),
    coral = Color(0xFFD24E45), sage = Color(0xFF4E9A64), night = false,
)

val LocalPalette = staticCompositionLocalOf { NightPalette }

/** JetBrains Mono, the terminal's own face (shared from the Mac app's fonts, in assets). */
val LocalMono = staticCompositionLocalOf<FontFamily> { FontFamily.Monospace }

@OptIn(androidx.compose.ui.text.ExperimentalTextApi::class)
private fun inter(weight: Int) = Font(R.font.inter, FontWeight(weight), variationSettings = FontVariation.Settings(FontVariation.weight(weight)))

@OptIn(androidx.compose.ui.text.ExperimentalTextApi::class)
val Inter = FontFamily(inter(300), inter(400), inter(500), inter(600))

object Type {
    val title = TextStyle(fontFamily = Inter, fontWeight = FontWeight(500), fontSize = 17.sp, lineHeight = 22.sp)
    val body = TextStyle(fontFamily = Inter, fontWeight = FontWeight(400), fontSize = 15.sp, lineHeight = 20.sp)
    val small = TextStyle(fontFamily = Inter, fontWeight = FontWeight(400), fontSize = 13.sp, lineHeight = 17.sp)
    val tiny = TextStyle(fontFamily = Inter, fontWeight = FontWeight(400), fontSize = 12.sp, lineHeight = 16.sp)
    val label = TextStyle(fontFamily = Inter, fontWeight = FontWeight(500), fontSize = 14.sp, lineHeight = 18.sp)
    val mono = TextStyle(fontFamily = FontFamily.Monospace, fontSize = 13.sp)
}

@Composable
fun HiveTheme(choice: String = "system", content: @Composable () -> Unit) {
    val dark = when (choice) { "night" -> true; "day" -> false; else -> isSystemInDarkTheme() }
    val p = if (dark) NightPalette else DayPalette
    val assets = LocalContext.current.assets
    val mono = androidx.compose.runtime.remember { FontFamily(Font("JetBrainsMonoNerdFont-Regular.ttf", assets)) }
    CompositionLocalProvider(LocalPalette provides p, LocalMono provides mono, content = content)
}

/** The motion curve: quiet, cubic-bezier(0.4, 0, 0.2, 1). */
val Quiet = CubicBezierEasing(0.4f, 0f, 0.2f, 1f)

/** An agent's state as a pointy-top hexagon: working honey (pulsing), blocked coral, done sage; idle an outline. */
@Composable
fun Hexagon(state: String?, size: Dp = 10.dp, modifier: Modifier = Modifier) {
    val p = LocalPalette.current
    val reduce = reducedMotion()
    val pulse = if (state == "working" && !reduce) {
        val t = rememberInfiniteTransition(label = "pulse")
        val a by t.animateFloat(1f, 0.45f, infiniteRepeatable(tween(1100, easing = Quiet), RepeatMode.Reverse), label = "a")
        a
    } else 1f
    val color = when (state) {
        "working" -> p.honey
        "blocked" -> p.coral
        "done" -> p.sage
        else -> p.muted
    }
    Canvas(modifier.size(size)) {
        val r = this.size.minDimension / 2
        val path = Path()
        for (i in 0 until 6) {
            val a = Math.toRadians(60.0 * i - 90)
            val x = center.x + r * cos(a).toFloat()
            val y = center.y + r * sin(a).toFloat()
            if (i == 0) path.moveTo(x, y) else path.lineTo(x, y)
        }
        path.close()
        if (state == "working" || state == "blocked" || state == "done") drawPath(path, color.copy(alpha = pulse))
        else drawPath(path, color, style = Stroke(width = 1.2.dp.toPx()))
    }
}

@Composable
fun reducedMotion(): Boolean {
    val ctx = LocalContext.current
    return android.provider.Settings.Global.getFloat(ctx.contentResolver, android.provider.Settings.Global.ANIMATOR_DURATION_SCALE, 1f) == 0f
}
