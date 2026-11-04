package xyz.matm.hive.model

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import xyz.matm.hive.agents.Agent
import xyz.matm.hive.agents.Notices
import java.time.Instant

class ScriptsTest {
    private val listJson = """
        {"scripts": [
          {"name": "needs-you", "path": "/s/needs-you.ts", "state": "running", "permissions": ["read", "ui"], "description": "Reminds you.",
           "errors": 0, "overruns": 0, "loaded_at": "2026-10-22T09:00:00Z", "mode": "watch",
           "options": [
             {"key": "remind_min", "title": "Remind me after", "type": "number", "default": 10, "min": 1, "max": 240, "value": 15},
             {"key": "include_done", "title": "", "type": "bool", "default": false, "value": true},
             {"key": "greeting", "title": "Greeting", "type": "text", "default": "hi", "value": null}
           ]},
          {"name": "digest", "path": "/s/digest.ts", "state": "pending", "permissions": [], "asks": ["screen", "input", "ui"],
           "errors": 0, "overruns": 0, "mode": "act", "options": []},
          {"name": "Broken", "state": "failed", "error": "SyntaxError: unexpected token\n  at line 3", "permissions": ["read"], "mode": "act"}
        ], "runs": []}
    """.trimIndent()

    @Test fun parsesResidentsSortedByName() {
        val list = Resident.list(JSONObject(listJson))
        assertEquals(listOf("Broken", "digest", "needs-you"), list.map { it.name })
        val n = list[2]
        assertTrue(n.watching)
        assertTrue(n.on)
        assertEquals("Watching only", n.stateLine)
        assertEquals(listOf("notify you, mark panes and bring hive to the front"), n.may)
        assertEquals(3, n.options.size)
        val num = n.options[0]
        assertEquals("number", num.type)
        assertEquals(1.0, num.min!!, 0.0)
        assertEquals(240.0, num.max!!, 0.0)
        assertEquals("15", num.shown)
        assertEquals("1 to 240", num.range)
        assertTrue(n.options[1].on)
        assertEquals("include_done", n.options[1].label) // no title: its key
        assertEquals("hi", n.options[2].shown) // no value: the default
        assertNull(n.options[2].min)
    }

    @Test fun stateLines() {
        val list = Resident.list(JSONObject(listJson)).associateBy { it.name }
        assertEquals("Needs your OK", list["digest"]!!.stateLine)
        assertEquals("Failed: SyntaxError: unexpected token", list["Broken"]!!.stateLine)
        val r = list["digest"]!!
        assertEquals("Turned off", r.copy(state = "disabled").stateLine)
        assertFalse(r.copy(state = "disabled").on)
        assertEquals("Paused after running too long, too often", r.copy(state = "quarantined").stateLine)
        assertFalse(r.copy(state = "quarantined").on)
        assertEquals("Ended on its own", r.copy(state = "stopped").stateLine)
        assertEquals("Running", r.copy(state = "running").stateLine)
    }

    @Test fun numbersRespectTheirRange() {
        val o = ScriptOption("n", "N", "number", 10, 1.0, 240.0, 15)
        assertEquals(20.0 to null, o.parseNumber(" 20 "))
        assertEquals(null to "At least 1.", o.parseNumber("0"))
        assertEquals(null to "At most 240.", o.parseNumber("300"))
        assertEquals(null to "Give a number.", o.parseNumber("ten"))
        assertEquals(2.5 to null, o.parseNumber("2,5"))
        assertEquals("2.5", ScriptOption.show(2.5))
        assertEquals("10", ScriptOption.show(10.0))
    }

    @Test fun permissionsInPlainWords() {
        assertEquals(
            listOf("read terminals", "type into terminals", "notify you, mark panes and bring hive to the front", "open panes running programs", "arrange panes", "change settings", "manage the server"),
            ScriptWords.permissions(listOf("read", "input", "screen", "ui", "spawn", "control", "config", "server")),
        )
        assertEquals("It may notify you, mark panes and bring hive to the front.", ScriptWords.sentence(listOf("read", "ui")))
        assertEquals("It asks to read terminals, type into terminals and notify you, mark panes and bring hive to the front.", ScriptWords.sentence(listOf("ui", "input", "screen"), "It asks to"))
        assertNull(ScriptWords.sentence(listOf("read")))
        assertEquals("Can read your terminals and type into them, agents included.", ScriptWords.warning(listOf("screen", "input")))
        assertNull(ScriptWords.warning(listOf("input")))
    }

    private fun entry(principal: String, summary: String, result: String = "ok", preview: String = "", seq: Long = 1, method: String = "block.input") = JSONObject()
        .put("t", "2026-10-22T10:00:00Z").put("principal", principal).put("script", "limit-resume").put("method", method)
        .put("permission", "input").put("result", result).put("seq", seq).put("summary", summary).put("preview", preview)

