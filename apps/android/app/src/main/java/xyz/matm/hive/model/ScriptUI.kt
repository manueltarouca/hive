package xyz.matm.hive.model

import org.json.JSONObject

/** A badge a script (or the CLI) put on a pane, as the Mac shows it. */
data class ScriptBadge(val block: String, val app: String, val id: String, val state: String, val msg: String, val title: String, val progress: Int?) {
    val text: String get() = msg.ifEmpty { title.ifEmpty { id } }
    /** Badges that need you sort first. */
    val rank: Int get() = when (state) { "error", "blocked" -> 0; "working" -> 1; "done" -> 2; else -> 3 }

    companion object {
        fun of(j: JSONObject) = ScriptBadge(j.optString("block_id"), j.optString("app"), j.optString("id"), j.optString("state", "idle"),
            j.optString("msg"), j.optString("title"), if (j.has("progress") && !j.isNull("progress")) j.optInt("progress") else null)
    }
}

/** Every pane's badges on one host, from status.list and status_changed. Immutable, for Compose state. */
class BadgeBook(private val byBlock: Map<String, Map<String, ScriptBadge>> = emptyMap()) {
    fun load(list: JSONObject): BadgeBook {
        var b = BadgeBook()
        val arr = list.optJSONArray("badges") ?: return b
        for (i in 0 until arr.length()) b = b.apply(arr.getJSONObject(i))
        return b
    }

    fun apply(j: JSONObject): BadgeBook {
        val b = ScriptBadge.of(j)
        if (b.block.isEmpty()) return this
        val m = byBlock.toMutableMap()
        val pane = (m[b.block] ?: emptyMap()).toMutableMap()
        if (b.state == "clear") pane.remove(b.app + "/" + b.id) else pane[b.app + "/" + b.id] = b
        if (pane.isEmpty()) m.remove(b.block) else m[b.block] = pane
        return BadgeBook(m)
    }

    fun badges(block: String): List<ScriptBadge> =
        (byBlock[block]?.values ?: emptyList()).sortedWith(compareBy({ it.rank }, { it.id }, { it.app }))
}

/** A notification a script sent. */
data class ScriptNote(val id: String, val time: String, val title: String, val body: String, val block: String?, val session: String?, val actions: List<Action>, val acked: Boolean) {
    data class Action(val title: String, val action: String, val args: String, val step: String)

    /** The body, then what each button does. */
    val bodyWithSteps: String get() = (listOf(body).filter { it.isNotEmpty() } + actions.filter { it.step.isNotEmpty() }.map { "${it.title}: ${it.step}" }).joinToString("\n")

    /** The same on one line, for a collapsed notification: "2 failures. Ping: puts a badge on the pane." */
    val oneLine: String get() {
        val steps = actions.filter { it.step.isNotEmpty() }.joinToString("; ") { "${it.title}: ${it.step.replaceFirstChar(Char::lowercaseChar)}" }
        return listOf(body.trimEnd('.', ' '), steps).filter { it.isNotEmpty() }.joinToString(". ").let { if (it.isEmpty()) it else "$it." }
    }

    companion object {
        fun of(j: JSONObject): ScriptNote {
            val arr = j.optJSONArray("actions")
            val actions = if (arr == null) emptyList() else (0 until arr.length()).map {
                val a = arr.getJSONObject(it)
                Action(a.optString("title"), a.optString("action"), a.optJSONObject("args")?.toString() ?: "{}", a.optString("step"))
            }
            return ScriptNote(j.optString("notification_id"), j.optString("time"), j.optString("title"), j.optString("body"),
                j.optString("block_id").ifEmpty { null }, j.optString("session_id").ifEmpty { null }, actions, j.optBoolean("acked"))
        }
    }
}

/** What to show of what a server kept while the phone was away: the newest few, and how many more. */
data class NoteReplay(val show: List<ScriptNote>, val more: Int, val latest: String?) {
    companion object {
        private fun at(t: String): java.time.Instant = runCatching { java.time.Instant.parse(t) }.getOrDefault(java.time.Instant.EPOCH)

        fun of(list: JSONObject, seenUntil: String?, alreadyShown: Set<String>, max: Int = 3): NoteReplay {
            val arr = list.optJSONArray("notifications")
            val all = if (arr == null) emptyList() else (0 until arr.length()).map { ScriptNote.of(arr.getJSONObject(it)) }
            val seen = seenUntil?.let(::at)
            val fresh = all.filter { !it.acked && it.id !in alreadyShown && (seen == null || at(it.time) > seen) }
            val show = fresh.takeLast(max)
            return NoteReplay(show, fresh.size - show.size, fresh.maxByOrNull { at(it.time) }?.time)
        }
    }
}
