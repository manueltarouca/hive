package xyz.matm.hive.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class ComposerTest {
    @Test fun committedTextGoesAtOnce() {
        val c = Composer()
        assertEquals("a", c.commit("a"))
    }

    @Test fun compositionGoesWhenFinished() {
        val c = Composer()
        assertEquals("", c.compose("に"))
        assertEquals("", c.compose("にほ"))
        assertEquals("にほん", c.commit("にほん")) // committed: replaces the composition
        assertEquals("", c.finish())
        assertEquals("", c.compose("hel"))
        assertEquals("hel", c.finish()) // voice and swipe keyboards may only finish
        assertEquals("", c.finish())
    }

    @Test fun deletingInsideACompositionStaysThere() {
        val c = Composer()
        c.compose("abc")
        assertEquals(0, c.delete(1)) // nothing reached the pane yet: nothing to erase there
        assertEquals("ab", c.finish())
        assertEquals(2, c.delete(2))
    }
}
