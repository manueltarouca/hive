package xyz.matm.hive.ui

import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.Path
import kotlin.math.floor
import kotlin.math.roundToInt

/**
 * Box-drawing, block and Powerline characters drawn as shapes rather than
 * font glyphs, so they fill their cells exactly and TUI frames join without
 * gaps, whatever the font. The Mac app's BoxDrawing.swift, with y down.
 */
object BoxDrawing {
    data class Rect(val left: Float, val top: Float, val right: Float, val bottom: Float)

    /** What a character is made of: rects at an alpha, or a Powerline triangle (1 ▶, 2 ◀ solid; 3, 4 thin). */
    class Shapes(val rects: List<Rect>, val alpha: Float = 1f, val triangle: Int = 0)

    fun handles(cp: Int): Boolean = when (cp) {
        in 0x2500..0x257F -> lines.containsKey(cp)
        in 0x2580..0x259F, in 0xE0B0..0xE0B3, 0x23BF -> true
        else -> false
    }

    /** Line weights toward (up, right, down, left): 0 none, 1 light, 2 heavy, 3 double. */
    private val lines: Map<Int, IntArray> = HashMap<Int, IntArray>().apply {
        fun l(cp: Int, u: Int, r: Int, d: Int, lf: Int) { put(cp, intArrayOf(u, r, d, lf)) }
        l(0x2500, 0, 1, 0, 1); l(0x2501, 0, 2, 0, 2); l(0x2502, 1, 0, 1, 0); l(0x2503, 2, 0, 2, 0)
        l(0x250C, 0, 1, 1, 0); l(0x250F, 0, 2, 2, 0); l(0x2510, 0, 0, 1, 1); l(0x2513, 0, 0, 2, 2)
        l(0x2514, 1, 1, 0, 0); l(0x2517, 2, 2, 0, 0); l(0x2518, 1, 0, 0, 1); l(0x251B, 2, 0, 0, 2)
        l(0x251C, 1, 1, 1, 0); l(0x2523, 2, 2, 2, 0); l(0x2524, 1, 0, 1, 1); l(0x252B, 2, 0, 2, 2)
        l(0x252C, 0, 1, 1, 1); l(0x2533, 0, 2, 2, 2); l(0x2534, 1, 1, 0, 1); l(0x253B, 2, 2, 0, 2)
        l(0x253C, 1, 1, 1, 1); l(0x254B, 2, 2, 2, 2)
        l(0x2550, 0, 3, 0, 3); l(0x2551, 3, 0, 3, 0); l(0x2554, 0, 3, 3, 0); l(0x2557, 0, 0, 3, 3)
        l(0x255A, 3, 3, 0, 0); l(0x255D, 3, 0, 0, 3); l(0x2560, 3, 3, 3, 0); l(0x2563, 3, 0, 3, 3)
        l(0x2566, 0, 3, 3, 3); l(0x2569, 3, 3, 0, 3); l(0x256C, 3, 3, 3, 3)
        // Rounded corners, drawn square at this size.
        l(0x256D, 0, 1, 1, 0); l(0x256E, 0, 0, 1, 1); l(0x256F, 1, 0, 0, 1); l(0x2570, 1, 1, 0, 0)
        l(0x2574, 0, 0, 0, 1); l(0x2575, 1, 0, 0, 0); l(0x2576, 0, 1, 0, 0); l(0x2577, 0, 0, 1, 0)
        l(0x2578, 0, 0, 0, 2); l(0x2579, 2, 0, 0, 0); l(0x257A, 0, 2, 0, 0); l(0x257B, 0, 0, 2, 0)
        // Dashed lines draw solid.
        for ((d, s) in listOf(0x2504 to 0x2500, 0x2505 to 0x2501, 0x2506 to 0x2502, 0x2507 to 0x2503,
            0x2508 to 0x2500, 0x2509 to 0x2501, 0x250A to 0x2502, 0x250B to 0x2503,
            0x254C to 0x2500, 0x254D to 0x2501, 0x254E to 0x2502, 0x254F to 0x2503)) put(d, getValue(s))
    }

    /** Quadrant bits for ▖ through ▟. */
    private val quadrants = intArrayOf(0b0010, 0b0001, 0b1000, 0b1011, 0b1001, 0b1110, 0b1101, 0b0100, 0b0110, 0b0111)
    private val shades = floatArrayOf(0.25f, 0.5f, 0.75f)

    // Shapes for the current cell size: drawing runs every frame, so they're made once per size.
    private val cache = HashMap<Long, Shapes>()
    private var cachedFor = 0f to 0f
    private val path = Path()

    private fun cached(cp: Int, w: Float, h: Float, t: Float): Shapes {
        if (cachedFor != h to t) { cache.clear(); cachedFor = h to t }
        // Keyed by the width too: a wide cell is twice as wide.
        return cache.getOrPut(cp.toLong() shl 32 or w.toRawBits().toLong().and(0xFFFFFFFF)) { shapes(cp, w, h, t) }
    }

