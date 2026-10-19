package xyz.matm.hive.agents

import org.junit.Assert.assertEquals
import org.junit.Test

class NoticesTest {
    private fun a(block: String, state: String, label: String = "wkspc") =
        Agent(blockID = block, sessionID = "s", host = "h", sessionLabel = label, harness = "claude", state = state, message = "m", title = "")

    @Test fun notifiesWhenAnAgentBecomesBlockedOrDone() {
        val before = mapOf("b1" to "working", "b2" to "blocked", "b3" to "done")
        val now = listOf(a("b1", "blocked"), a("b2", "blocked"), a("b3", "working"), a("b4", "done"))
        assertEquals(listOf("b1" to "blocked", "b4" to "done"), Notices.changes(before, now).map { it.blockID to it.state })
    }

    @Test fun theFirstLoadIsNotNews() {
        assertEquals(emptyList<Agent>(), Notices.changes(null, listOf(a("b1", "blocked"))))
    }

    @Test fun wording() {
        assertEquals("Claude Code needs you", Notices.title(a("b", "blocked")))
        assertEquals("Claude Code is done", Notices.title(a("b", "done")))
        assertEquals("wkspc", Notices.subtitle(a("b", "done"), hostName = "This Mac", local = true))
        assertEquals("wkspc on hive", Notices.subtitle(a("b", "done"), hostName = "hive", local = false))
        assertEquals("a session on hive", Notices.subtitle(a("b", "done", label = ""), hostName = "hive", local = false))
    }

    private val dialog = listOf(" Do you want to proceed?", " ❯ 1. Yes", "   2. No, and tell Claude what to do differently (esc)")

    @Test fun allowOnlyAnswersTheQuestionItDescribed() {
        val now = a("b", "blocked").copy(message = "Claude needs your permission to use Bash")
        assertEquals(true, Notices.allowable("Claude needs your permission to use Bash", now, dialog))
        // It moved on to another question meanwhile.
        assertEquals(false, Notices.allowable("Claude needs your permission to use Bash", now.copy(message = "delete the branch?"), dialog))
        assertEquals(false, Notices.allowable(now.message, now.copy(state = "working"), dialog))
        assertEquals(false, Notices.allowable(now.message, null, dialog))
        // The screen isn't a dialog whose first answer is yes.
        assertEquals(false, Notices.allowable(now.message, now, listOf("> ")))
        assertEquals(false, Notices.allowable(now.message, now, listOf(" Pick a plan?", " ❯ 1. Rewrite it", "   2. Keep it")))
    }

    @Test fun allowIsOfferedForClaudeOnly() {
        assertEquals(true, Notices.offersAllow(a("b", "blocked")))
        assertEquals(false, Notices.offersAllow(a("b", "blocked").copy(harness = "codex")))
        assertEquals(false, Notices.offersAllow(a("b", "done")))
    }
}
