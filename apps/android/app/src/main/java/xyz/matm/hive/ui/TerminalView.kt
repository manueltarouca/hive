package xyz.matm.hive.ui

import android.annotation.SuppressLint
import android.content.Context
import android.graphics.Canvas
import android.graphics.DashPathEffect
import android.graphics.Paint
import android.graphics.Typeface
import android.text.InputType
import android.util.TypedValue
import android.view.GestureDetector
import android.view.KeyEvent
import android.view.MotionEvent
import android.view.ScaleGestureDetector
import android.view.View
import android.view.inputmethod.BaseInputConnection
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputConnection
import android.view.inputmethod.InputMethodManager
import xyz.matm.hive.model.PaneStream
import xyz.matm.hive.vt.Frame
import xyz.matm.hive.vt.Mirror
import xyz.matm.hive.vt.TermTheme
import kotlin.math.abs
import kotlin.math.ceil
import kotlin.math.floor
import kotlin.math.roundToInt
import kotlin.math.max
import kotlin.math.min

/**
 * A pane, drawn on a Canvas from the mirror's frames. Two sizes: the Mac's
 * (the pane's own columns, zoomed to the phone's width; pinch to read
 * closer) or the phone's (fitted: as many columns as fit at a readable
 * size). Taps click for programs that track the mouse, open links, or
 * bring up the keyboard; vertical drags scroll.
 */
/** A face of the terminal font, with text-style symbol fonts before the system's (whose fallback is colour emoji). */
private fun withFallbacks(ctx: Context, file: String): Typeface {
    fun family(f: String) = android.graphics.fonts.FontFamily.Builder(android.graphics.fonts.Font.Builder(ctx.assets, f).build()).build()
    return Typeface.CustomFallbackBuilder(family(file))
        .addCustomFallback(family("NotoSansSymbols2-Regular.ttf"))
        .addCustomFallback(family("NotoSansMath-Regular.ttf"))
        .addCustomFallback(family("NotoSansSymbols-Regular.ttf"))
        .setSystemFallback("monospace")
        .build()
}

@SuppressLint("ViewConstructor")
class TerminalView(context: Context) : View(context) {
    var stream: PaneStream? = null
    var fitted = false
        set(v) { if (field != v) { field = v; zoom = 1f; panX = 0f; requestLayoutMetrics() } }
    /** Called with the phone's columns and rows (and cell size in px) whenever they change. */
    var onPhoneSize: ((Int, Int, Int, Int) -> Unit)? = null
    var onLink: ((String) -> Unit)? = null
    var onSwipe: ((Int) -> Unit)? = null
    /** The first pinch: the size hint has been understood. */
    var onPinched: (() -> Unit)? = null
    /** Ctrl from the key bar: the next typed letter goes as ctrl+letter. */
    var ctrlLatched = false
    var onCtrlUsed: (() -> Unit)? = null

