package xyz.matm.hive.agents

/** One of the answers a blocked agent offers, and the key that picks it. */
class Choice(val key: String, val label: String) {
    /** For a button: "Yes", "Yes, this session", "No". */
    val short: String
        get() {
            val first = label.substringBefore(',').trim()
            val l = label.lowercase()
            if (first.equals("yes", true) && (l.contains("this session") || l.contains("during this session"))) return "Yes, this session"
            return if (first.length <= 24) first else first.take(24).trimEnd()
        }
}

/**
 * A question an agent is asking at the bottom of its pane, with numbered
 * answers, such as Claude Code's permission dialog. The server only says
 * an agent is blocked and why; the answers are read off the screen.
 */
class Ask(val question: String, val choices: List<Choice>, val selected: Int) {
    companion object {
        private val option = Regex("""^(\s*)([❯›>]\s*)?(\d)\.\s+(.*)$""")
        private const val WINDOW = 30

        /** The ask in a pane's rows, or null when its last lines aren't one. */
        fun find(rows: List<String>): Ask? {
            val lines = rows.map { it.trim('│', '┃', '║').trimEnd() }.toMutableList()
            // Below the answers: blanks and the dialog's border only.
            while (lines.isNotEmpty() && (lines.last().isBlank() || lines.last().trim().all { it in "─━═╰╯╭╮└┘┌┐ " })) lines.removeAt(lines.lastIndex)
            if (lines.isEmpty()) return null
            val from = maxOf(0, lines.size - WINDOW)
            for (q in lines.lastIndex downTo from) {
                val text = lines[q].trim()
                if (!text.endsWith("?") || option.matches(lines[q])) continue
                return parse(text, lines.subList(q + 1, lines.size))
            }
            return null
        }

        private fun parse(question: String, rest: List<String>): Ask? {
            val keys = ArrayList<String>()
            val labels = ArrayList<StringBuilder>()
            var selected = 0
            var indent = 0
            for (line in rest) {
                if (line.isBlank()) { if (keys.isEmpty()) continue else return null }
                val m = option.matchEntire(line)
                if (m != null) {
                    if (m.groupValues[3] != (keys.size + 1).toString()) return null
                    if (m.groupValues[2].isNotEmpty()) selected = keys.size
                    keys += m.groupValues[3]
                    labels += StringBuilder(m.groupValues[4].trim())
                    indent = m.groupValues[1].length + m.groupValues[2].length
                } else {
                    // A long answer wraps onto the next line, indented at least as far.
                    val lead = line.length - line.trimStart().length
                    if (keys.isEmpty() || lead < indent) return null
                    labels.last().append(' ').append(line.trim())
                }
            }
            if (keys.size < 2) return null
            val choices = keys.indices.map { i ->
                Choice(keys[i], labels[i].toString().removeSuffix("(esc)").trim().replace(Regex("\\s+"), " "))
            }
            return Ask(question, choices, selected)
        }
    }
}
