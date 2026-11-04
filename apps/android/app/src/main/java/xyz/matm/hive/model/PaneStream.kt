package xyz.matm.hive.model

import android.os.Handler
import android.os.Looper
import xyz.matm.hive.net.BlockUpdate
import xyz.matm.hive.net.HiveClient
import xyz.matm.hive.vt.Frame
import xyz.matm.hive.vt.Mirror
import xyz.matm.hive.vt.TermTheme
import java.util.concurrent.Executors

/**
 * The pane on screen: one block subscribed, mirrored by libghostty on the
 * vt thread, frames handed to [sink] on the main thread. Only the pane on
 * screen is subscribed (a phone keeps one mirror, not a session's worth).
 */
class PaneStream(val client: HiveClient, val blockID: String, theme: TermTheme, private val sink: (Frame) -> Unit) {
    @Volatile private var theme = theme
    private val main = Handler(Looper.getMainLooper())
    private var mirror: Mirror? = null
    @Volatile private var framePending = false
    @Volatile private var closed = false
    @Volatile var exited = false
        private set
    /** The server's size for the pane, from its snapshot and resize frames. */
    @Volatile var cols = 0
        private set
    @Volatile var rows = 0
        private set
    /** The cell size in pixels the size's owner reported (resize frames), 0 until one arrives. */
    @Volatile var cellWidth = 0
        private set
    @Volatile var cellHeight = 0
        private set

    companion object {
        /** Every mirror lives on this thread. */
        val vt = Executors.newSingleThreadExecutor { r -> Thread(r, "hive.vt").apply { isDaemon = true } }
    }

    fun start() = client.subscribe(blockID) { u -> vt.execute { apply(u) } }

    fun stop() {
        closed = true
        client.unsubscribe(blockID)
        vt.execute { mirror?.close(); mirror = null }
    }

    private fun apply(u: BlockUpdate) {
        if (closed) return
        when (u) {
            is BlockUpdate.Snapshot -> {
                mirror?.close()
                mirror = Mirror.fromSnapshot(u.data, theme)
                if (findQuery != null) refind()
            }
            is BlockUpdate.Output -> {
                mirror?.feed(u.data)
                // New output can move what find marked: search again shortly.
                if (findQuery != null && !findStale) {
                    findStale = true
                    main.postDelayed({ vt.execute { if (findStale) refind() } }, 150)
                }
            }
            is BlockUpdate.Resize -> {
                cellWidth = u.cellWidth
                cellHeight = u.cellHeight
                mirror?.resize(u.cols, u.rows, u.cellWidth, u.cellHeight)
            }
            BlockUpdate.Resync -> { start(); return }
            BlockUpdate.Exited -> exited = true
        }
        redraw()
    }

    /** Coalesces: one frame per burst of updates. */
    private fun redraw() {
        if (framePending) return
        framePending = true
        vt.execute {
            framePending = false
            val m = mirror ?: return@execute
            val f = m.frame()
            cols = f.cols
            rows = f.rows
            main.post { if (!closed) sink(f) }
        }
    }

    /** Night or Day changed: the default colours follow, as on the Mac. */
    fun setTheme(t: TermTheme) {
        theme = t
        vt.execute { mirror?.setTheme(t); redraw() }
    }

    /** Text the user typed (an input method's commit), as it is. */
    fun text(s: String) {
        if (s.isEmpty()) return
        vt.execute { mirror?.scrollToBottom() }
        client.input(blockID, s.toByteArray())
    }

    /** A named key, encoded by the server: escape, ctrl+c, up… */
    fun key(name: String) {
        vt.execute { mirror?.scrollToBottom() }
        client.key(blockID, name)
    }

    /** A hardware key, encoded here for the program's modes (kitty, application cursor…). */
    fun androidKey(code: Int, mods: Int, text: String, unshifted: Int) {
        vt.execute {
            val m = mirror ?: return@execute
            val consumed = if (text.isNotEmpty()) mods and (1 or 4 or 16) else 0
            val bytes = m.encodeKey(code, mods, consumed, text, unshifted)
            if (bytes.isNotEmpty()) { m.scrollToBottom(); client.input(blockID, bytes); redraw() }
        }
    }

