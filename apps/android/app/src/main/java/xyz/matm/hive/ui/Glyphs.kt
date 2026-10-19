package xyz.matm.hive.ui

/** Which font a cell's text wants. */
object Glyphs {
    /**
     * Colour emoji: text asking for it (VS16), or a symbol the terminal made
     * two cells wide, which it does for emoji presented as emoji by default
     * (⭐ 😀 ✅). One-cell symbols (Claude Code's ⏺ ⏸ ⏵) and wide CJK stay text.
     */
    fun colourEmoji(s: String, wide: Boolean): Boolean {
        if (s.contains('️')) return true
        if (!wide || s.isEmpty()) return false
        val cp = s.codePointAt(0)
        return cp in 0x2000..0x2BFF || cp in 0x1F000..0x1FAFF
    }
}
