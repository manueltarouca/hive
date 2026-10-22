package xyz.matm.hive.model

import org.json.JSONArray
import org.json.JSONObject
import java.time.Duration
import java.time.Instant

/** One of a script's switches, from script.list. */
data class ScriptOption(
    val key: String,
    val title: String,
    /** bool, number or text. */
    val type: String,
    val default: Any?,
    val min: Double?,
    val max: Double?,
    val value: Any?,
) {
    val label: String get() = title.ifEmpty { key }
    val on: Boolean get() = (value ?: default) == true
    /** The value as the field shows it: 10, not 10.0. */
    val shown: String get() = show(value ?: default)

    companion object {
        fun of(j: JSONObject) = ScriptOption(
            j.optString("key"), j.optString("title"), j.optString("type", "text"),
            j.opt("default").takeUnless { it == JSONObject.NULL }, j.optDouble("min").takeUnless { it.isNaN() },
            j.optDouble("max").takeUnless { it.isNaN() }, j.opt("value").takeUnless { it == JSONObject.NULL },
        )

        fun show(v: Any?): String = when (v) {
            null -> ""
            is Number -> v.toDouble().let { d -> if (d == Math.floor(d) && !d.isInfinite() && Math.abs(d) < 1e15) d.toLong().toString() else d.toString() }
            else -> v.toString()
        }
    }

    /** What typed text means for a number: the value to send, or why not (null value). */
    fun parseNumber(text: String): Pair<Double?, String?> {
        val d = text.trim().replace(',', '.').toDoubleOrNull() ?: return null to "Give a number."
        if (min != null && d < min) return null to "At least ${show(min)}."
        if (max != null && d > max) return null to "At most ${show(max)}."
        return d to null
    }

    /** The range in words, for under a number: "1 to 240". */
    val range: String?
        get() = when {
            min != null && max != null -> "${show(min)} to ${show(max)}"
            min != null -> "At least ${show(min)}"
            max != null -> "At most ${show(max)}"
            else -> null
        }
}

/** A script in the server's scripts folder, from script.list. */
data class Resident(
    val name: String,
    val path: String,
    /** running, pending, failed, disabled, quarantined or stopped. */
    val state: String,
    val permissions: List<String>,
    val asks: List<String>,
    val description: String,
    val error: String,
    val errors: Int,
    val overruns: Int,
    val loadedAt: String,
    /** act or watch. */
    val mode: String,
    val options: List<ScriptOption>,
) {
    val watching: Boolean get() = mode == "watch"
    /** The on/off switch: off when turned off, or paused for overrunning. */
    val on: Boolean get() = state != "disabled" && state != "quarantined"
    val pending: Boolean get() = state == "pending"

    /** Its state in plain words. */
    val stateLine: String
        get() = when (state) {
            "running" -> if (watching) "Running, watch only" else "Running"
            "pending" -> "Waiting for your OK"
            "disabled" -> "Turned off"
            "stopped" -> "Stopped by itself"
            "failed" -> "Failed" + (error.lineSequence().firstOrNull()?.trim()?.takeIf { it.isNotEmpty() }?.let { ": $it" } ?: "")
            "quarantined" -> "Paused: it ran too long too often"
            else -> state.replaceFirstChar { it.uppercase() }
        }

    /** What it may do, in plain words; read is implicit. */
    val may: List<String> get() = ScriptWords.permissions(permissions)

    companion object {
        fun of(j: JSONObject) = Resident(
            j.optString("name"), j.optString("path"), j.optString("state"), strings(j.optJSONArray("permissions")),
            strings(j.optJSONArray("asks")), j.optString("description"), j.optString("error"), j.optInt("errors"),
            j.optInt("overruns"), j.optString("loaded_at"), j.optString("mode", "act").ifEmpty { "act" },
            j.optJSONArray("options")?.let { a -> (0 until a.length()).map { ScriptOption.of(a.getJSONObject(it)) } } ?: emptyList(),
        )

        /** script.list's scripts, by name. */
        fun list(j: JSONObject): List<Resident> {
            val a = j.optJSONArray("scripts") ?: return emptyList()
            return (0 until a.length()).map { of(a.getJSONObject(it)) }.sortedBy { it.name.lowercase() }
        }

        private fun strings(a: JSONArray?): List<String> = if (a == null) emptyList() else (0 until a.length()).map { a.optString(it) }
    }
}

/** One line of what a script did (or what a person did to it), from audit.list. */
data class ActivityEntry(
    val time: String,
    val principal: String,
    val script: String,
    val method: String,
    val result: String,
    val seq: Long,
    val summary: String,
    val blockID: String?,
    val preview: String,
) {
    val byScript: Boolean get() = principal.startsWith("script:")

    companion object {
        fun of(j: JSONObject) = ActivityEntry(
            j.optString("t"), j.optString("principal"), j.optString("script"), j.optString("method"), j.optString("result"),
            j.optLong("seq"), j.optString("summary"), j.optString("block_id").ifEmpty { null }, j.optString("preview"),
        )

        /** audit.list's entries, newest first. */
        fun list(j: JSONObject): List<ActivityEntry> {
            val a = j.optJSONArray("entries") ?: return emptyList()
            return (0 until a.length()).map { of(a.getJSONObject(it)) }.sortedByDescending { it.seq }
        }
    }
}

