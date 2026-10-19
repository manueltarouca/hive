package xyz.matm.hive.ui

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicText
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.rotate
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/** One line that fades out at its end when too long: hive never truncates with "…". */
@Composable
fun FadeText(text: String, style: TextStyle, color: Color, modifier: Modifier = Modifier) {
    var overflow by remember(text) { mutableStateOf(false) }
    BasicText(
        text, style = style.copy(color = color), maxLines = 1, softWrap = false, overflow = TextOverflow.Clip,
        onTextLayout = { overflow = it.hasVisualOverflow },
        modifier = modifier
            .graphicsLayer { compositingStrategy = CompositingStrategy.Offscreen }
            .drawWithContent {
                drawContent()
                if (overflow) {
                    val w = minOf(28.dp.toPx(), size.width / 3)
                    drawRect(
                        Brush.horizontalGradient(listOf(Color.Black, Color.Transparent), startX = size.width - w, endX = size.width),
                        topLeft = Offset(size.width - w, 0f), blendMode = BlendMode.DstIn,
                    )
                }
            },
    )
}

/** A round pill button: honey-filled when primary, quiet otherwise. */
@Composable
fun Pill(text: String, primary: Boolean = false, modifier: Modifier = Modifier, selected: Boolean = false, danger: Boolean = false, onClick: () -> Unit) {
    val p = LocalPalette.current
    val shape = RoundedCornerShape(50)
    val bg = when {
        primary -> p.honey
        selected -> p.honey.copy(alpha = 0.16f)
        else -> p.surface
    }
    // A quiet pill keeps a hairline, so it reads as a button on any background.
    val line = when {
        primary -> null
        selected -> p.honey.copy(alpha = 0.5f)
        else -> p.line
    }
    Box(
        modifier
            .clip(shape)
            .background(bg)
            .then(if (line != null) Modifier.border(1.dp, line, shape) else Modifier)
            .clickable(role = Role.Button, onClick = onClick)
            .semantics { this.selected = selected }
            .padding(horizontal = 14.dp, vertical = 8.dp),
    ) {
        BasicText(text, style = Type.label.copy(color = if (primary) p.onHoney else if (danger) p.coral else p.text), maxLines = 1)
    }
}

/** Simple line icons, drawn so they take the palette's colors. */
enum class Icon { Plus, Chevron, Laptop, Globe, Keyboard, Send, Back, Check, Link, Alert, Camera, Gear, Minus, Pin, Search }

@Composable
fun LineIcon(icon: Icon, color: Color, size: Dp = 18.dp, modifier: Modifier = Modifier, description: String? = null) {
    Canvas(modifier.size(size).then(if (description != null) Modifier.semantics { contentDescription = description } else Modifier)) {
        drawIcon(icon, color)
    }
}

private fun DrawScope.drawIcon(icon: Icon, color: Color) {
    val s = size.minDimension
    val st = Stroke(width = s * 0.085f, cap = StrokeCap.Round)
    fun line(x1: Float, y1: Float, x2: Float, y2: Float) =
        drawLine(color, Offset(x1 * s, y1 * s), Offset(x2 * s, y2 * s), strokeWidth = st.width, cap = StrokeCap.Round)
    when (icon) {
        Icon.Plus -> { line(.5f, .2f, .5f, .8f); line(.2f, .5f, .8f, .5f) }
        Icon.Chevron -> { line(.3f, .4f, .5f, .6f); line(.5f, .6f, .7f, .4f) }
        Icon.Back -> { line(.6f, .25f, .35f, .5f); line(.35f, .5f, .6f, .75f) }
        Icon.Check -> { line(.22f, .52f, .42f, .72f); line(.42f, .72f, .8f, .3f) }
        Icon.Laptop -> {
            drawRoundRect(color, Offset(.2f * s, .28f * s), androidx.compose.ui.geometry.Size(.6f * s, .4f * s),
                androidx.compose.ui.geometry.CornerRadius(.05f * s), style = st)
            line(.1f, .78f, .9f, .78f)
        }
        Icon.Globe -> {
            drawCircle(color, .36f * s, center, style = st)
            line(.14f, .5f, .86f, .5f)
            drawOval(color, Offset(.35f * s, .14f * s), androidx.compose.ui.geometry.Size(.3f * s, .72f * s), style = st)
        }
        Icon.Keyboard -> {
            drawRoundRect(color, Offset(.1f * s, .27f * s), androidx.compose.ui.geometry.Size(.8f * s, .46f * s),
                androidx.compose.ui.geometry.CornerRadius(.08f * s), style = st)
            for (x in listOf(.27f, .42f, .58f, .73f)) drawCircle(color, .035f * s, Offset(x * s, .42f * s))
            line(.32f, .58f, .68f, .58f)
        }
        Icon.Send -> { line(.5f, .78f, .5f, .25f); line(.3f, .45f, .5f, .25f); line(.5f, .25f, .7f, .45f) }
        Icon.Link -> rotate(-45f) {
            // Two links of a chain, overlapping at the middle.
            drawRoundRect(color, Offset(.1f * s, .37f * s), androidx.compose.ui.geometry.Size(.46f * s, .26f * s),
                androidx.compose.ui.geometry.CornerRadius(.13f * s), style = st)
            drawRoundRect(color, Offset(.44f * s, .37f * s), androidx.compose.ui.geometry.Size(.46f * s, .26f * s),
                androidx.compose.ui.geometry.CornerRadius(.13f * s), style = st)
        }
        Icon.Gear -> {
            drawCircle(color, .13f * s, center, style = st)
            for (i in 0 until 8) {
                val a = Math.toRadians(45.0 * i)
                val c = Math.cos(a).toFloat(); val n = Math.sin(a).toFloat()
                line(.5f + .27f * c, .5f + .27f * n, .5f + .38f * c, .5f + .38f * n)
            }
            drawCircle(color, .27f * s, center, style = st)
        }
        Icon.Minus -> line(.2f, .5f, .8f, .5f)
        Icon.Pin -> {
            // A push pin, as the Mac's pin.fill: the head, the collar, the needle.
            val head = Path().apply {
                moveTo(.34f * s, .14f * s); lineTo(.66f * s, .14f * s); lineTo(.62f * s, .22f * s)
                lineTo(.64f * s, .44f * s); lineTo(.76f * s, .56f * s); lineTo(.24f * s, .56f * s)
                lineTo(.36f * s, .44f * s); lineTo(.38f * s, .22f * s); close()
            }
            drawPath(head, color)
            drawPath(head, color, style = Stroke(width = st.width * 0.6f, join = StrokeJoin.Round))
            line(.5f, .56f, .5f, .88f)
        }
        Icon.Search -> { drawCircle(color, .24f * s, Offset(.44f * s, .44f * s), style = st); line(.62f, .62f, .82f, .82f) }
        Icon.Alert -> { line(.5f, .2f, .5f, .6f); drawCircle(color, .05f * s, Offset(.5f * s, .78f * s)) }
        Icon.Camera -> {
            drawRoundRect(color, Offset(.12f * s, .3f * s), androidx.compose.ui.geometry.Size(.76f * s, .5f * s),
                androidx.compose.ui.geometry.CornerRadius(.08f * s), style = st)
            drawCircle(color, .13f * s, Offset(.5f * s, .55f * s), style = st)
            line(.38f, .3f, .43f, .2f); line(.43f, .2f, .57f, .2f); line(.57f, .2f, .62f, .3f)
        }
    }
}