    @Test fun entriesInPlainWords() {
        val list = ActivityEntry.list(JSONObject().put("entries", JSONArray()
            .put(entry("script:limit-resume", "typed 7 characters", preview = "go on", seq = 1))
            .put(entry("script:limit-resume", "typed 3 characters", result = "watching", seq = 2))
            .put(entry("peer:501", "mode watch", seq = 3, method = "script.set_mode"))
            .put(entry("tailscale:mt via tailnet (notification button)", "auto = false", seq = 4, method = "script.set_option"))
            .put(entry("script:limit-resume", "answered the agent", result = "not_found", seq = 5))))
        assertEquals(listOf(5L, 4L, 3L, 2L, 1L), list.map { it.seq }) // newest first
        assertEquals("answered the agent, failed: not found", ScriptWords.entry(list[0]))
        assertEquals("set “auto” to off by you from a notification", ScriptWords.entry(list[1]))
        val auto = ScriptOption("auto", "Type it by itself", "bool", null, null, null, null)
        assertEquals("set “Type it by itself” to off by you from a notification", ScriptWords.entry(list[1], listOf(auto)))
        assertEquals("set to watch only by you", ScriptWords.entry(list[2]))
        assertEquals("typed 3 characters, refused: the script can only watch", ScriptWords.entry(list[3]))
        assertEquals("typed 7 characters “go on”", ScriptWords.entry(list[4]))
        assertTrue(list[4].byScript)
        fun person(method: String, summary: String) = ScriptWords.entry(ActivityEntry.of(entry("peer:501", summary, method = method)))
        assertEquals("turned off by you", person("script.disable", "turned off"))
        assertEquals("allowed by you", person("script.grant", "granted"))
        assertEquals("let act again by you", person("script.set_mode", "mode act"))
        assertFalse(list[1].byScript)
    }

    @Test fun chipWords() {
        assertEquals("limit-resume answered the agent", ScriptWords.acted("limit-resume", "answered the agent", "ok"))
        assertEquals("limit-resume was stopped from acting", ScriptWords.acted("limit-resume", "typed 3 characters", "watching"))
    }

    @Test fun agoInPlainWords() {
        val now = Instant.parse("2026-10-22T12:00:00Z")
        assertEquals("just now", ScriptWords.ago("2026-10-22T11:59:30Z", now))
        assertEquals("3 min ago", ScriptWords.ago("2026-10-22T11:57:00Z", now))
        assertEquals("2 h ago", ScriptWords.ago("2026-10-22T09:30:00Z", now))
        assertEquals("yesterday", ScriptWords.ago("2026-10-21T09:00:00Z", now))
        assertEquals("4 days ago", ScriptWords.ago("2026-10-18T09:00:00Z", now))
        assertEquals("3 min ago", ScriptWords.ago("2026-10-22T11:57:00.123456+00:00".replace("+00:00", "Z"), now))
        assertEquals("", ScriptWords.ago("", now))
    }

    @Test fun hostSummary() {
        val list = Resident.list(JSONObject(listJson))
        assertEquals("1 running, 1 waiting for you, 1 failed", ScriptWords.summary(list))
        assertEquals("No scripts", ScriptWords.summary(emptyList()))
        assertEquals("1 off", ScriptWords.summary(listOf(list[0].copy(state = "disabled"))))
    }

    @Test fun stoppedAgents() {
        val a = Agent("b1", "s1", "h", "work", "claude", "done", "You've hit your session limit", "", kind = "error")
        assertEquals("stopped", a.shown)
        assertEquals("Claude Code stopped", Notices.title(a))
        assertEquals("You've hit your session limit", Notices.text(a))
        assertEquals("done", a.copy(kind = "").shown)
        assertEquals("Claude Code is done", Notices.title(a.copy(kind = "")))
        // done to stopped is news; the same stop again isn't.
        assertEquals(listOf("b1"), Notices.changes(mapOf("b1" to "done"), listOf(a)).map { it.blockID })
        assertEquals(emptyList<Agent>(), Notices.changes(mapOf("b1" to "stopped"), listOf(a)))
    }

    @Test fun builtinButtonsKeepTheirNames() {
        val n = ScriptNote.of(JSONObject().put("notification_id", "n1").put("title", "Resumed").put("actions", JSONArray()
            .put(JSONObject().put("title", "Stop doing this").put("action", "hive.set_option").put("args", JSONObject().put("key", "auto").put("value", false)))
            .put(JSONObject().put("title", "Watch only").put("action", "hive.set_mode").put("args", JSONObject().put("mode", "watch")))))
        assertEquals(listOf("hive.set_option", "hive.set_mode"), n.actions.map { it.action })
        assertEquals(false, JSONObject(n.actions[0].args).get("value"))
    }
}
