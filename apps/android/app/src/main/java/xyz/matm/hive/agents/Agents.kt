package xyz.matm.hive.agents

/** An agent in a pane, from agent.list. state: idle, working, blocked, done, ended. */
data class Agent(
    val blockID: String,
    val sessionID: String,
    val host: String,
    val sessionLabel: String,
    val harness: String,
    val state: String,
    val message: String,
    val title: String,
)

object Harness {
    fun name(harness: String): String = Programs.lookup(harness).name.takeIf { harness.isNotEmpty() && it != harness } ?: when (harness.lowercase()) {
        "claude" -> "Claude Code"
        "codex" -> "Codex"
        "opencode" -> "OpenCode"
        "gemini" -> "Gemini"
        "grok" -> "Grok"
        "" -> "An agent"
        else -> harness.replaceFirstChar { it.uppercase() }
    }
}

/** What to notify, as the Mac app does: an agent that became blocked or done. */
object Notices {
    /** before: each block's last state, or null on the first load (nothing is news then). */
    fun changes(before: Map<String, String>?, now: List<Agent>): List<Agent> {
        if (before == null) return emptyList()
        return now.filter { (it.state == "blocked" || it.state == "done") && before[it.blockID] != it.state }
    }

    /**
     * Whether Allow from a notification may answer now: the agent is still
     * blocked on the question the notification described, and the pane
     * shows a dialog whose first answer is a yes. Anything else opens the pane.
     */
    fun allowable(described: String, now: Agent?, pane: List<String>): Boolean {
        if (now == null || now.state != "blocked" || now.message != described) return false
        val first = Ask.find(pane)?.choices?.firstOrNull() ?: return false
        return first.key == "1" && first.label.lowercase().startsWith("yes")
    }

    /** Allow is Claude Code's "1. Yes"; other harnesses ask differently, so they get Reply and Deny only. */
    fun offersAllow(a: Agent) = a.state == "blocked" && a.harness.lowercase() == "claude"

    fun title(a: Agent) = Harness.name(a.harness) + if (a.state == "blocked") " needs you" else " is done"

    /** Where it is: the session, and the host unless it's this phone's own (never, today) or the only one. */
    fun subtitle(a: Agent, hostName: String, local: Boolean): String {
        if (local) return a.sessionLabel
        return a.sessionLabel.ifEmpty { "a session" } + " on " + hostName
    }
}
