package xyz.matm.hive.agents

/**
 * What a tab's card shows of its pane: the last lines that say what's going
 * on. An agent's own chrome (its dividers, empty prompt and status bar) is
 * left out, or every agent's card would read the same.
 */
object Previews {
    private val rule = Regex("^[\\s─━═╌╍┄┅┈┉▔▁-]+$")
    private val prompt = setOf(">", "❯", "›", "$", "%")
    private val chrome = Regex(
        "bypass permissions|accept edits on|plan mode on|manual mode on|auto-accept|shift\\+tab to cycle|\\? for shortcuts|" +
            "esc to interrupt|Enter to select|to navigate|ctrl\\+[a-z] to ",
        RegexOption.IGNORE_CASE,
    )

    /** [tail] is the pane's last lines, oldest first; [message] an agent's latest words. */
    fun pick(tail: List<String>, message: String?, n: Int = 2): List<String> {
        val kept = tail.filter { l ->
            val t = l.trim()
            t.isNotEmpty() && !rule.matches(l) && t !in prompt && !chrome.containsMatchIn(l)
        }.map(::text)
        if (kept.isNotEmpty()) return kept.takeLast(n)
        return listOfNotNull(message?.takeIf { it.isNotBlank() }?.let(::text))
    }

    /** Media-control symbols the fonts lack (they'd show as boxes or emoji), as shapes they have. */
    fun text(s: String): String = buildString {
        for (c in s) when (c) {
            '⏵', '▶' -> append('▸')
            '⏴', '◀' -> append('◂')
            '⏶' -> append('▴')
            '⏷' -> append('▾')
            '⏸' -> append('‖')
            '⏹' -> append('■')
            '⏺' -> append('●')
            '️', '︎' -> {} // emoji or text presentation: the plain shape either way
            else -> append(c)
        }
    }
}