    private var frame: Frame? = null
    private val density = resources.displayMetrics.density
    private val fittedTextPx = TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_SP, 11.5f, resources.displayMetrics)
    private val fonts = arrayOf("Regular", "Bold", "Italic", "BoldItalic").map {
        runCatching { withFallbacks(context, "JetBrainsMonoNerdFont-$it.ttf") }.getOrDefault(Typeface.MONOSPACE)
    }
    /** For text that asks for emoji (VS16): the system's colour emoji. */
    private val emoji = Paint(Paint.ANTI_ALIAS_FLAG).apply { typeface = Typeface.DEFAULT }
    private val text = Paint(Paint.ANTI_ALIAS_FLAG or Paint.SUBPIXEL_TEXT_FLAG).apply { typeface = fonts[0] }
    private val fill = Paint()
    /** Box and Powerline shapes: anti-aliased for the triangles' diagonals (rects are pixel-snapped anyway). */
    private val shapePaint = Paint(Paint.ANTI_ALIAS_FLAG)
    private val widths = HashMap<String, Float>()
    var theme: TermTheme = TermTheme.night
        set(v) {
            if (field === v) return
            field = v
            setBackgroundColor(0xFF000000.toInt() or v.background)
            hint.color = 0x99000000.toInt() or v.foreground
            edge.color = 0x55000000 or v.foreground
            invalidate()
        }
    private val hint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = 0xFF969896.toInt()
        textAlign = Paint.Align.CENTER
        textSize = 12 * density
        typeface = Typeface.create("sans-serif", Typeface.NORMAL)
    }
    private val edge = Paint().apply {
        color = 0x55969896
        style = Paint.Style.STROKE
        strokeWidth = density
        pathEffect = DashPathEffect(floatArrayOf(4 * density, 4 * density), 0f)
    }
    /** The margin inside the pane, so text doesn't touch its border. */
    private val pad = (6 * density).roundToInt().toFloat()
    private val innerW get() = width - 2 * pad
    private val innerH get() = height - pad
    private var cellW = 1f
    private var cellH = 1f
    private var baseline = 1f
    private var zoom = 1f
    private var panX = 0f
    private var scrollAccum = 0f
    private var lastDescribed = 0L

    init {
        isFocusable = true
        isFocusableInTouchMode = true
        setBackgroundColor(0xFF000000.toInt() or theme.background)
        importantForAccessibility = IMPORTANT_FOR_ACCESSIBILITY_YES
    }

    fun show(f: Frame?) {
        val sizeChanged = f?.cols != frame?.cols || f?.rows != frame?.rows
        frame = f
        if (sizeChanged) requestLayoutMetrics()
        describe()
        invalidate()
    }

    /** The pane's text as its description: screen readers, and the e2e checks, read it. */
    private fun describe() {
        val now = System.currentTimeMillis()
        if (now - lastDescribed < 250) { removeCallbacks(describeLater); postDelayed(describeLater, 260); return }
        lastDescribed = now
        contentDescription = frame?.text() ?: ""
    }

    private val describeLater = Runnable { lastDescribed = 0; describe() }

    override fun onSizeChanged(w: Int, h: Int, ow: Int, oh: Int) = requestLayoutMetrics()

    private var phone: IntArray? = null

    private fun requestLayoutMetrics() {
        if (width == 0) return
        // The phone's own size at a readable text size, for Fit to phone.
        setTextSize(fittedTextPx, wholeCells = true)
        val size = intArrayOf(max(20, floor(innerW / cellW).toInt()), max(5, floor(innerH / cellH).toInt()), cellW.toInt(), cellH.toInt())
        if (!size.contentEquals(phone)) { phone = size; onPhoneSize?.invoke(size[0], size[1], size[2], size[3]) }
        val f = frame
        if (!fitted && f != null && f.cols > 0) {
            // The Mac's columns across the phone's width, zoomed by the pinch.
            text.textSize = 100f
            val advance = text.measureText("M")
            // Never larger than the readable size: a narrow pane (a split) stays at it, not blown up.
            setTextSize(minOf(100f * (innerW / f.cols) / advance, fittedTextPx) * zoom)
        }
        invalidate()
    }

    /** Cells as the Mac sizes them (HiveRender/Font.swift): the font's own line, whole pixels high, no extra spacing. */
    private fun setTextSize(px: Float, wholeCells: Boolean = false) {
        text.textSize = px
        emoji.textSize = px
        text.letterSpacing = 0f
        val advance = text.measureText("M")
        cellW = advance
        if (wholeCells) {
            // Whole-pixel cells, as the Mac's: letter spacing (in ems) keeps a run of text on that grid.
            cellW = advance.roundToInt().toFloat()
            text.letterSpacing = (cellW - advance) / px
        }
        widths.clear()
        val fm = text.fontMetrics
        cellH = ceil(fm.descent - fm.ascent + fm.leading)
        baseline = cellH - (fm.descent + fm.leading / 2).roundToInt()
    }

    override fun onDraw(canvas: Canvas) {
        val f = frame ?: return
        val bg = 0xFF000000.toInt() or f.background
        // Compose doesn't clip an AndroidView: stay inside the pane.
        canvas.clipRect(0f, 0f, width.toFloat(), height.toFloat())
        canvas.drawColor(bg)
        canvas.save()
        canvas.clipRect(pad, 0f, width - pad, height.toFloat())
        // Whole pixels, so shapes and backgrounds snapped to them meet without seams.
        canvas.translate(pad - panX.roundToInt(), (pad / 2).roundToInt().toFloat())
        val chars = CharArray(f.cols * 2)
        for (y in 0 until f.rows) {
            val row = f.cells.getOrNull(y) ?: continue
            val top = y * cellH
            if (top > height) break
            // Backgrounds, then text in runs of one style.
            for (x in row.indices) {
                val c = row[x]
                val cbg = cellBg(c, f)
                if (cbg != f.background) {
                    fill.color = 0xFF000000.toInt() or cbg
                    canvas.drawRect((x * cellW).roundToInt().toFloat(), top, ((x + 1) * cellW + if (c.wide == 1) cellW else 0f).roundToInt().toFloat(), top + cellH, fill)
                }
            }
            var x = 0
            while (x < row.size) {
                val c = row[x]
                if (c.spacer || c.text.isEmpty() || c.text == " " || c.invisible) { x++; continue }
                val style = (if (c.bold) 1 else 0) or (if (c.italic) 2 else 0)
                val fg = cellFg(c, f)
                text.typeface = fonts[style]
                text.color = (if (c.faint) 0x99000000.toInt() else 0xFF000000.toInt()) or fg
                text.isStrikeThruText = c.strikethrough
                text.isUnderlineText = c.underline != 0
                if (c.text.length == 1 && c.text[0].code < 0x7F && c.wide == 0) {
                    // A run of plain ASCII in one style: one draw, monospace keeps it on the grid.
                    var n = 0
                    var e = x
                    while (e < row.size) {
                        val d = row[e]
                        if (d.text.length != 1 || d.text[0].code >= 0x7F || d.wide != 0 || d.invisible || d.flags != c.flags ||
                            d.underline != c.underline || cellFg(d, f) != fg) break
                        chars[n++] = d.text[0]
                        e++
                    }
                    canvas.drawText(chars, 0, n, x * cellW, top + baseline, text)
                    x = e
                } else {
                    drawCell(canvas, c.text, c.wide == 1, x * cellW, top)
                    x++
                }
            }
        }
        text.isStrikeThruText = false
        text.isUnderlineText = false
        // The cursor: a block, bar or underline, in the cursor color.
        if (f.cursorVisible && f.cursorY < f.rows) {
            fill.color = 0xFF000000.toInt() or theme.cursor
            val cx = (f.cursorX * cellW).roundToInt().toFloat()
            val cy = f.cursorY * cellH
            val under = f.cells.getOrNull(f.cursorY)?.getOrNull(f.cursorX)
            val cw = if (under?.wide == 1) cellW * 2 else cellW
            when (f.cursorStyle) {
                0 -> canvas.drawRect(cx, cy, cx + max(1f, 1.5f * density), cy + cellH, fill)
                2 -> canvas.drawRect(cx, cy + cellH - 2 * density, cx + cw, cy + cellH, fill)
                3 -> { fill.style = Paint.Style.STROKE; fill.strokeWidth = density; canvas.drawRect(cx, cy, cx + cw, cy + cellH, fill); fill.style = Paint.Style.FILL }
                else -> {
                    canvas.drawRect(cx, cy, cx + cw, cy + cellH, fill)
                    if (under != null && under.text.isNotBlank() && !under.spacer) {
                        // The cell under the cursor, drawn as any cell is, in the background colour.
                        text.typeface = fonts[(if (under.bold) 1 else 0) or (if (under.italic) 2 else 0)]
                        text.color = 0xFF000000.toInt() or f.background
                        if (under.text.length == 1 && under.text[0].code < 0x7F) canvas.drawText(under.text, cx, cy + baseline, text)
                        else drawCell(canvas, under.text, under.wide == 1, cx, cy)
                    }
                }
            }
        }
        canvas.restore()
        // In the Mac's size, below its last row: the pane ends here.
        val end = f.rows * cellH + pad / 2
        if (!fitted && end < height - 24 * density) {
            canvas.drawLine(0f, end, width.toFloat(), end, edge)
            canvas.drawText("Your Mac's pane ends here", width / 2f, end + (height - end) / 2, hint)
        }
    }

    /**
     * One cell's text that isn't plain ASCII: box and block characters as
     * shapes; other glyphs centred in their cells, and scaled down when a
     * fallback font draws them wider (as the Mac's atlas does).
     */
    private fun drawCell(canvas: Canvas, s0: String, wide: Boolean, x: Float, top: Float, paint0: Paint = text) {
        val span = if (wide) cellW * 2 else cellW
        val cp = s0.codePointAt(0)
        if (Character.charCount(cp) == s0.length && BoxDrawing.handles(cp)) {
            shapePaint.color = paint0.color
            BoxDrawing.draw(canvas, cp, x, top, span, cellH, max(1f, (cellW / 8).roundToInt().toFloat()), shapePaint)
            return
        }
        val paint = if (Glyphs.colourEmoji(s0, wide)) emoji.also {
            it.color = paint0.color
            it.isUnderlineText = paint0.isUnderlineText
            it.isStrikeThruText = paint0.isStrikeThruText
        } else paint0
        // Claude Code's ⏺ at the size the Mac shows it: the terminal font's own ●.
        val s = if (s0 == "\u23FA") "\u25CF" else s0
        // Widths are measured once per size and face: this runs for every symbol, every frame.
        val key = if (paint === emoji) "e$s" else "${paint.typeface.hashCode()}$s"
        val w = widths.getOrPut(key) { paint.measureText(s) }
        if (w > span * 1.05f) {
            canvas.save()
            canvas.scale(span / w, span / w, x, top + baseline)
            canvas.drawText(s, x, top + baseline, paint)
            canvas.restore()
        } else {
            canvas.drawText(s, x + max(0f, (span - w) / 2), top + baseline, paint)
        }
    }

    private fun cellBg(c: xyz.matm.hive.vt.Cell, f: Frame): Int {
        if (c.selected) return theme.selection
        if (c.inverse) return if (c.fg >= 0) c.fg else f.foreground
        return if (c.bg >= 0) c.bg else f.background
    }

    private fun cellFg(c: xyz.matm.hive.vt.Cell, f: Frame): Int {
        if (c.inverse) return if (c.bg >= 0) c.bg else f.background
        return if (c.fg >= 0) c.fg else f.foreground
    }

    /** libghostty works in whole-pixel cells: positions given to it are scaled onto a grid of those. */
    private val gridCellW get() = max(1, cellW.roundToInt())

    private fun geometry(): Mirror.Geometry {
        val f = frame
        return Mirror.Geometry((f?.cols ?: 1) * gridCellW, ((f?.rows ?: 1) * cellH).toInt(), gridCellW, cellH.toInt())
    }

    private fun gridX(x: Float) = x / cellW * gridCellW

    // MARK: touch

    private val gestures = GestureDetector(context, object : GestureDetector.SimpleOnGestureListener() {
        override fun onDown(e: MotionEvent) = true

        override fun onSingleTapUp(e: MotionEvent): Boolean {
            val f = frame ?: return true
            val x = e.x - pad + panX
            val y = e.y - pad / 2
            val col = (x / cellW).toInt().coerceIn(0, max(0, f.cols - 1))
            val row = (y / cellH).toInt()
            if (row >= f.rows || y < 0) { showKeyboard(); return true }
            val s = stream ?: return true
            s.tap(col, row, gridX(x), y, geometry()) { link ->
                if (link != null) onLink?.invoke(link) else showKeyboard()
            }
            return true
        }

        override fun onScroll(e1: MotionEvent?, e2: MotionEvent, dx: Float, dy: Float): Boolean {
            if (abs(dx) > abs(dy) && zoom > 1f) {
                val f = frame ?: return true
                panX = (panX + dx).coerceIn(0f, max(0f, f.cols * cellW - innerW))
                invalidate()
                return true
            }
            if (abs(dy) <= abs(dx)) return true
            scrollAccum += dy
            val rows = (scrollAccum / cellH).toInt()
            if (rows != 0) {
                scrollAccum -= rows * cellH
                stream?.scroll(rows, gridX(e2.x - pad + panX), e2.y - pad / 2, geometry())
            }
            return true
        }

        override fun onFling(e1: MotionEvent?, e2: MotionEvent, vx: Float, vy: Float): Boolean {
            // Sideways, unzoomed: the next or previous pane of the split.
            if (zoom <= 1f && abs(vx) > 2 * abs(vy) && abs(vx) > 800 && e1 != null && abs(e2.x - e1.x) > 80 * density) {
                onSwipe?.invoke(if (vx < 0) 1 else -1)
                return true
            }
            return false
        }
    })

    private val scaler = ScaleGestureDetector(context, object : ScaleGestureDetector.SimpleOnScaleGestureListener() {
        override fun onScale(d: ScaleGestureDetector): Boolean {
            if (fitted) return true
            onPinched?.invoke()
            val old = zoom
            zoom = (zoom * d.scaleFactor).coerceIn(1f, 4f)
            panX = ((panX + d.focusX) * zoom / old - d.focusX).coerceAtLeast(0f)
            requestLayoutMetrics()
            val f = frame
            if (f != null) panX = panX.coerceIn(0f, max(0f, f.cols * cellW - innerW))
            return true
        }
    })

    @SuppressLint("ClickableViewAccessibility")
    override fun onTouchEvent(e: MotionEvent): Boolean {
        scaler.onTouchEvent(e)
        if (!scaler.isInProgress) gestures.onTouchEvent(e)
        return true
    }

    // MARK: keyboard

    fun showKeyboard() {
        requestFocus()
        context.getSystemService(InputMethodManager::class.java).showSoftInput(this, 0)
    }

    override fun onCheckIsTextEditor() = true

    override fun onCreateInputConnection(out: EditorInfo): InputConnection {
        // As terminals do: no suggestions or corrections, each key committed as typed.
        out.inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
        out.imeOptions = EditorInfo.IME_FLAG_NO_EXTRACT_UI or EditorInfo.IME_FLAG_NO_FULLSCREEN or EditorInfo.IME_ACTION_NONE
        val composer = Composer()
        return object : BaseInputConnection(this, false) {
            override fun commitText(t: CharSequence, newCursorPosition: Int): Boolean {
                typed(composer.commit(t.toString()))
                return true
            }

            override fun setComposingText(t: CharSequence, newCursorPosition: Int): Boolean {
                composer.compose(t.toString())
                return true
            }

            override fun finishComposingText(): Boolean {
                typed(composer.finish())
                return true
            }

            override fun deleteSurroundingText(before: Int, after: Int): Boolean {
                repeat(composer.delete(max(1, before))) { stream?.key("backspace") }
                return true
            }

            override fun sendKeyEvent(event: KeyEvent): Boolean {
                if (event.action == KeyEvent.ACTION_DOWN) onKeyDown(event.keyCode, event)
                return true
            }
        }
    }

    /** Text from the keyboard: enter as a return, a latched ctrl applied to the first letter. */
    private fun typed(s: String) {
        val st = stream ?: return
        if (s.isEmpty()) return
        if (ctrlLatched) {
            ctrlLatched = false
            onCtrlUsed?.invoke()
            val c = s[0].lowercaseChar()
            st.key("ctrl+$c")
            if (s.length > 1) typed(s.substring(1))
            return
        }
        st.text(s.replace("\n", "\r"))
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean {
        val st = stream ?: return super.onKeyDown(keyCode, event)
        if (keyCode == KeyEvent.KEYCODE_BACK) return super.onKeyDown(keyCode, event)
        var mods = 0
        if (event.isShiftPressed) mods = mods or 1
        if (event.isCtrlPressed || ctrlLatched) mods = mods or 2
        if (event.isAltPressed) mods = mods or 4
        if (event.isMetaPressed) mods = mods or 8
        val ch = event.getUnicodeChar(event.metaState and (KeyEvent.META_SHIFT_MASK or KeyEvent.META_ALT_MASK or KeyEvent.META_CAPS_LOCK_ON))
        val text = if (ch > 0x1F && mods and 2 == 0) String(Character.toChars(ch)) else ""
        val unshifted = event.getUnicodeChar(0)
        if (ctrlLatched) { ctrlLatched = false; onCtrlUsed?.invoke() }
        st.androidKey(keyCode, mods, text, unshifted)
        return true
    }

    override fun onKeyUp(keyCode: Int, event: KeyEvent) = keyCode != KeyEvent.KEYCODE_BACK || super.onKeyUp(keyCode, event)
}
