package xyz.matm.hive.ui

import android.graphics.BitmapFactory
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.clipPath
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.graphics.vector.PathParser
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import xyz.matm.hive.agents.Program
import xyz.matm.hive.agents.ProgramIcon
import xyz.matm.hive.agents.Programs
import kotlin.math.cos
import kotlin.math.hypot
import kotlin.math.min
import kotlin.math.roundToInt
import kotlin.math.sin

/** Decoded once: vendor icons and logo paths. */
private object IconCache {
    val vendors = HashMap<String, ImageBitmap?>()
    val logos = HashMap<String, Path?>()
    private val cells = HashMap<Long, Path>()

    /** A cell's path, made once per size: tabs draw on every frame. */
    fun cell(w: Float, h: Float): Path = cells.getOrPut(w.toRawBits().toLong() shl 32 or h.toRawBits().toLong().and(0xFFFFFFFF)) { hexCell(w, h) }

    fun vendor(name: String) = vendors.getOrPut(name) {
        Programs.vendorPng(name)?.let { BitmapFactory.decodeByteArray(it, 0, it.size)?.asImageBitmap() }
    }

    fun logo(slug: String) = logos.getOrPut(slug) { Programs.brandPath(slug)?.let { PathParser().parsePathString(it).toPath() } }
}

/** A honeycomb cell in a w × h box: a hexagon on a point, side corners at the box's edges, rounded (HexCell on the Mac). */
fun hexCell(w: Float, h: Float, rounding: Float = 0.12f, inset: Float = 0f): Path {
    val hh = h - 2 * inset
    val ww = min(w - 2 * inset * 0.866f, hh * 0.866f)
    val cx = w / 2
    val cy = h / 2
    val corners = (0 until 6).map { i ->
        val a = Math.toRadians(60.0 * i - 90)
        Offset(cx + (cos(a) * (ww / 2) / 0.866).toFloat(), cy + (sin(a) * hh / 2).toFloat())
    }
    val r = hh * rounding
    fun toward(a: Offset, b: Offset, d: Float): Offset {
        val len = hypot(b.x - a.x, b.y - a.y)
        return Offset(a.x + (b.x - a.x) * d / len, a.y + (b.y - a.y) * d / len)
    }
    val p = Path()
    for (i in 0 until 6) {
        val c = corners[i]
        val start = toward(c, corners[(i + 5) % 6], r)
        val end = toward(c, corners[(i + 1) % 6], r)
        if (i == 0) p.moveTo(start.x, start.y) else p.lineTo(start.x, start.y)
        p.quadraticTo(c.x, c.y, end.x, end.y)
    }
    p.close()
    return p
}

/**
 * A program as a honeycomb cell, as in the Mac's tab bar: its vendor's icon,
 * else its logo on its brand colour, else a symbol on graphite. A tab with
 * several panes is a small comb: two cells joined on a slanted edge, three
 * as a triangle with the tab's own program on top.
 */
@Composable
fun ProgramTile(program: Program, size: Dp = 18.dp, behind: List<Program> = emptyList(), modifier: Modifier = Modifier) {
    val cells = listOf(program) + behind.take(2)
    // Comb.layout (HiveCore/Comb.swift).
    val h = if (cells.size == 1) size.value else size.value / 1.75f
    val w = h * 0.866f
    val origins = when (cells.size) {
        1 -> listOf(Offset.Zero)
        2 -> listOf(Offset.Zero, Offset(w / 2, h * 0.75f))
        else -> listOf(Offset(w / 2, 0f), Offset(0f, h * 0.75f), Offset(w, h * 0.75f))
    }
    val width = when (cells.size) { 1 -> w; 2 -> w * 1.5f; else -> w * 2 }
    val label = if (cells.size > 1) "${program.name} and ${cells.size - 1} more" else program.name
    Canvas(modifier.size(width.dp, size).semantics { contentDescription = label }) {
        val px = this.size.height / size.value
        // Drawn back to front: the tab's own program last, on top.
        for (i in cells.indices.reversed()) {
            // 0.94 of the frame, centred in it: a hairline wall between cells, as on the Mac.
            translate(origins[i].x * px, origins[i].y * px + h * px * 0.03f) { cellFace(cells[i], w * px, h * px * 0.94f) }
        }
    }
}

/** One cell with the depth of an app icon: fill and mark, gloss, outline. */
private fun DrawScope.cellFace(program: Program, w: Float, h: Float) {
    val cell = IconCache.cell(w, h)
    val vendor = program.vendor?.let(IconCache::vendor)
    clipPath(cell) {
        if (vendor != null) {
            val bg = program.vendor?.let(Programs::vendorBackground)
            if (bg != null) {
                drawRect(Color(0xFF000000.toInt() or bg))
                val s = w * 0.62f
                drawImage(vendor, dstOffset = IntOffset(((w - s) / 2).roundToInt(), ((h - s) / 2).roundToInt()),
                    dstSize = IntSize(s.roundToInt(), s.roundToInt()))
            } else {
                // A full icon: fill the cell.
                drawImage(vendor, dstOffset = IntOffset(((w - h) / 2).roundToInt(), 0), dstSize = IntSize(h.roundToInt(), h.roundToInt()))
            }
        } else {
            val brand = (program.icon as? ProgramIcon.Brand)?.slug?.takeIf { Programs.brandPath(it) != null }
            val fill = brand?.let(Programs::fill) ?: 0x2C2D31
            val glyph = if (brand != null) Programs.onFill(fill) else 0xFFFFFF
            drawRect(Color(0xFF000000.toInt() or fill))
            val g = w * 0.56f
            translate((w - g) / 2, (h - g) / 2) { programMark(program, Color(0xFF000000.toInt() or glyph), g) }
        }
        // Gloss: light from above, fading by the middle.
        drawRect(Brush.verticalGradient(0f to Color.White.copy(alpha = 0.22f), 0.5f to Color.Transparent, 1f to Color.Black.copy(alpha = 0.10f), startY = 0f, endY = h))
    }
    drawPath(cell, Color.Black.copy(alpha = 0.35f), style = Stroke(width = 0.75.dp.toPx()))
}

