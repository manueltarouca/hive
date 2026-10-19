package xyz.matm.hive.model

import android.os.Handler
import android.os.Looper
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import org.json.JSONObject
import xyz.matm.hive.agents.Agent
import xyz.matm.hive.net.Endpoint
import xyz.matm.hive.net.HandshakeRefused
import xyz.matm.hive.net.HiveClient
import xyz.matm.hive.net.ReconnectPlan
import java.net.ConnectException
import java.net.SocketTimeoutException
import java.util.concurrent.Executor

/** A session on a host, from session.list. */
data class SessionInfo(val id: String, val host: String, val label: String, val blocks: Int, val agents: Int)

/** How a host's connection is, in the Mac app's words. */
sealed class Link {
    object Idle : Link()
    object Connecting : Link()
    object Connected : Link()
    object Reconnecting : Link()
    class Refused(val message: String) : Link()
    object NoServer : Link()
    object Silent : Link()
    class Failed(val message: String) : Link()
}

/**
 * One saved host: its connection, sessions and agents. Main thread only.
 * It connects when asked ([start]) and keeps trying, as the Mac does: the
 * candidates of a bare host in order, then backing off up to 10 s.
 */
class HostConnection(host: Host, private val main: Executor, private val listener: Listener) {
    /** Compose state: its name and choices show the moment they change. */
    var host by mutableStateOf(host)
    interface Listener {
        fun connected(h: HostConnection)
        fun disconnected(h: HostConnection)
        fun changed(h: HostConnection)
        fun event(h: HostConnection, ev: JSONObject)
        fun resynced(h: HostConnection)
    }

    private val handler = Handler(Looper.getMainLooper())
    val id get() = host.label.lowercase()
    val name get() = host.label

    var link by mutableStateOf<Link>(Link.Idle)
        private set
    var sessions by mutableStateOf<List<SessionInfo>>(emptyList())
    var agents by mutableStateOf<List<Agent>>(emptyList())
    /** Each agent's state at the last load, for notifications; null before the first. */
    var lastAgentStates: Map<String, String>? = null
    /** The server's machine (server.version), for naming apps on it: "Hive.app on studio". */
    var serverHostname = ""
    var everConnected = false
        private set
    var client: HiveClient? = null
        private set
    private var endpoints = Endpoint.candidates(host.address)
    private var plan = ReconnectPlan(endpoints.size)
    private var wanted = false
    private var retry: Runnable? = null
    private var lastEventSeq: Long? = null
    val endpoint: Endpoint? get() = endpoints.getOrNull(plan.index)

    val connected get() = link == Link.Connected

    /** Connects, unless already connected or trying. */
    fun start() {
        if (wanted) return
        wanted = true
        endpoints = Endpoint.candidates(host.address)
        plan = ReconnectPlan(endpoints.size)
        attempt()
    }

    /** Disconnects and stops trying. */
    fun stop() {
        wanted = false
        retry?.let(handler::removeCallbacks)
        retry = null
        client?.close()
        client = null
        link = Link.Idle
        listener.changed(this)
    }

    /** Tries again now, skipping the backoff (the app came back to the foreground). */
    fun retryNow() {
        if (!wanted || connected || client != null) return
        retry?.let(handler::removeCallbacks)
        attempt()
    }

    private fun attempt() {
        retry = null
        val ep = endpoint ?: run { link = Link.Failed("Not a server address: ${host.address}"); listener.changed(this); return }
        // Once it has been up, every try is a reconnect, whatever the last failure was.
        link = if (everConnected) Link.Reconnecting else Link.Connecting
        listener.changed(this)
        val c = HiveClient(ep, main)
        client = c
        c.onEvent = { ev ->
            ev.optLong("seq", -1).takeIf { it >= 0 }?.let { lastEventSeq = it }
            listener.event(this, ev)
        }
        c.onEventsResync = { lastEventSeq = null; listener.resynced(this) }
        c.connect(clientName, VERSION) { r ->
            if (client !== c) return@connect
            r.onSuccess {
                plan.connected()
                everConnected = true
                link = Link.Connected
                // Events after the last one seen, so nothing is missed across a reconnect.
                c.subscribeEvents(all = true, since = lastEventSeq)
                listener.connected(this)
                listener.changed(this)
            }.onFailure { e -> failed(e) }
        }
        c.onClose = { e ->
            if (client === c) {
                client = null
                val was = link == Link.Connected
                if (was) {
                    listener.disconnected(this)
                    if (wanted) { link = Link.Reconnecting; schedule(plan.lost()) }
                    listener.changed(this)
                }
            }
        }
    }

    private fun failed(e: Throwable) {
        client = null
        link = when {
            e is HandshakeRefused && e.code == 403 -> Link.Refused("${endpoint?.url} refused this phone: its Tailscale login may not use this server.")
            e is HandshakeRefused -> Link.Failed("The server answered ${e.status}")
            e is ConnectException -> Link.NoServer
            e is SocketTimeoutException -> Link.Silent
            else -> Link.Failed(e.message ?: e.javaClass.simpleName)
        }
        listener.changed(this)
        if (wanted) schedule(plan.failed())
    }

    /** In the background a host that's away is tried once a minute; back on screen, at once (retryNow). */
    var background = false

    private fun schedule(ms: Long) {
        val r = Runnable { if (wanted && client == null) attempt() }
        retry = r
        handler.postDelayed(r, if (background) maxOf(ms, 60_000) else ms)
    }

    fun call(method: String, params: JSONObject = JSONObject(), done: (Result<JSONObject>) -> Unit = {}) {
        val c = client
        if (c == null || !connected) { done(Result.failure(xyz.matm.hive.net.HiveError.closed)); return }
        c.call(method, params, done)
    }

    /** What's wrong, for the away card's title; null when fine. */
    val problem: String?
        get() = when (val l = link) {
            Link.Connected, Link.Idle -> null
            Link.Connecting -> "Connecting to $name…"
            is Link.Refused -> "$name refused this phone"
            Link.Reconnecting -> "Reconnecting to $name"
            else -> if (!everConnected) "Can't reach $name" else "Reconnecting to $name"
        }

    /** The same under the host's own header in the switcher. */
    val shortProblem: String?
        get() = when (link) {
            Link.Connected, Link.Idle -> null
            Link.Connecting -> "Connecting…"
            Link.Reconnecting -> "Reconnecting…"
            is Link.Refused -> "Refused this phone"
            Link.NoServer -> "No hive server running"
            Link.Silent -> "Not answering"
            is Link.Failed -> "Can't connect"
        }

    /** The cause in full. */
    val detail: String?
        get() {
            val at = endpoint?.url ?: host.address
            return when (val l = link) {
                is Link.Refused -> l.message
                is Link.Failed -> "${l.message} ($at). hive keeps trying."
                Link.NoServer -> "No hive server answers at $at. hive keeps trying."
                Link.Silent -> "$at doesn't answer. hive keeps trying."
                Link.Reconnecting -> "The phone lost $name. hive keeps trying; the session keeps running on the server."
                Link.Connecting -> "Reaching $at."
                else -> null
            }
        }

    companion object {
        /** As other clients see this phone: "hive on Pixel 8". */
        val clientName: String = "hive on " + (android.os.Build.MODEL?.takeIf { it.isNotBlank() } ?: "Android")
        const val VERSION = "0.1.0"
    }
}
