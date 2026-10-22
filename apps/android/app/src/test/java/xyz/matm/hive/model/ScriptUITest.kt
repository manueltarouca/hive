package xyz.matm.hive.model

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Test

class ScriptUITest {
    private fun badge(block: String, app: String, id: String, state: String, msg: String = "") =
        JSONObject().put("block_id", block).put("app", app).put("id", id).put("state", state).put("msg", msg)

    @Test fun badgesFromAListThenEvents() {
        var book = BadgeBook().load(JSONObject().put("badges", JSONArray().put(badge("b1", "script:t", "tests", "error", "3 failing")).put(badge("b1", "cli", "ci", "working"))))
        assertEquals(listOf("tests", "ci"), book.badges("b1").map { it.id }) // failing first
        book = book.apply(badge("b1", "script:t", "tests", "done", "pass")).apply(badge("b1", "cli", "ci", "clear"))
        assertEquals(listOf("pass"), book.badges("b1").map { it.msg })
        assertEquals(emptyList<ScriptBadge>(), book.badges("b2"))
    }

    private fun note(id: String, t: String, acked: Boolean = false) =
        JSONObject().put("notification_id", id).put("time", t).put("title", "T $id").put("acked", acked)

    @Test fun replayShowsTheNewestAndCountsTheRest() {
        val arr = JSONArray()
        for (i in 1..8) arr.put(note("n$i", "2026-10-21T10:0$i:00Z"))
        arr.put(note("old", "2026-10-21T09:00:00Z"))
        arr.put(note("frac", "2026-10-21T10:00:29.9Z")) // a fraction of a second before what was seen
        val r = NoteReplay.of(JSONObject().put("notifications", arr), seenUntil = "2026-10-21T10:00:29.95Z", alreadyShown = setOf("n1"), max = 3)
        assertEquals(listOf("n6", "n7", "n8"), r.show.map { it.id })
        assertEquals(4, r.more)
        assertEquals("2026-10-21T10:08:00Z", r.latest)
    }

    @Test fun stepsShowWhatButtonsDo() {
        val n = ScriptNote.of(JSONObject().put("notification_id", "n").put("title", "Approve?").put("body", "Read foo")
            .put("actions", JSONArray().put(JSONObject().put("title", "Approve").put("action", "rule:a").put("args", JSONObject().put("revision", 7)).put("step", "Press enter in the agent's pane"))))
        assertEquals("rule:a", n.actions[0].action)
        assertEquals("{\"revision\":7}", n.actions[0].args)
        assertEquals("Read foo\nApprove: Press enter in the agent's pane", n.bodyWithSteps)
        assertEquals("Read foo. Approve: press enter in the agent's pane.", n.oneLine)
    }
}
