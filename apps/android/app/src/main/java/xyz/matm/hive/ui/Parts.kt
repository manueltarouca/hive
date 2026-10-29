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
import androidx.compose.ui.Alignment
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.wrapContentWidth
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
        contentAlignment = Alignment.Center,
    ) {
        BasicText(text, style = Type.label.copy(color = if (primary) p.onHoney else if (danger) p.coralText else p.text), maxLines = 1)
    }
}

/** Simple line icons, drawn so they take the palette's colors. */
enum class Icon { Plus, Chevron, Laptop, Globe, Keyboard, KeyboardHide, Send, Back, Check, Link, Alert, Camera, Gear, Minus, Pin, Search, Bolt, More, Sidebar, Share, Image, Doc, Folder, Close, Wifi }

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
        // A script's action: a lightning bolt.
        Icon.Bolt -> { line(.58f, .14f, .32f, .54f); line(.32f, .54f, .62f, .48f); line(.62f, .48f, .4f, .86f) }
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
        Icon.KeyboardHide -> {
            // The keyboard, smaller and higher, over a chevron pointing down: put it away.
            drawRoundRect(color, Offset(.16f * s, .14f * s), androidx.compose.ui.geometry.Size(.68f * s, .4f * s),
                androidx.compose.ui.geometry.CornerRadius(.07f * s), style = st)
            for (x in listOf(.3f, .43f, .57f, .7f)) drawCircle(color, .032f * s, Offset(x * s, .28f * s))
            line(.36f, .42f, .64f, .42f)
            line(.38f, .7f, .5f, .82f); line(.5f, .82f, .62f, .7f)
        }
        Icon.More -> for (x in listOf(.26f, .5f, .74f)) drawCircle(color, .055f * s, Offset(x * s, .5f * s))
        Icon.Sidebar -> {
            drawRoundRect(color, Offset(.14f * s, .22f * s), androidx.compose.ui.geometry.Size(.72f * s, .56f * s),
                androidx.compose.ui.geometry.CornerRadius(.08f * s), style = st)
            line(.4f, .22f, .4f, .78f)
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
        Icon.Share -> { line(.5f, .14f, .5f, .6f); line(.32f, .3f, .5f, .14f); line(.5f, .14f, .68f, .3f)
            line(.22f, .52f, .22f, .84f); line(.22f, .84f, .78f, .84f); line(.78f, .84f, .78f, .52f) }
        Icon.Image -> {
            drawRoundRect(color, Offset(.14f * s, .18f * s), androidx.compose.ui.geometry.Size(.72f * s, .64f * s),
                androidx.compose.ui.geometry.CornerRadius(.08f * s), style = st)
            drawCircle(color, .07f * s, Offset(.37f * s, .4f * s), style = st)
            line(.86f, .64f, .66f, .46f); line(.66f, .46f, .3f, .82f)
        }
        Icon.Doc -> { line(.26f, .12f, .58f, .12f); line(.58f, .12f, .74f, .28f); line(.74f, .28f, .74f, .88f)
            line(.74f, .88f, .26f, .88f); line(.26f, .88f, .26f, .12f); line(.38f, .52f, .62f, .52f); line(.38f, .68f, .62f, .68f) }
        Icon.Folder -> {
            val f = androidx.compose.ui.graphics.Path().apply {
                moveTo(.12f * s, .28f * s); lineTo(.38f * s, .28f * s); lineTo(.46f * s, .36f * s); lineTo(.88f * s, .36f * s)
                lineTo(.88f * s, .8f * s); lineTo(.12f * s, .8f * s); close()
            }
            drawPath(f, color, style = Stroke(width = st.width, join = StrokeJoin.Round))
        }
        Icon.Close -> { line(.26f, .26f, .74f, .74f); line(.74f, .26f, .26f, .74f) }
        Icon.Wifi -> {
            for ((r, sweep) in listOf(.42f to 90f, .28f to 90f)) drawArc(color, 225f, sweep, false,
                Offset((.5f - r) * s, (.72f - r) * s), androidx.compose.ui.geometry.Size(2 * r * s, 2 * r * s), style = st)
            drawCircle(color, .05f * s, Offset(.5f * s, .72f * s))
        }
        Icon.Camera -> {
            drawRoundRect(color, Offset(.12f * s, .3f * s), androidx.compose.ui.geometry.Size(.76f * s, .5f * s),
                androidx.compose.ui.geometry.CornerRadius(.08f * s), style = st)
            drawCircle(color, .13f * s, Offset(.5f * s, .55f * s), style = st)
            line(.38f, .3f, .43f, .2f); line(.43f, .2f, .57f, .2f); line(.57f, .2f, .62f, .3f)
        }
    }
}

/**
 * A column of text and controls at a width you can read on a tablet (640 dp), centered; the
 * whole width on a phone. The background around it stays the screen's.
 */
fun Modifier.readingWidth(): Modifier = this.fillMaxWidth().wrapContentWidth(Alignment.CenterHorizontally).widthIn(max = 640.dp).fillMaxWidth()

/** Width classes: one layout that adapts, never a separate tablet screen. */
enum class Space { Compact, Medium, Expanded;
    companion object {
        fun of(widthDp: Float) = when { widthDp < 600 -> Compact; widthDp < 840 -> Medium; else -> Expanded }
    }
}
