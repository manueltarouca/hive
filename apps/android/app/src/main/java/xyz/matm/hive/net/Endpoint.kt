package xyz.matm.hive.net

import java.net.URI

/** Where a hive server is on the network, as the Mac app's ServerEndpoint. */
data class Endpoint(val host: String, val port: Int, val tls: Boolean) {
    /** The Host header: the name the server answers to (Tailscale Serve matches on it). */
    val hostHeader: String
        get() {
            val h = if (host.contains(":")) "[$host]" else host
            return if ((tls && port == 443) || (!tls && port == 80)) h else "$h:$port"
        }

    val url: String get() = (if (tls) "https://" else "http://") + hostHeader

    override fun toString() = url

    companion object {
        /** hive's port for tailnet and plain listeners. */
        const val DEFAULT_PORT = 4483

        /** A server URL: https or http, a host, an optional port, no path. */
        fun parse(s: String): Endpoint? {
            val u = runCatching { URI(s) }.getOrNull() ?: return null
            val scheme = u.scheme?.lowercase() ?: return null
            if (scheme != "https" && scheme != "http") return null
            val host = u.host?.trim('[', ']') ?: return null
            if (host.isEmpty() || u.rawUserInfo != null || u.rawQuery != null || u.rawFragment != null) return null
            if ((u.rawPath ?: "").trim('/').isNotEmpty()) return null
            val tls = scheme == "https"
            val port = if (u.port == -1) (if (tls) 443 else 80) else u.port
            if (port !in 1..65535) return null
            return Endpoint(host, port, tls)
        }

        /**
         * Where a saved host is tried, in order, as the CLI and the Mac do: a
         * URL as it is; a bare host at https:443, then https and http on 4483;
         * HOST:PORT with https, then http.
         */
        fun candidates(address: String): List<Endpoint> {
            val a = address.trim()
            if (a.contains("://")) return listOfNotNull(parse(a))
            if (a.isEmpty() || a.contains('/') || a.contains('@') || a.any { it.isWhitespace() }) return emptyList()
            var host = a
            var port: Int? = null
            if (a.startsWith("[")) {
                val close = a.indexOf(']')
                if (close < 0) return emptyList()
                host = a.substring(1, close)
                val rest = a.substring(close + 1)
                if (rest.startsWith(":")) port = rest.drop(1).toIntOrNull() ?: return emptyList()
            } else if (a.count { it == ':' } == 1) {
                host = a.substringBefore(':')
                port = a.substringAfter(':').toIntOrNull() ?: return emptyList()
            }
            if (host.isEmpty()) return emptyList()
            if (port != null) {
                if (port !in 1..65535) return emptyList()
                return listOf(Endpoint(host, port, true), Endpoint(host, port, false))
            }
            return listOf(Endpoint(host, 443, true), Endpoint(host, DEFAULT_PORT, true), Endpoint(host, DEFAULT_PORT, false))
        }
    }
}

/**
 * When to try a server again, as the Mac's ReconnectPlan: candidates of one
 * round follow each other at once; after a round fails, or a connection is
 * lost, it backs off 0.5 s, 1 s, 2 s… up to 10 s.
 */
class ReconnectPlan(candidates: Int) {
    val candidates = maxOf(1, candidates)
    var index = 0
        private set
    var backoffMs = 500L
        private set

    /** An attempt failed: how long to wait before the next candidate. */
    fun failed(): Long {
        if (index + 1 < candidates) { index++; return 0 }
        index = 0
        return bump()
    }

    /** An established connection was lost: the same candidate, after the backoff. */
    fun lost(): Long = bump()

    fun connected() { backoffMs = 500 }

    private fun bump(): Long = backoffMs.also { backoffMs = minOf(backoffMs * 2, 10_000) }
}
