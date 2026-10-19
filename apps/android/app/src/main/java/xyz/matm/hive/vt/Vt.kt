package xyz.matm.hive.vt

import java.nio.ByteBuffer
import java.nio.ByteOrder

/** libghostty-vt through the JNI shim (src/main/cpp/jni.c). */
internal object Vt {
    init { System.loadLibrary("hivevt") }

    @JvmStatic external fun nNew(cols: Int, rows: Int): Long
    @JvmStatic external fun nFromSnapshot(data: ByteArray): Long
    @JvmStatic external fun nFree(h: Long)
    @JvmStatic external fun nFeed(h: Long, data: ByteArray, off: Int, len: Int)
    @JvmStatic external fun nResize(h: Long, cols: Int, rows: Int, cellWidth: Int, cellHeight: Int)
    @JvmStatic external fun nSetTheme(h: Long, fg: Int, bg: Int, cursor: Int, palette: IntArray)
    @JvmStatic external fun nFrame(h: Long): ByteBuffer?
    @JvmStatic external fun nEncodeKey(h: Long, code: Int, mods: Int, consumed: Int, utf8: ByteArray, unshifted: Int): ByteArray?
    @JvmStatic external fun nEncodePaste(h: Long, utf8: ByteArray): ByteArray?
    @JvmStatic external fun nPasteSafe(h: Long, utf8: ByteArray): Boolean
    @JvmStatic external fun nScroll(h: Long, rows: Int)
    @JvmStatic external fun nScrollBottom(h: Long)
    @JvmStatic external fun nWheel(h: Long, rows: Int, x: Float, y: Float, sw: Int, sh: Int, cw: Int, ch: Int): ByteArray?
    @JvmStatic external fun nMouse(h: Long, action: Int, x: Float, y: Float, sw: Int, sh: Int, cw: Int, ch: Int): ByteArray?
    @JvmStatic external fun nHyperlink(h: Long, col: Int, row: Int): String?
    @JvmStatic external fun nPlainText(h: Long): String
    @JvmStatic external fun nMode(h: Long, number: Int, ansi: Boolean): Boolean
}

/** One cell of a frame. Colors are 0xRRGGBB, or -1 for the default. */
class Cell(
    @JvmField val text: String,
    @JvmField val wide: Int,
    @JvmField val flags: Int,
    @JvmField val underline: Int,
    @JvmField val fg: Int,
    @JvmField val bg: Int,
) {
    val bold get() = flags and 1 != 0
    val italic get() = flags and 2 != 0
    val faint get() = flags and 4 != 0
    val inverse get() = flags and 8 != 0
    val invisible get() = flags and 16 != 0
    val strikethrough get() = flags and 32 != 0
    val selected get() = flags and 128 != 0
    /** The second half of a wide character, or the filler before one that wrapped. */
    val spacer get() = wide == 2 || wide == 3

    companion object {
        val blank = Cell("", 0, 0, 0, -1, -1)
    }
}

/** An immutable picture of a pane; rows that didn't change are shared with the previous frame. */
class Frame(
    val cols: Int,
    val rows: Int,
    val cells: List<Array<Cell>>,
    val cursorX: Int,
    val cursorY: Int,
    val cursorVisible: Boolean,
    val cursorStyle: Int,
    val background: Int,
    val foreground: Int,
    val generation: Long,
) {
    fun rowText(y: Int): String {
        val row = cells.getOrNull(y) ?: return ""
        val sb = StringBuilder()
        for (c in row) if (!c.spacer) sb.append(c.text.ifEmpty { " " })
        return sb.trimEnd().toString()
    }

    /** The visible text, one line per row, trailing blank rows dropped. */
    fun text(): String = (0 until rows).map(::rowText).joinToString("\n").trimEnd('\n')
}

/** Theme: Ghostty's default, as the Mac app (HiveCore/Theme.swift). */
object TermTheme {
    const val FOREGROUND = 0xFFFFFF
    const val BACKGROUND = 0x282C34
    const val CURSOR = 0xFFFFFF
    const val SELECTION = 0x3E4451
    val palette = intArrayOf(
        0x1D1F21, 0xCC6666, 0xB5BD68, 0xF0C674, 0x81A2BE, 0xB294BB, 0x8ABEB7, 0xC5C8C6,
        0x666666, 0xD54E53, 0xB9CA4A, 0xE7C547, 0x7AA6DA, 0xC397D8, 0x70C0B1, 0xEAEAEA,
    )
}

/**
 * A libghostty terminal mirroring one block. Not thread-safe: use it from
 * the vt thread only. It never answers the program: the server's terminal
 * already did.
 */
class Mirror private constructor(private var h: Long) {
    private var last: Frame? = null
    private var generation = 0L
    private val textCache = HashMap<String, String>()

    init {
        Vt.nSetTheme(h, TermTheme.FOREGROUND, TermTheme.BACKGROUND, TermTheme.CURSOR, TermTheme.palette)
    }

    companion object {
        fun create(cols: Int, rows: Int): Mirror = Mirror(Vt.nNew(cols, rows).also { require(it != 0L) { "terminal_new" } })
        fun fromSnapshot(data: ByteArray): Mirror? = Vt.nFromSnapshot(data).takeIf { it != 0L }?.let(::Mirror)
    }

