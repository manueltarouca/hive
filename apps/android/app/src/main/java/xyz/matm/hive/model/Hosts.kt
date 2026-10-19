package xyz.matm.hive.model

import org.json.JSONArray
import org.json.JSONObject
import xyz.matm.hive.net.Endpoint
import java.util.Base64

/**
 * A named remote server, as hosts.json on the Mac (internal/hosts): an
 * address (a URL or a bare host), reached on demand unless saved as always.
 */
data class Host(val label: String, val address: String, val connect: String? = null) {
    val always get() = connect == ALWAYS

    companion object {
        const val ALWAYS = "always"
        const val ON_DEMAND = "on-demand"
    }
}

object Hosts {
    /** The server's CheckLabel: null when fine, else why not. */
    fun checkLabel(l: String): String? = when {
        l.isEmpty() -> "A host needs a label"
        l.any { it in "/:@ \t" } || l.startsWith("~") || l.startsWith(".") || l.startsWith("-") ->
            "Letters, digits, '-' and '_' only, not starting with '-'"
        l.contains('.') -> "A label with a dot reads as a host name; use something like ${l.replace('.', '-')}"
        else -> null
    }

    fun checkAddress(a: String): String? =
        if (Endpoint.candidates(a).isEmpty()) "Give a URL (https://box.example.ts.net) or a host name" else null

    /** Saved again (case-insensitive label): replaced, keeping its connect choice unless one is given. */
    fun add(hs: List<Host>, h: Host): List<Host> {
        val i = hs.indexOfFirst { it.label.equals(h.label, ignoreCase = true) }
        val keep = if (h.connect == null && i >= 0) hs[i].connect else h.connect
        val saved = h.copy(connect = if (keep == Host.ON_DEMAND) null else keep)
        return if (i >= 0) hs.toMutableList().also { it[i] = saved } else hs + saved
    }

    /** A label for a host added by its address, as the Mac makes one: its first name (box, for box.example.ts.net), made unique. */
    fun newLabel(address: String, taken: List<String>): String {
        var host = address.trim().substringAfter("://")
        host = host.substringBefore('/')
        host = if (host.startsWith("[")) host.drop(1).substringBefore(']') else if (host.count { it == ':' } == 1) host.substringBefore(':') else host
        val isIP = host.all { it.isDigit() || it in 'a'..'f' || it in 'A'..'F' || it == '.' || it == ':' }
        var base = (if (isIP) host else host.substringBefore('.'))
            .map { if (it.isLetterOrDigit() || it == '-' || it == '_') it else '-' }.joinToString("").trimStart('-')
        if (base.isEmpty()) base = "host"
        val used = taken.map { it.lowercase() }.toSet()
        var label = base
        var n = 2
        while (label.lowercase() in used) label = "$base-${n++}"
        return label
    }

    /** "https://h, https://h:4483, http://h:4483" as "Tried HTTPS on 443 and 4483, and HTTP on 4483." */
    fun tried(urls: List<String>): String {
        val ports = LinkedHashMap<String, MutableList<String>>()
        for (u in urls) {
            val e = Endpoint.parse(u) ?: continue
            ports.getOrPut(if (e.tls) "HTTPS" else "HTTP") { ArrayList() } += e.port.toString()
        }
        val parts = ports.map { (scheme, ps) -> "$scheme on ${ps.joinToString(" and ")}" }
        if (parts.isEmpty()) return ""
        return "Tried " + (if (parts.size == 1) parts[0] else parts.dropLast(1).joinToString(", ") + ", and " + parts.last()) + "."
    }

    /** Which server answered (server.version): its own ID, else its tailnet name, else its hostname and state. */
    fun identity(v: org.json.JSONObject): String? {
        v.optString("server_id").takeIf { it.length >= 16 }?.let { return "id:$it" }
        v.optJSONObject("tailscale")?.optString("dns_name")?.takeIf { it.isNotEmpty() }?.let { return "ts:$it" }
        val host = v.optString("hostname"); val dir = v.optString("state_dir")
        return if (host.isEmpty() && dir.isEmpty()) null else "$host|$dir"
    }

    fun remove(hs: List<Host>, label: String) = hs.filterNot { it.label.equals(label, ignoreCase = true) }

    fun toJson(hs: List<Host>): String {
        val arr = JSONArray()
        for (h in hs) {
            val o = JSONObject().put("label", h.label).put("address", h.address)
            if (h.always) o.put("connect", Host.ALWAYS)
            arr.put(o)
        }
        return JSONObject().put("version", 1).put("hosts", arr).toString()
    }

    fun fromJson(s: String): List<Host> = runCatching {
        val arr = JSONObject(s).optJSONArray("hosts") ?: return emptyList()
        (0 until arr.length()).mapNotNull { i ->
            val o = arr.optJSONObject(i) ?: return@mapNotNull null
            Host(o.optString("label"), o.optString("address"), o.optString("connect").ifEmpty { null })
        }
    }.getOrDefault(emptyList())
}

/** A host a code offers, and whether its address is on a tailnet. */
data class Offered(val host: Host, val inTailnet: Boolean)

/**
 * The code the Mac shows (Hive menu, Share Hosts with Phone):
 * hive://hosts?d=BASE64URL({"v":1,"hosts":[{"label","address"}]}). Labels
 * and addresses only, never secrets. Decoding treats it as untrusted.
 */
object HostsCode {
    const val PREFIX = "hive://hosts?d="
    private const val MAX_LENGTH = 8192
    private const val MAX_HOSTS = 64

    fun encode(hs: List<Host>): String {
        val arr = JSONArray()
        for (h in hs) arr.put(JSONObject().put("label", h.label).put("address", h.address))
        val json = JSONObject().put("v", 1).put("hosts", arr).toString()
        return PREFIX + Base64.getUrlEncoder().withoutPadding().encodeToString(json.toByteArray())
    }

    /** The hosts a code offers, all on demand; invalid ones left out. null: not a hosts code. */
    fun decode(s: String): List<Offered>? {
        val t = s.trim()
        if (!t.startsWith(PREFIX) || t.length > MAX_LENGTH) return null
        val json = runCatching { String(Base64.getUrlDecoder().decode(t.removePrefix(PREFIX))) }.getOrNull() ?: return null
        val arr = runCatching { JSONObject(json).getJSONArray("hosts") }.getOrNull() ?: return null
        val out = ArrayList<Offered>()
        for (i in 0 until minOf(arr.length(), MAX_HOSTS)) {
            val o = arr.optJSONObject(i) ?: continue
            val label = o.optString("label")
            val address = o.optString("address")
            if (Hosts.checkLabel(label) != null || Hosts.checkAddress(address) != null) continue
            if (out.any { it.host.label.equals(label, ignoreCase = true) }) continue
            out += Offered(Host(label, address), inTailnet(address))
        }
        return out
    }

    /** Whether an address names a tailnet machine: MagicDNS (*.ts.net or a bare name), 100.64/10, fd7a:115c:a1e0::/48. */
    fun inTailnet(address: String): Boolean {
        val host = Endpoint.candidates(address).firstOrNull()?.host?.lowercase() ?: return false
        if (host.endsWith(".ts.net")) return true
        if (host.contains(':')) return host.startsWith("fd7a:115c:a1e0:")
        val parts = host.split('.')
        if (parts.size == 4 && parts.all { p -> p.toIntOrNull()?.let { it in 0..255 } == true }) {
            val a = parts[0].toInt()
            val b = parts[1].toInt()
            return a == 100 && b in 64..127
        }
        return parts.size == 1 && parts[0].toIntOrNull() == null
    }
}
