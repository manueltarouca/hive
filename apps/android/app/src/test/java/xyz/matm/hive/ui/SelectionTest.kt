package xyz.matm.hive.ui

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import xyz.matm.hive.model.Layout
import xyz.matm.hive.vt.Cell
import xyz.matm.hive.vt.Frame

class SelectionTest {
    private fun frame(vararg rows: String, cols: Int = 30): Frame = Frame(
        cols, rows.size,
        rows.map { r -> Array(cols) { x -> Cell(r.getOrNull(x)?.toString() ?: "", 0, 0, 0, -1, -1) } },
        0, 0, true, 1, 0, 0xFFFFFF, 1,
    )

    @Test fun aWordStopsAtSpacesAndBracketsButKeepsPaths() {
        val f = frame("Read(internal/cache/store.go)", "see ~/code/hive: done.")
        assertEquals(5 to 27, Selection.word(f, 10, 0)) // internal/cache/store.go, without the brackets
        assertEquals(4 to 14, Selection.word(f, 6, 1)) // ~/code/hive, without the colon
        assertEquals(17 to 20, Selection.word(f, 18, 1)) // done, without the full stop
        assertEquals(0 to 2, Selection.word(f, 3, 1)) // between words: the nearest, "see"
        val wide = frame("a          b")
        assertEquals(1 to 10, Selection.word(wide, 6, 0)) // far from any word: the spaces
    }

    @Test fun textIsAsShownWithoutTrailingSpaces() {
        val f = frame("first line", "second line", "third")
        val s = Selection.between(1, 7, 0, 6) // backwards: still from first to last
        assertEquals(Selection(0, 6, 1, 7), s)
        assertEquals("line\nsecond l", s.text(f))
        assertEquals("first line\nsecond line\nthird", Selection.all(f).text(f))
    }

    @Test fun selectAllStopsAtTheLastRowWithText() {
        val f = frame("a", "b", "", "")
        assertEquals(1, Selection.all(f).endRow)
    }

    @Test fun layoutKeepsSplitsAndRatios() {
        val j = JSONObject("""{"type":"split","direction":"horizontal","ratio":0.7,"a":{"type":"block","block_id":"A"},
            "b":{"type":"split","direction":"vertical","ratio":0.5,"a":{"type":"block","block_id":"B"},"b":{"type":"block","block_id":"C"}}}""")
        val l = Layout.parse(j) as Layout.Split
        assertTrue(l.horizontal)
        assertEquals(0.7f, l.ratio)
        assertEquals(listOf("A", "B", "C"), l.blocks)
        assertFalse((l.b as Layout.Split).horizontal)
    }

    @Test fun sideBySideOnlyWhenEveryPaneIsUsable() {
        val two = Layout.Split(true, 0.5f, Layout.Leaf("A"), Layout.Leaf("B"))
        assertTrue(fitsSideBySide(two, 1440f, 700f, 11.5f)) // a tablet in landscape
        assertTrue(fitsSideBySide(two, 900f, 1000f, 11.5f)) // a tablet upright: 450 dp each
        assertFalse(fitsSideBySide(two, 400f, 700f, 11.5f)) // a phone: 200 dp each is too narrow
        val three = Layout.Split(true, 0.34f, Layout.Leaf("A"), Layout.Split(true, 0.5f, Layout.Leaf("B"), Layout.Leaf("C")))
        assertFalse(fitsSideBySide(three, 700f, 900f, 11.5f)) // three across 700 dp: about 230 dp each
        val stacked = Layout.Split(false, 0.5f, Layout.Leaf("A"), Layout.Leaf("B"))
        assertFalse(fitsSideBySide(stacked, 1440f, 300f, 11.5f)) // the keyboard's up: too few rows each
    }
}
