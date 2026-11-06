package xyz.matm.hive.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import xyz.matm.hive.vt.Cell
import xyz.matm.hive.vt.Frame

class PathAtTest {
    /** Rows of the screen; a row ending in "\\" soft-wraps into the next (the \ isn't shown). */
    private fun frame(vararg rows: String, cols: Int = 20): Frame {
        val wraps = BooleanArray(rows.size) { rows[it].endsWith("\\") }
        val text = rows.map { it.removeSuffix("\\") }
        return Frame(cols, rows.size,
            text.map { r -> Array(cols) { x -> Cell(r.getOrNull(x)?.toString() ?: "", 0, 0, 0, -1, -1) } },
            0, 0, true, 1, 0, 0xFFFFFF, 1, wraps)
    }

    @Test fun aPathUnderTheFinger() {
        val f = frame("  Saved build/a.png", "Update(ui/Desk.kt)")
        assertEquals("build/a.png", PathAt.find(f, 12, 0))
        assertEquals("ui/Desk.kt", PathAt.find(f, 9, 1)) // inside the brackets
    }

    @Test fun joinsRowsTheTerminalWrapped() {
        // A path longer than the pane: 20 columns, wrapped twice.
        val f = frame("see /Users/ana/src/p\\", "rojects/hive/apps/an\\", "droid/Desk.kt ok")
        val whole = "/Users/ana/src/projects/hive/apps/android/Desk.kt"
        assertEquals(whole, PathAt.find(f, 6, 0))
        assertEquals(whole, PathAt.find(f, 3, 1))
        assertEquals(whole, PathAt.find(f, 2, 2))
        assertNull(PathAt.find(f, 15, 2)) // "ok": a word, not a path
    }

    @Test fun keepsALineNumberDropsPunctuation() {
        val f = frame("at main.go:42: nil", "(see ./run.sh).", "`~/x/y.txt`,")
        assertEquals("main.go:42", PathAt.find(f, 5, 0))
        assertEquals("./run.sh", PathAt.find(f, 8, 1))
        assertEquals("~/x/y.txt", PathAt.find(f, 4, 2))
    }

    @Test fun notAPath() {
        val f = frame("Baking... done", "  hello world", "a/b")
        assertNull(PathAt.find(f, 2, 0)) // a word
        assertNull(PathAt.find(f, 0, 1)) // a space
        assertEquals("a/b", PathAt.find(f, 1, 2))
    }
}
