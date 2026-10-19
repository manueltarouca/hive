package xyz.matm.hive.ui

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class GlyphsTest {
    @Test fun colourEmojiStayColour() {
        // Emoji_Presentation characters: the terminal makes them two cells wide.
        for (s in listOf("⭐", "🟢", "⌛", "⏳", "⚫", "😀", "✅")) assertTrue(s, Glyphs.colourEmoji(s, wide = true))
        assertTrue("VS16 asks for emoji", Glyphs.colourEmoji("⚠️", wide = true))
        assertTrue(Glyphs.colourEmoji("✔️", wide = false))
    }

    @Test fun textSymbolsStayText() {
        // Claude Code's symbols are one cell wide, text by default.
        for (s in listOf("⏺", "⏸", "⏵", "✻", "⎿", "★", "●")) assertFalse(s, Glyphs.colourEmoji(s, wide = false))
        // Wide but not emoji: CJK keeps the text fonts.
        assertFalse(Glyphs.colourEmoji("漢", wide = true))
        assertFalse(Glyphs.colourEmoji("ア", wide = true))
    }
}
