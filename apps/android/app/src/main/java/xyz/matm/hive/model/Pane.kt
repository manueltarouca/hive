package xyz.matm.hive.model

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import xyz.matm.hive.vt.Frame

/** A window's layout, from session.view: one block, or a split of two parts. */
sealed class Layout {
    data class Leaf(val block: String) : Layout()
    /** [horizontal]: a beside b; else a above b. [ratio]: a's share. */
    data class Split(val horizontal: Boolean, val ratio: Float, val a: Layout, val b: Layout) : Layout()

    val blocks: List<String> get() = when (this) { is Leaf -> listOf(block); is Split -> a.blocks + b.blocks }

    companion object {
        fun parse(l: org.json.JSONObject?): Layout? = when (l?.optString("type")) {
            "block" -> Leaf(l.optString("block_id"))
            "split" -> {
                val a = parse(l.optJSONObject("a"))
                val b = parse(l.optJSONObject("b"))
                when {
                    a == null -> b
                    b == null -> a
                    else -> Split(l.optString("direction") != "vertical", l.optDouble("ratio", 0.5).toFloat().coerceIn(0.1f, 0.9f), a, b)
                }
            }
            else -> null
        }
    }
}

/**
 * A pane on screen: its stream, its last frame, and whether this device
 * holds its size. Several are on screen at once when a wide screen shows a
 * split side by side; one on a phone.
 */
class Pane(val blockID: String, val stream: PaneStream) {
    var frame by mutableStateOf<Frame?>(null)
    /** The pane takes this device's size (the Mac letterboxes) until the user stops, or the Mac takes it back. */
    var fitted by mutableStateOf(false)
    /** Who held the pane before it was fitted here: the button gives it back to them. */
    var previousHolder by mutableStateOf<String?>(null)
    /** The size asked for last: cols, rows, cell width, cell height. */
    var fitSize: IntArray? = null
    /** The size the pane had before it was fitted here, to give back: cols, rows, cell width, cell height. */
    var before: IntArray? = null
    /** The sizes asked for lately: a size nobody here asked for means another device took the pane. */
    val asked = ArrayDeque<Pair<Int, Int>>()
    /** This device's room for the pane, as its view measured it: cols, rows, cell width, cell height. */
    var room: IntArray? = null
    /** Opening has been dealt with: fitted, or left at the other device's size. */
    var autoFitDone = false
    /** A wide screen took it on first use; after a give back it stays given back. */
    var usedFitDone = false

    fun mine(cols: Int, rows: Int) = asked.any { it.first == cols && it.second == rows }
}
