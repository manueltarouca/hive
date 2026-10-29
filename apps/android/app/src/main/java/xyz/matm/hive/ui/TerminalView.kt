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
 * size). Taps click for programs that track the mouse or open links, and
 * never bring up the keyboard (the key bar's keyboard key does, and a
 * two-finger tap); vertical drags scroll; a long press selects text.
 */
/** A face of the terminal font, with text-style symbol fonts before the system's (whose fallback is colour emoji). */
internal fun withFallbacks(ctx: Context, file: String): Typeface {
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
    /** Called with this device's columns and rows for the pane (and cell size in px) whenever they change. */
    var onPhoneSize: ((Int, Int, Int, Int) -> Unit)? = null
    /** The user acted on the pane (tap, scroll, key): it gets the focus, and a wide screen takes its size. */
    var onUsed: (() -> Unit)? = null
    /** Selecting in a pane focuses it (side by side), so its Paste goes there. */
    var onFocus: (() -> Unit)? = null
    /** A two-finger tap: show or hide the keyboard. */
    var onTwoFingerTap: (() -> Unit)? = null
    /** The keyboard came or went because of this view (true: wanted). */
    var onKeyboardWanted: ((Boolean) -> Unit)? = null
    /** Selection colours: honey, translucent, over the cells (Night and Day differ in strength). */
    var selectionColor = 0x55F2A93B
    var handleColor = 0xFFF2A93B.toInt()
    var onLink: ((String) -> Unit)? = null
    /** A tap on a path a program printed (not a link): the path, as shown. */
    var onPath: ((String) -> Unit)? = null
    /** The path being viewed, lit in the pane until the viewer closes. */
    var lit: Selection? = null
        set(v) { field = v; invalidate() }
    /** What was lit before the last tap, for [unlight]. */
    private var litBefore: Selection? = null
    private var myTap = 0

    /** The last tapped path (in any pane) wasn't a file: if it was this pane's, light what was before. */
    fun unlight() { if (myTap != 0 && myTap == lastPathTap) { myTap = 0; lit = litBefore } }
    var litColor = 0x38F2A93B
    var onSwipe: ((Int) -> Unit)? = null
    /** The first pinch: the size hint has been understood. */
    var onPinched: (() -> Unit)? = null
    /** Modifiers from the key bar: "ctrl", "alt" or both, for the next typed key (or every one while locked). */
    var mods: () -> String = { "" }
    /** A typed key used the latched modifiers: a once-only latch lets go. */
    var onModsUsed: (() -> Unit)? = null

    private var frame: Frame? = null
    private val density = resources.displayMetrics.density
    private var fittedTextPx = TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_SP, 11.5f, resources.displayMetrics)
    /** The text size when fitted to the phone (Settings), in sp. */
    var textSp = 11.5f
        set(v) {
            if (field == v) return
            field = v
            fittedTextPx = TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_SP, v, resources.displayMetrics)
            requestLayoutMetrics()
        }
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
        // The pane keeps focus for a hardware keyboard; Android's focus veil (a white wash in keyboard
        // navigation mode) would grey the whole terminal. Its honey border says it's focused.
        defaultFocusHighlightEnabled = false
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
        lit?.let { drawLit(canvas, it) }
        selection?.let { drawSelection(canvas, it) }
        // In the Mac's size, below its last row: the pane ends here.
        val end = f.rows * cellH + pad / 2
        if (!fitted && end < height - 24 * density) {
            canvas.drawLine(0f, end, width.toFloat(), end, edge)
            canvas.drawText("The pane ends here", width / 2f, end + (height - end) / 2, hint)
        }
    }

    /** The viewed path: a honey tint and a line under it, no handles. */
    private fun drawLit(canvas: Canvas, s: Selection) {
        fill.color = litColor
        for (r in s.startRow..s.endRow) {
            val a = if (r == s.startRow) s.startCol else 0
            val b = if (r == s.endRow) s.endCol else (frame?.cols ?: 1) - 1
            canvas.drawRect(sx(a), sy(r), sx(b + 1), sy(r + 1), fill)
            fill.color = handleColor
            canvas.drawRect(sx(a), sy(r + 1) - density, sx(b + 1), sy(r + 1), fill)
            fill.color = litColor
        }
    }

    private fun drawSelection(canvas: Canvas, s: Selection) {
        fill.color = selectionColor
        for (r in s.startRow..s.endRow) {
            val a = if (r == s.startRow) s.startCol else 0
            val b = if (r == s.endRow) s.endCol else (frame?.cols ?: 1) - 1
            canvas.drawRect(sx(a), sy(r), sx(b + 1), sy(r + 1), fill)
        }
        // Handles: Android's teardrops in honey, their points at the selection's ends; the start's
        // hangs to the left, the end's to the right, so two on one word never cover each other.
        fill.color = handleColor
        val r = handleR
        for ((x, row, left) in listOf(Triple(sx(s.startCol), s.startRow, true), Triple(sx(s.endCol + 1), s.endRow, false))) {
            val top = sy(row + 1)
            val cx = if (left) x - r else x + r
            canvas.drawCircle(cx, top + r, r, fill)
            if (left) canvas.drawRect(x - r, top, x, top + r, fill) else canvas.drawRect(x, top, x + r, top + r, fill)
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
            // A tap with text selected only lets go of it.
            if (selection != null) { clearSelection(); return true }
            onUsed?.invoke()
            val f = frame ?: return true
            val x = e.x - pad + panX
            val y = e.y - pad / 2
            val col = (x / cellW).toInt().coerceIn(0, max(0, f.cols - 1))
            val row = (y / cellH).toInt()
            if (row >= f.rows || y < 0) return true
            // A click for a program that tracks the mouse, or the link under the finger. Never the
            // keyboard: a tap to read or aim mustn't cover half the screen.
            val s = stream ?: return true
            s.tap(col, row, gridX(x), y, geometry()) { link ->
                if (link != null) { onLink?.invoke(link); return@tap }
                // No link: a path in plain text (what agents print), read across wrapped rows.
                val hit = frame?.let { PathAt.hit(it, col, row) } ?: return@tap
                litBefore = lit
                myTap = ++lastPathTap
                lit = hit.second
                onPath?.invoke(hit.first)
            }
            return true
        }

        override fun onLongPress(e: MotionEvent) {
            if (multi) return
            val f = frame ?: return
            onFocus?.invoke()
            val (col, row) = cellAt(e.x, e.y, f) ?: return
            val (a, b) = Selection.word(f, col, row)
            selection = Selection(row, a, row, b)
            anchor = row to a
            anchorEnd = row to b
            extending = true
            stopActions()
            performHapticFeedback(android.view.HapticFeedbackConstants.LONG_PRESS)
            invalidate()
        }

        override fun onScroll(e1: MotionEvent?, e2: MotionEvent, dx: Float, dy: Float): Boolean {
            if (abs(dx) > abs(dy) && zoom > 1f) {
                val f = frame ?: return true
                panX = (panX + dx).coerceIn(0f, max(0f, f.cols * cellW - innerW))
                invalidate()
                return true
            }
            if (abs(dy) <= abs(dx)) return true
            if (selection != null) clearSelection()
            onUsed?.invoke()
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

    // MARK: selection and two-finger tap

    /** The selected cells, rows top to bottom, in the frame's coordinates; null when none. */
    private var selection: Selection? = null
    /** Where a long press started: the word it selected, kept while the finger extends it. */
    private var anchor = 0 to 0
    private var anchorEnd = 0 to 0
    /** Long pressed and not lifted yet: moving extends the selection. */
    private var extending = false
    /** A handle being dragged: 1 the start, 2 the end. */
    private var dragging = 0
    private var actions: android.view.ActionMode? = null
    private val slop = android.view.ViewConfiguration.get(context).scaledTouchSlop
    private var twoDown = 0L
    private var twoMoved = false
    private var twoX = FloatArray(2)
    private var twoY = FloatArray(2)
    private var scaled = false

    private fun cellAt(px: Float, py: Float, f: Frame): Pair<Int, Int>? {
        val x = px - pad + panX
        val y = py - pad / 2
        if (y < 0) return null
        val row = (y / cellH).toInt()
        if (row >= f.rows) return null
        return (x / cellW).toInt().coerceIn(0, max(0, f.cols - 1)) to row
    }

    /** Selected text, as shown: one line per row, trailing spaces dropped. */
    fun selectedText(): String? {
        val f = frame ?: return null
        return selection?.text(f)
    }

    fun clearSelection() {
        if (selection == null) return
        selection = null
        extending = false
        dragging = 0
        stopActions()
        invalidate()
    }

    private fun stopActions() { actions?.finish(); actions = null }

    /** Screen x and y of a cell's corner (the frame's coordinates to the view's). */
    private fun sx(col: Int) = pad - panX + col * cellW
    private fun sy(row: Int) = pad / 2 + row * cellH

    private val handleR get() = 9 * density

    /** Which handle is under the finger: 1 the start's, 2 the end's, 0 none. Generous, for a thumb. */
    private fun handleAt(x: Float, y: Float): Int {
        val s = selection ?: return 0
        val reach = 26 * density
        val hx1 = sx(s.startCol) - handleR; val hy1 = sy(s.startRow + 1) + handleR
        val hx2 = sx(s.endCol + 1) + handleR; val hy2 = sy(s.endRow + 1) + handleR
        val d1 = Math.hypot((x - hx1).toDouble(), (y - hy1).toDouble())
        val d2 = Math.hypot((x - hx2).toDouble(), (y - hy2).toDouble())
        return when {
            d2 <= reach && d2 <= d1 -> 2
            d1 <= reach -> 1
            else -> 0
        }
    }

    /** The floating Copy / Select all / Paste bar over the selection, Android's own. */
    private fun showActions() {
        if (selection == null) return
        stopActions()
        actions = startActionMode(object : android.view.ActionMode.Callback2() {
            override fun onCreateActionMode(mode: android.view.ActionMode, menu: android.view.Menu): Boolean {
                menu.add(0, 1, 0, android.R.string.copy)
                menu.add(0, 2, 1, android.R.string.selectAll)
                menu.add(0, 3, 2, android.R.string.paste)
                return true
            }
            override fun onPrepareActionMode(mode: android.view.ActionMode, menu: android.view.Menu) = false
            override fun onActionItemClicked(mode: android.view.ActionMode, item: android.view.MenuItem): Boolean {
                when (item.itemId) {
                    1 -> { selectedText()?.let { onCopy?.invoke(it) }; clearSelection() }
                    2 -> { frame?.let { f -> selection = Selection.all(f); invalidate(); post { showActions() } } }
                    3 -> { clearSelection(); onPaste?.invoke() }
                }
                return true
            }
            override fun onDestroyActionMode(mode: android.view.ActionMode) { if (actions === mode) actions = null }
            override fun onGetContentRect(mode: android.view.ActionMode, view: View, out: android.graphics.Rect) {
                val s = selection ?: return out.set(0, 0, width, 0)
                out.set(0, sy(s.startRow).toInt(), width, sy(s.endRow + 1).toInt())
            }
        }, android.view.ActionMode.TYPE_FLOATING)
    }

    /** Copy (the selection's text) and Paste, from the floating bar. */
    var onCopy: ((String) -> Unit)? = null
    var onPaste: (() -> Unit)? = null

    @SuppressLint("ClickableViewAccessibility")
    override fun onTouchEvent(e: MotionEvent): Boolean {
        val f = frame
        when (e.actionMasked) {
            MotionEvent.ACTION_DOWN -> {
                scaled = false
                twoDown = 0L
                if (selection != null) {
                    dragging = handleAt(e.x, e.y)
                    if (dragging != 0) { stopActions(); parent?.requestDisallowInterceptTouchEvent(true); return true }
                }
            }
            MotionEvent.ACTION_POINTER_DOWN -> if (e.pointerCount == 2) {
                // Two fingers: a pinch or a two-finger tap, never a long press, a tap or a scroll.
                cancelGestures(e)
                twoDown = e.eventTime
                twoMoved = false
                for (i in 0..1) { twoX[i] = e.getX(i); twoY[i] = e.getY(i) }
            }
            MotionEvent.ACTION_MOVE -> {
                if (dragging != 0 && f != null) {
                    val s = selection ?: return true
                    // The finger sits on the handle below the line: aim at the line above it.
                    cellAt(e.x, e.y - handleR - cellH / 2, f)?.let { (c, r) ->
                        selection = if (dragging == 1) Selection.between(r, c, s.endRow, s.endCol) else Selection.between(s.startRow, s.startCol, r, c)
                        val n = selection!!
                        if (n.startRow != s.startRow || n.startCol != s.startCol) dragging = if (n.startRow == r && n.startCol == c) 1 else 2
                        invalidate()
                    }
                    return true
                }
                if (extending && f != null) {
                    cellAt(e.x, e.y, f)?.let { (c, r) ->
                        // From the pressed word to the finger, either way.
                        selection = if (r < anchor.first || (r == anchor.first && c < anchor.second)) Selection.between(r, c, anchorEnd.first, anchorEnd.second)
                        else Selection.between(anchor.first, anchor.second, r, c)
                        invalidate()
                    }
                    return true
                }
                if (twoDown != 0L && e.pointerCount >= 2) for (i in 0..1) {
                    if (abs(e.getX(i) - twoX[i]) > slop || abs(e.getY(i) - twoY[i]) > slop) twoMoved = true
                }
            }
            MotionEvent.ACTION_POINTER_UP -> {
                if (twoDown != 0L && !twoMoved && !scaled && e.eventTime - twoDown < 300) { twoDown = 0L; onTwoFingerTap?.invoke(); return true }
                twoDown = 0L
            }
            MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                if (dragging != 0 || extending) {
                    dragging = 0
                    extending = false
                    // Not an UP for the detector: it would take the lift for a tap and clear the selection.
                    cancelGestures(e)
                    if (e.actionMasked == MotionEvent.ACTION_UP) showActions()
                    return true
                }
            }
        }
        if (extending || dragging != 0) return true
        scaler.onTouchEvent(e)
        if (scaler.isInProgress) scaled = true
        // After a second finger, the rest of the gesture belongs to the pinch or the two-finger tap.
        if (e.actionMasked == MotionEvent.ACTION_DOWN) multi = false
        if (!multi && !scaler.isInProgress && e.pointerCount < 2) gestures.onTouchEvent(e)
        return true
    }

    /** Two fingers came down, or a selection drag ended: the detector forgets the gesture. */
    private var multi = false

    private fun cancelGestures(e: MotionEvent) {
        if (e.actionMasked == MotionEvent.ACTION_POINTER_DOWN) multi = true
        val c = MotionEvent.obtain(e)
        c.action = MotionEvent.ACTION_CANCEL
        gestures.onTouchEvent(c)
        c.recycle()
    }

    // MARK: keyboard

    /**
     * Whether the on-screen keyboard is wanted. Only then is the view a text editor: a view that
     * always says it is one gets the keyboard on some phones (Samsung's) as soon as it has focus,
     * and it needs focus for a hardware keyboard.
     */
    var keyboardWanted = false
        private set

    fun showKeyboard() {
        keyboardWanted = true
        requestFocus()
        val imm = context.getSystemService(InputMethodManager::class.java)
        imm.restartInput(this)
        imm.showSoftInput(this, 0)
        onKeyboardWanted?.invoke(true)
    }

    fun hideKeyboard() {
        keyboardWanted = false
        val imm = context.getSystemService(InputMethodManager::class.java)
        imm.hideSoftInputFromWindow(windowToken, 0)
        imm.restartInput(this)
        onKeyboardWanted?.invoke(false)
    }

    /** The keyboard went away by itself (Back, the system): not wanted any more. */
    fun keyboardGone() {
        if (!keyboardWanted) return
        keyboardWanted = false
        context.getSystemService(InputMethodManager::class.java).restartInput(this)
    }

    fun toggleKeyboard() = if (keyboardWanted) hideKeyboard() else showKeyboard()

    /** Focus without the keyboard: a hardware keyboard types here. */
    fun takeFocus() { if (!hasFocus()) requestFocus() }

    override fun onCheckIsTextEditor() = keyboardWanted

    override fun onCreateInputConnection(out: EditorInfo): InputConnection? {
        // No connection unless the keyboard is wanted: a keyboard bound to the pane shows itself
        // on the first key typed (adb's, a hardware one's). Hardware keys come to onKeyDown without it.
        if (!keyboardWanted) return null
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
        onUsed?.invoke()
        val m = mods()
        if (m.isNotEmpty()) {
            // The latched modifiers go with the first key, encoded by the server for the program's modes.
            val c = s[0]
            st.key(m + "+" + if (c == ' ') "space" else if (c == '\n') "enter" else c.lowercaseChar().toString())
            onModsUsed?.invoke()
            if (s.length > 1) typed(s.substring(1))
            return
        }
        st.text(s.replace("\n", "\r"))
    }

    /** Back from a real keyboard (some map Esc to it), not the system's: a keyboard's id is positive. */
    private fun keyboardBack(keyCode: Int, event: KeyEvent) =
        keyCode == KeyEvent.KEYCODE_BACK && event.deviceId > 0 && event.source and android.view.InputDevice.SOURCE_KEYBOARD == android.view.InputDevice.SOURCE_KEYBOARD &&
            event.device?.isVirtual == false

    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean {
        val st = stream ?: return super.onKeyDown(keyCode, event)
        if (keyboardBack(keyCode, event)) { onUsed?.invoke(); st.key("escape"); return true }
        if (keyCode == KeyEvent.KEYCODE_BACK) return super.onKeyDown(keyCode, event)
        // Ctrl+Shift+V pastes, as in desktop terminals.
        if (keyCode == KeyEvent.KEYCODE_V && event.isCtrlPressed && event.isShiftPressed) { onPaste?.invoke(); return true }
        if (selection != null) clearSelection()
        onUsed?.invoke()
        val latched = mods()
        var mods = 0
        if (event.isShiftPressed) mods = mods or 1
        if (event.isCtrlPressed || "ctrl" in latched) mods = mods or 2
        if (event.isAltPressed || "alt" in latched) mods = mods or 4
        if (event.isMetaPressed) mods = mods or 8
        val ch = event.getUnicodeChar(event.metaState and (KeyEvent.META_SHIFT_MASK or KeyEvent.META_CAPS_LOCK_ON))
        val text = if (ch > 0x1F && mods and (2 or 4) == 0) String(Character.toChars(ch)) else ""
        val unshifted = event.getUnicodeChar(0)
        if (latched.isNotEmpty() && !KeyEvent.isModifierKey(keyCode)) onModsUsed?.invoke()
        st.androidKey(keyCode, mods, text, unshifted)
        return true
    }

    override fun onKeyUp(keyCode: Int, event: KeyEvent) = keyCode != KeyEvent.KEYCODE_BACK || keyboardBack(keyCode, event) || super.onKeyUp(keyCode, event)
}

/** The latest path tapped in any pane: the one a "not a file" answer is about. */
private var lastPathTap = 0