/** A program's mark alone, in one colour: pane headers draw it in the text colour, as the Mac does. */
@Composable
fun ProgramGlyph(program: Program, color: Color, size: Dp = 13.dp, modifier: Modifier = Modifier) {
    Box(modifier.size(size)) {
        Canvas(Modifier.size(size)) { programMark(program, color, this.size.minDimension) }
    }
}

private fun DrawScope.programMark(program: Program, color: Color, s: Float) {
    when (val icon = program.icon) {
        is ProgramIcon.Brand -> IconCache.logo(icon.slug)?.let { path ->
            scale(s / 24f, s / 24f, pivot = Offset.Zero) { drawPath(path, color) }
        } ?: symbol("apple.terminal", color, s)
        is ProgramIcon.Symbol -> symbol(icon.name, color, s)
    }
}

/** The Mac's SF Symbols, as simple line drawings on a 24 grid. */
private fun DrawScope.symbol(name: String, color: Color, s: Float) {
    val k = s / 24f
    val st = Stroke(width = 2.2f * k, cap = StrokeCap.Round, join = StrokeJoin.Round)
    fun line(vararg pts: Float) {
        val p = Path()
        p.moveTo(pts[0] * k, pts[1] * k)
        var i = 2
        while (i < pts.size) { p.lineTo(pts[i] * k, pts[i + 1] * k); i += 2 }
        drawPath(p, color, style = st)
    }
    fun hex(fill: Boolean) {
        val p = hexCell(20 * k, 22 * k, rounding = 0.1f)
        translate(2 * k, 1 * k) { if (fill) drawPath(p, color) else drawPath(p, color, style = st) }
    }
    when (name) {
        "apple.terminal" -> {
            drawRoundRect(color, Offset(2 * k, 4 * k), Size(20 * k, 16 * k), androidx.compose.ui.geometry.CornerRadius(3 * k), style = st)
            line(6f, 9f, 9.5f, 12f, 6f, 15f); line(12f, 15.5f, 17f, 15.5f)
        }
        "bolt" -> {
            val p = Path().apply { moveTo(13 * k, 2 * k); lineTo(5 * k, 13.5f * k); lineTo(11.5f * k, 13.5f * k); lineTo(10.5f * k, 22 * k); lineTo(19 * k, 10 * k); lineTo(12.5f * k, 10 * k); close() }
            drawPath(p, color)
        }
        "chart.bar.xaxis" -> { line(3f, 21f, 21f, 21f); line(7f, 18f, 7f, 12f); line(12f, 18f, 12f, 6f); line(17f, 18f, 17f, 9f) }
        "chevron.left.forwardslash.chevron.right" -> { line(7f, 7f, 2.5f, 12f, 7f, 17f); line(17f, 7f, 21.5f, 12f, 17f, 17f); line(14f, 4f, 10f, 20f) }
        "hexagon" -> hex(false)
        "hexagon.fill" -> hex(true)
        "network" -> {
            drawCircle(color, 9.5f * k, Offset(12 * k, 12 * k), style = st)
            line(2.5f, 12f, 21.5f, 12f)
            drawOval(color, Offset(7.5f * k, 2.5f * k), Size(9 * k, 19 * k), style = st)
        }
        "shippingbox" -> { line(3f, 7.5f, 12f, 3f, 21f, 7.5f, 21f, 17f, 12f, 21.5f, 3f, 17f, 3f, 7.5f); line(3f, 7.5f, 12f, 12f, 21f, 7.5f); line(12f, 12f, 12f, 21.5f) }
        "sparkles" -> {
            fun star(cx: Float, cy: Float, r: Float) {
                val p = Path().apply {
                    moveTo(cx * k, (cy - r) * k); quadraticTo(cx * k, cy * k, (cx + r) * k, cy * k)
                    quadraticTo(cx * k, cy * k, cx * k, (cy + r) * k); quadraticTo(cx * k, cy * k, (cx - r) * k, cy * k)
                    quadraticTo(cx * k, cy * k, cx * k, (cy - r) * k); close()
                }
                drawPath(p, color)
            }
            star(10f, 13f, 8.5f); star(18.5f, 5.5f, 4f)
        }
        "square.stack.3d.up" -> { line(12f, 3f, 21f, 7.5f, 12f, 12f, 3f, 7.5f, 12f, 3f); line(3f, 12f, 12f, 16.5f, 21f, 12f); line(3f, 16.5f, 12f, 21f, 21f, 16.5f) }
        else -> symbol("apple.terminal", color, s)
    }
}
