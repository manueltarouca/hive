package xyz.matm.hive.model

import org.junit.Assert.assertEquals
import org.junit.Test

class TabGroupsTest {
    private fun tabs(vararg t: Pair<Boolean, String?>): List<TabGroups.Tab> = t.mapIndexed { i, (pin, st) -> TabGroups.Tab(i, pin, st) }

    @Test fun shownPutsPinsFirstKeepingOrder() {
        assertEquals(listOf(1, 3, 0, 2), TabGroups.shown(listOf(false, true, false, true)))
        assertEquals(emptyList<Int>(), TabGroups.shown(emptyList()))
    }

    @Test fun fewTabsAllShowInTheStrip() {
        val t = tabs(false to null, true to "working", false to "blocked")
        assertEquals(listOf(1, 0, 2), TabGroups.strip(t, current = 0))
    }

    @Test fun manyTabsTheStripKeepsPinsWhatNeedsYouAndWhereYouAre() {
        val t = tabs(false to null, false to "done", true to null, false to "working", false to "blocked", false to null, false to null)
        // Pins, then blocked, then done, then the current tab.
        assertEquals(listOf(2, 4, 1, 6), TabGroups.strip(t, current = 6))
        // The current tab is already there: not twice.
        assertEquals(listOf(2, 4, 1), TabGroups.strip(t, current = 4))
    }

    @Test fun groupsByWhatTheyAreDoing() {
        val t = tabs(false to null, false to "done", true to "blocked", false to "working", false to "blocked", false to null)
        val g = TabGroups.groups(t)
        assertEquals(listOf(2), g.pinned)
        assertEquals(listOf(4, 1), g.needsYou)
        assertEquals(listOf(3), g.working)
        assertEquals(listOf(0, 5), g.quiet)
    }

    @Test fun stripHides() {
        val many = tabs(*Array(7) { false to null })
        assertEquals(true, TabGroups.strip(many, current = 0).size < many.size)
    }
}