    fun shapes(cp: Int, w: Float, h: Float, t: Float): Shapes {
        lines[cp]?.let { return Shapes(lineRects(it, w, h, t)) }
        val full = Rect(0f, 0f, w, h)
        return when (cp) {
            0x2580 -> Shapes(listOf(Rect(0f, 0f, w, h / 2)))                                    // ▀
            in 0x2581..0x2588 -> {                                                              // ▁…█
                val k = (h * (cp - 0x2580) / 8f).roundToInt().toFloat()
                Shapes(listOf(Rect(0f, h - k, w, h)))
            }
            in 0x2589..0x258F -> Shapes(listOf(Rect(0f, 0f, (w * (0x2590 - cp) / 8f).roundToInt().toFloat(), h))) // ▉…▏
            0x2590 -> Shapes(listOf(Rect(w / 2, 0f, w, h)))                                     // ▐
            0x2591, 0x2592, 0x2593 -> Shapes(listOf(full), alpha = shades[cp - 0x2591]) // ░▒▓
            0x2594 -> Shapes(listOf(Rect(0f, 0f, w, (h / 8).roundToInt().toFloat())))           // ▔
            0x2595 -> Shapes(listOf(Rect(w - (w / 8).roundToInt(), 0f, w, h)))                  // ▕
            in 0x2596..0x259F -> {
                // Quadrants: upper-left 8, upper-right 4, lower-left 2, lower-right 1.
                val bits = quadrants[cp - 0x2596]
                Shapes(buildList {
                    if (bits and 0b1000 != 0) add(Rect(0f, 0f, w / 2, h / 2))
                    if (bits and 0b0100 != 0) add(Rect(w / 2, 0f, w, h / 2))
                    if (bits and 0b0010 != 0) add(Rect(0f, h / 2, w / 2, h))
                    if (bits and 0b0001 != 0) add(Rect(w / 2, h / 2, w, h))
                })
            }
            // ⎿, before Claude Code's tool results: down from the line above, then right at the baseline.
            0x23BF -> {
                val x = (w * 0.35f).roundToInt().toFloat()
                val y = (h * 0.72f).roundToInt().toFloat()
                Shapes(listOf(Rect(x, 0f, x + t, y + t), Rect(x, y, w, y + t)))
            }
            0xE0B0 -> Shapes(emptyList(), triangle = 1)
            0xE0B2 -> Shapes(emptyList(), triangle = 2)
            0xE0B1 -> Shapes(emptyList(), triangle = 3)
            0xE0B3 -> Shapes(emptyList(), triangle = 4)
            else -> Shapes(emptyList())
        }
    }

    /** Each arm runs from the cell edge to the centre, overlapping by half a line so arms of different weights meet. */
    private fun lineRects(l: IntArray, w: Float, h: Float, t: Float): List<Rect> {
        val cx = floor(w / 2)
        val cy = floor(h / 2)
        val out = ArrayList<Rect>()
        fun arm(weight: Int, horizontal: Boolean, towardStart: Boolean) {
            if (weight == 0) return
            val k = if (weight == 2) t * 2 else t
            val over = if (weight == 3) t else k / 2
            for (o in if (weight == 3) listOf(-t, t) else listOf(0f)) {
                if (horizontal) {
                    val y = cy + o - k / 2
                    out += if (towardStart) Rect(0f, y, cx + over, y + k) else Rect(cx - over, y, w, y + k)
                } else {
                    val x = cx + o - k / 2
                    out += if (towardStart) Rect(x, 0f, x + k, cy + over) else Rect(x, cy - over, x + k, h)
                }
            }
        }
        arm(l[0], horizontal = false, towardStart = true)  // up
        arm(l[1], horizontal = true, towardStart = false)  // right
        arm(l[2], horizontal = false, towardStart = false) // down
        arm(l[3], horizontal = true, towardStart = true)   // left
        return out
    }

    private fun snap(v: Float) = Math.round(v).toFloat()

    /** Draws [cp] into the cell at (x, y). [paint] has the cell's colour. */
    fun draw(canvas: Canvas, cp: Int, x: Float, y: Float, w: Float, h: Float, t: Float, paint: Paint) {
        val s = cached(cp, w, h, t)
        val alpha = paint.alpha
        if (s.alpha < 1f) paint.alpha = (alpha * s.alpha).roundToInt()
        // On whole pixels: cells are fractional wide when zoomed, and soft edges would show seams between them.
        for (r in s.rects) canvas.drawRect(snap(x + r.left), snap(y + r.top), snap(x + r.right), snap(y + r.bottom), paint)
        paint.alpha = alpha
        if (s.triangle != 0) {
            val right = s.triangle == 1 || s.triangle == 3
            val p = path.apply {
                rewind()
                moveTo(x + if (right) 0f else w, y)
                lineTo(x + if (right) w else 0f, y + h / 2)
                lineTo(x + if (right) 0f else w, y + h)
                if (s.triangle <= 2) close()
            }
            val style = paint.style
            val width = paint.strokeWidth
            if (s.triangle > 2) { paint.style = Paint.Style.STROKE; paint.strokeWidth = t }
            canvas.drawPath(p, paint)
            paint.style = style
            paint.strokeWidth = width
        }
    }
}
