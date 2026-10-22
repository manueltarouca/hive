package xyz.matm.hive.agents

import org.junit.Assert.assertEquals
import org.junit.Test

class PreviewsTest {
    // The bottom of a Claude Code pane, as the tab sheet used to show it.
    private val claude = listOf(
        "● Updated docs/scripts.md with the trigger examples",
        "  ⎿  Wrote 42 lines",
        "",
        "────────────────────────────────────────────",
        "> ",
        "────────────────────────────────────────────",
        "  ⏵⏵ bypass permissions on (shift+tab to cycle)",
    )

    @Test fun anAgentsChromeIsLeftOut() {
        assertEquals(listOf("● Updated docs/scripts.md with the trigger examples", "  ⎿  Wrote 42 lines"), Previews.pick(claude, null))
    }

    @Test fun otherFootersToo() {
        val manual = listOf("● Looking at the image", "──────", "❯ ", "──────", "  ⏸ manual mode on · ? for shortcuts")
        assertEquals(listOf("● Looking at the image"), Previews.pick(manual, null))
        val picker = listOf("Which one?", " ❯ 1. Postgres", "   2. SQLite", "Enter to select · ↑/↓ to navigate · Esc to cancel")
        assertEquals(listOf(" ❯ 1. Postgres", "   2. SQLite"), Previews.pick(picker, null))
    }

    @Test fun nothingLeftFallsBackToTheAgentsMessage() {
        val bare = listOf("──────", "> ", "──────", "  ⏵⏵ bypass permissions on (shift+tab to cycle)")
        assertEquals(listOf("Claude needs your permission to use Bash"), Previews.pick(bare, "Claude needs your permission to use Bash"))
        assertEquals(emptyList<String>(), Previews.pick(bare, ""))
    }

    @Test fun aShellIsShownAsIs() {
        assertEquals(listOf("ok  github.com/x/session 0.4s", "~/src $ "), Previews.pick(listOf("PASS", "ok  github.com/x/session 0.4s", "~/src $ "), null))
    }

    @Test fun glyphsTheFontLacksBecomeOnesItHas() {
        assertEquals("▸▸ on ‖ paused ● rec", Previews.text("⏵⏵ on ⏸️ paused ⏺ rec"))
    }
}