    fun close() { if (h != 0L) { Vt.nFree(h); h = 0L } }

    // Every call below needs a live terminal: after close() they do nothing.
    fun feed(data: ByteArray, off: Int = 0, len: Int = data.size - off) { if (h != 0L) Vt.nFeed(h, data, off, len) }
    fun resize(cols: Int, rows: Int, cellWidth: Int, cellHeight: Int) { if (h != 0L) Vt.nResize(h, cols, rows, cellWidth, cellHeight) }
    fun scroll(rows: Int) { if (h != 0L) Vt.nScroll(h, rows) }
    fun scrollToBottom() { if (h != 0L) Vt.nScrollBottom(h) }
    fun plainText(): String = if (h == 0L) "" else Vt.nPlainText(h)
    fun hyperlink(col: Int, row: Int): String? = if (h == 0L) null else Vt.nHyperlink(h, col, row)
    fun mode(n: Int, ansi: Boolean = false) = h != 0L && Vt.nMode(h, n, ansi)

    /** A key, by Android key code, encoded for the program's current modes. */
    fun encodeKey(code: Int, mods: Int = 0, consumed: Int = 0, text: String = "", unshifted: Int = 0): ByteArray =
        if (h == 0L) ByteArray(0) else Vt.nEncodeKey(h, code, mods, consumed, text.toByteArray(), unshifted) ?: ByteArray(0)

    fun encodePaste(text: String): ByteArray = (if (h == 0L) null else Vt.nEncodePaste(h, text.toByteArray())) ?: text.toByteArray()
    fun isPasteSafe(text: String) = h != 0L && Vt.nPasteSafe(h, text.toByteArray())

    /** null: scroll the viewport here. Otherwise the bytes for the program. */
    fun wheel(rows: Int, x: Float, y: Float, g: Geometry) = if (h == 0L) ByteArray(0) else Vt.nWheel(h, rows, x, y, g.width, g.height, g.cellWidth, g.cellHeight)

    /** A press (0) or release (1) for a program that tracks the mouse, or null when it doesn't. */
    fun mouse(action: Int, x: Float, y: Float, g: Geometry) = if (h == 0L) null else Vt.nMouse(h, action, x, y, g.width, g.height, g.cellWidth, g.cellHeight)

    data class Geometry(val width: Int, val height: Int, val cellWidth: Int, val cellHeight: Int)

    /** The current picture. Synchronized output (mode 2026) keeps the last complete one, for up to a second. */
    fun frame(): Frame {
        val buf = if (h == 0L) null else Vt.nFrame(h)
        val prev = last
        if (buf == null) return prev ?: Frame(0, 0, emptyList(), 0, 0, false, 1, TermTheme.BACKGROUND, TermTheme.FOREGROUND, 0)
        buf.order(ByteOrder.LITTLE_ENDIAN)
        val cols = buf.getInt(0)
        val rows = buf.getInt(4)
        val full = buf.get(8).toInt() != 0
        val visible = buf.get(9).toInt() != 0 && buf.get(10).toInt() != 0
        val cx = buf.getShort(12).toInt() and 0xFFFF
        val cy = buf.getShort(14).toInt() and 0xFFFF
        val style = buf.get(16).toInt()
        val bg = buf.getInt(20)
        val fg = buf.getInt(24)
        val dirty = buf.getInt(28)
        val cells: MutableList<Array<Cell>> =
            if (full || prev == null || prev.rows != rows || prev.cols != cols) MutableList(rows) { Array(cols) { Cell.blank } }
            else prev.cells.toMutableList()
        buf.position(36)
        val scratch = ByteArray(64)
        repeat(dirty) {
            val y = buf.short.toInt() and 0xFFFF
            val n = buf.short.toInt() and 0xFFFF
            val row = Array(cols) { Cell.blank }
            for (x in 0 until n) {
                val len = buf.get().toInt() and 0xFF
                buf.get(scratch, 0, len)
                val text = when (len) {
                    0 -> ""
                    1 -> ascii[scratch[0].toInt() and 0x7F].takeIf { scratch[0] >= 0 } ?: String(scratch, 0, 1)
                    else -> String(scratch, 0, len).let { textCache.getOrPut(it) { it } }
                }
                val wide = buf.get().toInt()
                val flags = buf.get().toInt() and 0xFF
                val underline = buf.get().toInt()
                val colors = buf.get().toInt()
                val cfg = if (colors and 1 != 0) rgb(buf) else -1
                val cbg = if (colors and 2 != 0) rgb(buf) else -1
                if (x < cols) row[x] = Cell(text, wide, flags, underline, cfg, cbg)
            }
            if (y < rows) cells[y] = row
        }
        if (textCache.size > 4096) textCache.clear()
        generation++
        return Frame(cols, rows, cells, cx, cy, visible, style, bg, fg, generation).also { last = it }
    }

    private fun rgb(b: ByteBuffer): Int {
        val r = b.get().toInt() and 0xFF
        val g = b.get().toInt() and 0xFF
        val bl = b.get().toInt() and 0xFF
        return r shl 16 or (g shl 8) or bl
    }
}

private val ascii = Array(128) { it.toChar().toString() }
