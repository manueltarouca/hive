package xyz.matm.hive.ui

import xyz.matm.hive.vt.Frame

/**
 * Selected cells on the screen, from (startRow, startCol) to (endRow, endCol)
 * inclusive, start before end. The app's own: it doesn't touch the program,
 * and covers the visible screen only.
 */
data class Selection(val startRow: Int, val startCol: Int, val endRow: Int, val endCol: Int) {

    /** The text, as shown: one line per row, trailing spaces dropped, wide characters once. */
    fun text(f: Frame): String = (startRow..endRow).joinToString("\n") { r ->
        val row = f.cells.getOrNull(r) ?: return@joinToString ""
        val a = if (r == startRow) startCol else 0
        val b = if (r == endRow) endCol else row.size - 1
        val sb = StringBuilder()
        for (x in a..minOf(b, row.size - 1)) {
            val c = row[x]
            if (!c.spacer) sb.append(c.text.ifEmpty { " " })
        }
        sb.trimEnd().toString()
    }

    companion object {
        /** From one cell to another, whichever comes first. */
        fun between(r1: Int, c1: Int, r2: Int, c2: Int): Selection =
            if (r1 < r2 || (r1 == r2 && c1 <= c2)) Selection(r1, c1, r2, c2) else Selection(r2, c2, r1, c1)

        /** Every row on screen that has text. */
        fun all(f: Frame): Selection {
            var last = f.rows - 1
            while (last > 0 && f.rowText(last).isBlank()) last--
            return Selection(0, 0, last, maxOf(0, f.cols - 1))
        }

        /** Characters that end a word: spaces, brackets, quotes and box lines; paths and URLs stay whole. */
        private fun breaks(s: String) = s.isBlank() || s.length == 1 && s[0] in " \t()[]{}<>'\"`,;│|─"

        /**
         * The word at a cell (its start and end columns). A press between words takes the nearest
         * one within three cells, as a thumb lands near what it means; else the run of spaces.
         */
        fun word(f: Frame, col: Int, row: Int): Pair<Int, Int> {
            val cells = f.cells.getOrNull(row) ?: return col to col
            fun t(x: Int) = cells.getOrNull(x)?.let { if (it.spacer) "x" else it.text } ?: " "
            if (breaks(t(col))) {
                for (d in 1..3) {
                    if (col - d >= 0 && !breaks(t(col - d))) return word(f, col - d, row)
                    if (col + d < cells.size && !breaks(t(col + d))) return word(f, col + d, row)
                }
                var a = col
                var b = col
                while (a > 0 && t(a - 1).isBlank()) a--
                while (b < cells.size - 1 && t(b + 1).isBlank()) b++
                return a to b
            }
            var a = col
            var b = col
            while (a > 0 && !breaks(t(a - 1))) a--
            while (b < cells.size - 1 && !breaks(t(b + 1))) b++
            // A sentence's full stop or a path's trailing colon isn't part of the word.
            while (b > a && t(b) in setOf(".", ":", "!", "?")) b--
            return a to b
        }
    }
}