/** A script acted on a pane on screen: the chip says so for a while. */
data class ActedChip(val host: String, val script: String, val block: String, val text: String, val refused: Boolean, val until: Long,
    /** Where it happened, for the desk when that isn't on screen. */
    val session: String = "", val place: String = "")

/** The words the Scripts screen uses. */
object ScriptWords {
    private val perms = linkedMapOf(
        "screen" to "read terminals",
        "input" to "type into terminals",
        "ui" to "notify you, mark panes and bring hive to the front",
        "spawn" to "open panes running programs",
        "control" to "arrange panes",
        "config" to "change settings",
        "server" to "manage the server",
    )

    /** Permissions in plain words, read left out (every script may read). */
    fun permissions(list: List<String>): List<String> {
        val order = perms.keys.toList()
        return list.filter { it != "read" && it.isNotEmpty() }.distinct()
            .sortedBy { order.indexOf(it).let { i -> if (i < 0) order.size else i } }.map { perms[it] ?: it }
    }

    /** One sentence: "It may type into terminals and read terminals." */
    fun sentence(list: List<String>, lead: String = "It may"): String? {
        val words = permissions(list)
        if (words.isEmpty()) return null
        val joined = if (words.size == 1) words[0] else words.dropLast(1).joinToString(", ") + " and " + words.last()
        return "$lead $joined."
    }

    /** The plain warning when a script asks to both read and type. */
    fun warning(asks: List<String>): String? =
        if ("screen" in asks && "input" in asks) "It can read your terminals and type into them, agents included." else null

    /** A result as people read it: nothing for ok. */
    fun result(r: String): String? = when (r) {
        "", "ok" -> null
        "watching" -> "refused: watch only"
        "conflict" -> "not sent: it had moved on"
        "permission_denied" -> "refused: not allowed"
        else -> "failed: " + r.replace('_', ' ')
    }

    /** An entry's line, without its time: what, the typed text, who, how it went. */
    fun entry(e: ActivityEntry, options: List<ScriptOption> = emptyList()): String {
        val what = if (e.byScript) e.summary.ifEmpty { e.method } else personal(e, options)
        val parts = mutableListOf(what)
        if (e.preview.isNotEmpty()) parts += "“${e.preview}”"
        if (!e.byScript) {
            parts += "by you"
            if (e.principal.endsWith("(notification button)")) parts += "from a notification"
        }
        val line = parts.joinToString(" ")
        return result(e.result)?.let { "$line, $it" } ?: line
    }

    /** What a person did to a script, in words: "set to watch only", "set “Remind me after” to 30". */
    private fun personal(e: ActivityEntry, options: List<ScriptOption>): String = when (e.method) {
        "script.set_mode" -> if (e.summary.endsWith("watch")) "set to watch only" else "let act"
        "script.enable" -> "turned on"
        "script.disable" -> "turned off"
        "script.grant" -> "allowed"
        "script.revoke" -> "permissions taken away"
        "script.set_option" -> e.summary.split(" = ", limit = 2).let { kv ->
            if (kv.size != 2) return@let e.summary
            val label = options.firstOrNull { it.key == kv[0] }?.title?.ifEmpty { null } ?: kv[0]
            val value = when (kv[1]) { "true" -> "on"; "false" -> "off"; else -> kv[1] }
            "set “$label” to $value"
        }
        else -> e.summary.ifEmpty { e.method }
    }

    /** The chip on a pane: "limit-resume answered the agent", or that it was refused. */
    fun acted(script: String, summary: String, result: String): String =
        if (result == "watching") "$script was stopped from acting" else "$script ${summary.ifEmpty { "acted" }}"

    /** When, as people say it: just now, 3 min ago, 2 h ago, yesterday, 4 days ago. */
    fun ago(t: String, now: Instant = Instant.now()): String {
        val at = runCatching { Instant.parse(t) }.getOrNull() ?: return ""
        val s = Duration.between(at, now).seconds
        return when {
            s < 45 -> "just now"
            s < 3600 -> "${maxOf(1, (s + 30) / 60)} min ago"
            s < 86400 -> "${s / 3600} h ago"
            s < 2 * 86400 -> "yesterday"
            else -> "${s / 86400} days ago"
        }
    }

    /** A host's scripts in a few words, for its Settings row: "3 running, 1 waiting for you". */
    fun summary(list: List<Resident>): String {
        if (list.isEmpty()) return "No scripts"
        val running = list.count { it.state == "running" }
        val waiting = list.count { it.pending }
        val off = list.count { !it.on }
        val failed = list.count { it.state == "failed" }
        val parts = listOfNotNull(
            running.takeIf { it > 0 }?.let { "$it running" },
            waiting.takeIf { it > 0 }?.let { "$it waiting for you" },
            failed.takeIf { it > 0 }?.let { "$it failed" },
            off.takeIf { it > 0 }?.let { "$it off" },
        )
        return parts.joinToString(", ").ifEmpty { if (list.size == 1) "1 script" else "${list.size} scripts" }
    }
}
