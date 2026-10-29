package xyz.matm.hive.ui

import xyz.matm.hive.vt.Frame

/**
 * The file path under a tap, as programs print them in plain text (agents
 * don't make links): the run of non-space characters under the finger, read
 * across rows the terminal wrapped at the edge, without the brackets, quotes
 * and punctuation around it. A `:line` stays: the server splits it off. The
 * Mac reads them the same way (LinkDetector); whether it exists is the
 * server's to say.
 */
object PathAt {
    private const val stops = " \t\"'`<>()[]{}|,;"
    private const val trimStart = "([{<'\"`"
    private const val trimEnd = ".,;:!?)]}>'\"`"
    private val named = Regex("""^[\w@+-][\w@+.-]*\.[A-Za-z][A-Za-z0-9]{0,7}(:\d+(:\d+)?)?$""")

    fun find(f: Frame, col: Int, row: Int): String? = hit(f, col, row)?.first

    /** The path and the cells it covers (to light them), across wrapped rows. */
    fun hit(f: Frame, col: Int, row: Int): Pair<String, Selection>? {
        if (row !in 0 until f.rows || col !in 0 until f.cols) return null
        // The logical line: up while the row above wraps into this one, down while this one wraps.
        var top = row
        while (top > 0 && f.wraps.getOrElse(top - 1) { false }) top--
        var bottom = row
        while (bottom < f.rows - 1 && f.wraps.getOrElse(bottom) { false }) bottom++
        val cells = ArrayList<String>((bottom - top + 1) * f.cols)
        for (y in top..bottom) {
            val r = f.cells.getOrNull(y)
            for (x in 0 until f.cols) {
                val c = r?.getOrNull(x)
                cells += when {
                    c == null -> " "
                    c.spacer -> ""
                    else -> c.text.ifEmpty { " " }
                }
            }
        }
        val at = (row - top) * f.cols + col
        if (isStop(cells[at])) return null
        var a = at
        while (a > 0 && !isStop(cells[a - 1])) a--
        var b = at
        while (b < cells.size - 1 && !isStop(cells[b + 1])) b++
        // Trimmed cell by cell, so the lit cells are the path's own.
        while (a <= b && cells[a].length == 1 && cells[a][0] in trimStart) a++
        while (b >= a && cells[b].length == 1 && cells[b][0] in trimEnd) b--
        if (a > b) return null
        val token = cells.subList(a, b + 1).joinToString("")
        if (!looksLikeAPath(token)) return null
        return token to Selection(top + a / f.cols, a % f.cols, top + b / f.cols, b % f.cols)
    }

    private fun isStop(s: String) = s.isEmpty().not() && s.length == 1 && s[0] in stops

    /** Something with a slash in it, or a name with an extension (main.go, Desk.kt:12). */
    fun looksLikeAPath(t: String) =
        (t.contains('/') && !t.startsWith("//") && t.any { it.isLetterOrDigit() }) || named.matches(t)
}