    /** Pastes, bracketed when the program asked for it. [confirm] decides, on the main thread, text that could run commands. */
    fun paste(s: String, confirm: (lines: Int, go: () -> Unit) -> Unit) {
        vt.execute {
            val m = mirror ?: return@execute
            val bytes = m.encodePaste(s)
            if (m.isPasteSafe(s)) { m.scrollToBottom(); client.input(blockID, bytes) }
            else main.post { confirm(s.lines().size) { client.input(blockID, bytes) } }
        }
    }

    /** Scrolls by rows (negative: up, into the scrollback), or sends the wheel to a program that wants it. */
    fun scroll(rowsBy: Int, x: Float, y: Float, g: Mirror.Geometry) {
        vt.execute {
            val m = mirror ?: return@execute
            val bytes = m.wheel(rowsBy, x, y, g)
            if (bytes == null) { m.scroll(rowsBy); redraw() } else client.input(blockID, bytes)
        }
    }

    /** A tap: a click for a program that tracks the mouse, else the link under it (on the main thread), if any. */
    fun tap(col: Int, row: Int, x: Float, y: Float, g: Mirror.Geometry, link: (String?) -> Unit) {
        vt.execute {
            val m = mirror ?: return@execute
            val press = m.mouse(0, x, y, g)
            if (press != null) {
                val release = m.mouse(1, x, y, g) ?: ByteArray(0)
                client.input(blockID, press + release)
                return@execute // the program took the tap: no link, no path
            }
            val uri = m.hyperlink(col, row)
            main.post { link(uri) }
        }
    }

    // Find: kept here so a new snapshot (a resize, a resync) is marked again.
    private var findQuery: String? = null
    private var matches: List<Mirror.FindMatch> = emptyList()
    private var findCurrent = -1
    private var findStale = false
    /** Find searched again on its own (new output, a snapshot): count and current, on main. */
    @Volatile var onFind: ((Int, Int) -> Unit)? = null

    private fun refind() {
        val q = findQuery ?: return
        val m = mirror ?: return
        findStale = false
        val was = matches.getOrNull(findCurrent)
        matches = m.find(q)
        findCurrent = was?.let { w -> matches.indexOfFirst { it.row == w.row && it.col == w.col } }?.takeIf { it >= 0 } ?: matches.lastIndex
        m.setFindMarks(matches, findCurrent)
        redraw()
        val n = matches.size
        val c = findCurrent
        main.post { onFind?.invoke(n, c) }
    }

    /** Searches the scrollback; the match nearest the bottom is current. [done] gets the count and the current one, on main. */
    fun find(query: String, done: (Int, Int) -> Unit) = vt.execute {
        val m = mirror ?: return@execute
        matches = m.find(query)
        findCurrent = matches.lastIndex
        findQuery = query.ifEmpty { null }
        mark(m)
        val n = matches.size
        val c = findCurrent
        main.post { done(n, c) }
    }

    /** The next (1) or previous (-1) match, wrapping around. */
    fun stepFind(by: Int, done: (Int) -> Unit) = vt.execute {
        val m = mirror ?: return@execute
        if (matches.isEmpty()) return@execute
        val n = matches.size
        findCurrent = ((findCurrent + by) % n + n) % n
        mark(m)
        val c = findCurrent
        main.post { done(c) }
    }

    fun endFind() = vt.execute {
        findQuery = null
        matches = emptyList()
        findCurrent = -1
        mirror?.setFindMarks(emptyList(), -1)
        redraw()
    }

    private fun mark(m: Mirror) {
        m.setFindMarks(matches, findCurrent)
        matches.getOrNull(findCurrent)?.let(m::reveal)
        redraw()
    }

    /** The pane's text, for checks; on the vt thread. */
    fun plainText(done: (String) -> Unit) = vt.execute { val t = mirror?.plainText() ?: ""; main.post { done(t) } }
}
