package xyz.matm.hive.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** Shapes in a 10 x 20 cell, line thickness 1, y down (as Android's Canvas). */
class BoxDrawingTest {
    private fun shapes(cp: Int) = BoxDrawing.shapes(cp, 10f, 20f, 1f)

    @Test fun whatIsDrawnAsShapes() {
        for (cp in listOf(0x2500, 0x2502, 0x256D, 0x2550, 0x2588, 0x2590, 0x2591, 0x259B, 0xE0B0)) assertTrue(cp.toString(16), BoxDrawing.handles(cp))
        for (cp in listOf(0x23FA, 0x41, 0x25CF, 0x2571)) assertFalse(cp.toString(16), BoxDrawing.handles(cp))
    }

    @Test fun aVerticalLineRunsTheFullHeight() {
        val r = shapes(0x2502).rects
        assertEquals(0f, r.minOf { it.top })
        assertEquals(20f, r.maxOf { it.bottom })
        assertTrue(r.all { it.right - it.left == 1f && it.left == 4.5f })
    }

    @Test fun aCrossReachesEveryEdge() {
        val r = shapes(0x253C).rects
        assertEquals(0f, r.minOf { it.left }); assertEquals(10f, r.maxOf { it.right })
        assertEquals(0f, r.minOf { it.top }); assertEquals(20f, r.maxOf { it.bottom })
    }

    @Test fun blocksFillTheirPart() {
        assertEquals(listOf(BoxDrawing.Rect(0f, 0f, 10f, 20f)), shapes(0x2588).rects) // █
        assertEquals(listOf(BoxDrawing.Rect(5f, 0f, 10f, 20f)), shapes(0x2590).rects) // ▐
        assertEquals(listOf(BoxDrawing.Rect(0f, 0f, 10f, 10f)), shapes(0x2580).rects) // ▀
        assertEquals(listOf(BoxDrawing.Rect(0f, 10f, 10f, 20f)), shapes(0x2584).rects) // ▄
        assertEquals(3, shapes(0x259B).rects.size) // ▛: three quadrants
    }

    @Test fun shadesAreSolidAtAnAlpha() {
        assertEquals(0.25f, shapes(0x2591).alpha)
        assertEquals(0.75f, shapes(0x2593).alpha)
        assertEquals(1f, shapes(0x2588).alpha)
    }

    /** Claude Code's result bracket: a stroke from the cell's top, turning right near the baseline. */
    @Test fun theResultBracketIsAShape() {
        assertTrue(BoxDrawing.handles(0x23BF))
        val r = shapes(0x23BF).rects
        assertEquals(2, r.size)
        assertEquals(0f, r.minOf { it.top })
        assertEquals(10f, r.maxOf { it.right })
    }
}
