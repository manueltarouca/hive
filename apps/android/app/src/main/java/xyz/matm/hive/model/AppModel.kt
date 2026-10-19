package xyz.matm.hive.model

import android.content.Context
import android.os.Handler
import android.os.Looper
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import org.json.JSONArray
import org.json.JSONObject
import xyz.matm.hive.agents.Agent
import xyz.matm.hive.agents.Harness
import xyz.matm.hive.agents.Notices
import xyz.matm.hive.notify.Notifier
import xyz.matm.hive.vt.Frame
import java.io.File

/** A window's panes, in layout order. */
data class WindowInfo(val id: String, val label: String, val blocks: List<String>, val focused: String?)

/** A pane, from session.view. */
data class BlockInfo(
    val id: String,
    val title: String,
    val cwd: String,
    /** The program in front, as the server names it (zsh, node, claude). */
    val foreground: String,
    val harness: String?,
    val agentState: String?,
    val label: String = "",
) {
    /** The agent if one reports from here, else the program in front (Programs.shown, as on the Mac). */
    val program: xyz.matm.hive.agents.Program get() = xyz.matm.hive.agents.Programs.shown(harness, foreground.ifEmpty { null })

    /** "Claude Code" for an agent's pane, else its program. */
    val name: String get() = program.name

    /** An agent's task, from the title it sets. */
    val task: String? get() = harness?.let { xyz.matm.hive.agents.Programs.taskName(title, it) }

    /** Header and tab title, as the Mac's: the label, an agent's task, or the program; then where. */
    val titleParts: Pair<String, String>
        get() = when {
            label.isNotEmpty() -> label to shortDir
            task != null -> task!! to shortDir.substringAfterLast('/')
            else -> name to shortDir
        }

    val shortDir: String get() = xyz.matm.hive.ui.shortPath(cwd)
}

/**
 * Everything the app shows: saved hosts and their connections, the session
 * on screen, its tabs and panes, and the pane's stream. Main thread only.
 */
class AppModel(private val context: Context) : HostConnection.Listener {
    private val main = Handler(Looper.getMainLooper())
    private val prefs = context.getSharedPreferences("hive", Context.MODE_PRIVATE)
    private val hostsFile = File(context.filesDir, "hosts.json")

    var hosts by mutableStateOf<List<HostConnection>>(emptyList())
        private set
    var current by mutableStateOf<HostConnection?>(null)
        private set
    var currentSession by mutableStateOf<String?>(null)
        private set
    var sessionLabel by mutableStateOf("")
        private set
    var windows by mutableStateOf<List<WindowInfo>>(emptyList())
        private set
    var blocks by mutableStateOf<Map<String, BlockInfo>>(emptyMap())
        private set
    /** The tab and pane on screen: the phone's own choice, not the session's focus (the Mac keeps its own). */
    var windowIndex by mutableStateOf(0)
        private set
    var paneIndex by mutableStateOf(0)
        private set
    var frame by mutableStateOf<Frame?>(null)
        private set
    /** The pane takes the phone's size (the Mac letterboxes) until the user stops, or the Mac takes it back. */
    var fitted by mutableStateOf(false)
        private set
    private var fitSize: IntArray? = null
    /** Compose state: the key bar, the size chip and the terminal follow it. */
    var stream by mutableStateOf<PaneStream?>(null)
        private set
    // MARK: settings (Settings screen), kept on the phone.

    /** system, night or day. */
    var themeChoice by mutableStateOf(prefs.getString("theme", "system") ?: "system")
        private set
    /** The terminal's text size when fitted to the phone, in sp. */
    var textSp by mutableStateOf(prefs.getFloat("textSp", 11.5f))
        private set
    /** As the Mac's notify: background, always or never. */
    var notify by mutableStateOf(prefs.getString("notify", "background") ?: "background")
        private set
    var confirmPaste by mutableStateOf(prefs.getBoolean("confirmPaste", true))
        private set
    /** Panes open fitted to the phone instead of at the Mac's size. */
    var openFitted by mutableStateOf(prefs.getBoolean("openFitted", false))
        private set

    fun chooseTheme(v: String) { themeChoice = v; prefs.edit().putString("theme", v).apply() }
    fun chooseTextSize(sp: Float) { textSp = sp.coerceIn(9f, 18f); prefs.edit().putFloat("textSp", textSp).apply() }
    fun chooseNotify(v: String) { notify = v; prefs.edit().putString("notify", v).apply() }
    fun chooseConfirmPaste(v: Boolean) { confirmPaste = v; prefs.edit().putBoolean("confirmPaste", v).apply() }
    fun chooseOpenFitted(v: Boolean) { openFitted = v; prefs.edit().putBoolean("openFitted", v).apply() }

    /** The terminal's colours follow the system's Night or Day, as the Mac's follow its appearance. */
    var termTheme by mutableStateOf(
        if (context.resources.configuration.uiMode and android.content.res.Configuration.UI_MODE_NIGHT_MASK == android.content.res.Configuration.UI_MODE_NIGHT_NO)
            xyz.matm.hive.vt.TermTheme.day else xyz.matm.hive.vt.TermTheme.night,
    )
        private set

    fun setDark(dark: Boolean) {
        val t = if (dark) xyz.matm.hive.vt.TermTheme.night else xyz.matm.hive.vt.TermTheme.day
        if (t === termTheme) return
        termTheme = t
        stream?.setTheme(t)
    }

    /** Whether the app is on screen: notifications are for when it isn't, as on the Mac. */
    var foreground = false
    var onNotify: ((HostConnection, Agent) -> Unit)? = null

    init {
        val saved = runCatching { Hosts.fromJson(hostsFile.readText()) }.getOrDefault(emptyList())
        hosts = saved.map { HostConnection(it, main::post, this) }
    }

    /** Connects the hosts saved as always, and the last session's host; opens that session. */
    fun launch() {
        val lastHost = prefs.getString("lastSessionHost", null)
        val last = prefs.getString("lastSession", null)
        for (h in hosts) if (h.host.always || h.id == lastHost) h.start()
        val h = hosts.firstOrNull { it.id == lastHost }
        if (h != null && last != null) { current = h; currentSession = last }
    }

    // MARK: hosts

    fun saveHost(host: Host): HostConnection {
        val list = Hosts.add(hosts.map { it.host }, host)
        persist(list)
        val saved = list.first { it.label.equals(host.label, true) }
        val existing = hosts.firstOrNull { it.host.label.equals(host.label, true) }
        if (existing != null) {
            val moved = existing.host.address != saved.address
            existing.host = saved
            if (moved) { existing.stop(); existing.start() }
            // Stay connected means now, not at the next launch.
            else if (saved.always) existing.start()
            hosts = hosts.toList()
            return existing
        }
        val h = HostConnection(saved, main::post, this)
        hosts = hosts + h
        if (saved.always) h.start()
        return h
    }

    fun removeHost(h: HostConnection) {
        h.stop()
        if (current === h) { detach(); current = null; currentSession = null; windows = emptyList() }
        hosts = hosts.filterNot { it === h }
        persist(hosts.map { it.host })
    }

    private fun persist(list: List<Host>) {
        val tmp = File(hostsFile.parentFile, "hosts.json.tmp")
        tmp.writeText(Hosts.toJson(list))
        tmp.renameTo(hostsFile)
    }

    /**
     * Add Host: tries the address as the CLI does (a bare host at https:443,
     * then https and http on 4483), saves the URL that answered under a new
     * label, and opens its first session. [done] gets null, or the problem
     * as a title and its particulars.
     */
    fun addHost(address: String, done: (Pair<String, String>?) -> Unit) {
        val a = address.trim()
        val candidates = xyz.matm.hive.net.Endpoint.candidates(a)
        if (candidates.isEmpty()) return done("Give a URL (https://box.example.ts.net) or a host name" to "")
        var refused: String? = null
        fun attempt(i: Int) {
            if (i >= candidates.size) {
                val host = candidates[0].host
                return done((refused ?: "No hive server answers at $host") to Hosts.tried(candidates.map { it.url }))
            }
            val c = xyz.matm.hive.net.HiveClient(candidates[i], main::post)
            c.connect("hive for Android", HostConnection.VERSION) { r ->
                c.close()
                r.onSuccess {
                    val label = Hosts.newLabel(a, hosts.map { it.host.label })
                    val h = saveHost(Host(label, candidates[i].url))
                    h.start()
                    whenConnected(h) { reloadSessions(h); h.call("session.list") { l ->
                        val first = l.getOrNull()?.optJSONArray("sessions")?.optJSONObject(0)?.optString("session_id")
                        if (first != null) select(h, first) else newSession(h)
                    } }
                    done(null)
                }.onFailure { e ->
                    if (e is xyz.matm.hive.net.HandshakeRefused && e.code == 403) refused = "${candidates[i].hostHeader} refused this phone"
                    attempt(i + 1)
                }
            }
        }
        attempt(0)
    }

    /** Saves the hosts a code offered, all on demand. */
    fun addOffered(list: List<Host>) {
        for (h in list) {
            // A new address from a code is never "always": it would connect there at every launch.
            val old = hosts.firstOrNull { it.host.label.equals(h.label, true) }?.host
            saveHost(Host(h.label, h.address, if (old != null && old.address != h.address) Host.ON_DEMAND else null))
        }
    }

    private fun whenConnected(h: HostConnection, tries: Int = 0, then: () -> Unit) {
        if (h.connected) then() else if (tries < 75) main.postDelayed({ whenConnected(h, tries + 1, then) }, 200)
    }

    /** Connects an on-demand host (Connect in the switcher). */
    fun connect(h: HostConnection) = h.start()

    // MARK: sessions

    /** block: the pane to show (from a notification), else the session's own focus. */
    fun select(h: HostConnection, session: String, block: String? = null) {
        pendingBlock = block
        if (current !== h || currentSession != session) {
            detach()
            windows = emptyList(); blocks = emptyMap(); sessionLabel = ""; frame = null
            windowIndex = 0; paneIndex = 0
        }
        current = h
        currentSession = session
        prefs.edit().putString("lastSession", session).putString("lastSessionHost", h.id).apply()
        h.start()
        if (h.connected) reloadView(focusServer = true)
    }

    /** Re-reads a host's sessions and agents (the switcher opening). */
    fun refresh(h: HostConnection) = reloadSessions(h)

    fun newSession(h: HostConnection, label: String? = null) {
        h.start()
        val params = JSONObject()
        if (!label.isNullOrBlank()) params.put("label", label)
        h.call("session.create", params) { r ->
            r.onSuccess { j -> j.optString("session_id").ifEmpty { null }?.let { select(h, it) }; reloadSessions(h) }
        }
    }

    /** A new tab in the pane's directory; it comes up on screen. */
    fun newTab() {
        val h = current ?: return
        val sid = currentSession ?: return
        val layout = JSONObject()
        visibleBlock?.let { blocks[it] }?.cwd?.takeIf { it.startsWith("/") }?.let { layout.put("cwd", it) }
        h.call("session.new_window", JSONObject().put("session_id", sid).put("layout", layout)) { r ->
            r.onSuccess { j -> pendingWindow = j.optString("window_id").ifEmpty { null }; scheduleReload() }
        }
    }

    private var pendingWindow: String? = null
    private var pendingBlock: String? = null

    fun selectWindow(i: Int) {
        if (i !in windows.indices || i == windowIndex) return
        windowIndex = i
        val w = windows[i]
        paneIndex = maxOf(0, w.blocks.indexOf(w.focused))
        attachVisible()
    }

    fun selectPane(i: Int) {
        val w = windows.getOrNull(windowIndex) ?: return
        if (i !in w.blocks.indices || i == paneIndex) return
        paneIndex = i
        attachVisible()
    }

    /** A tab's program: its agent if one of its panes has one, else its focused pane's (the Mac's program(of:)). */
    fun program(w: WindowInfo): xyz.matm.hive.agents.Program {
        val bs = w.blocks.mapNotNull { blocks[it] }
        return (bs.firstOrNull { it.harness != null } ?: blocks[w.focused ?: ""] ?: bs.firstOrNull())?.program
            ?: xyz.matm.hive.agents.Programs.lookup("")
    }

    /** The programs of a tab's other panes, for the cells behind its icon: up to two. */
    fun otherPrograms(w: WindowInfo): List<xyz.matm.hive.agents.Program> {
        val main = program(w)
        val seen = ArrayList<xyz.matm.hive.agents.Program>()
        for (id in w.blocks) { val p = blocks[id]?.program ?: continue; if (p != main && p !in seen) seen += p }
        while (seen.size < minOf(w.blocks.size - 1, 2)) seen += main
        return seen.take(2)
    }

    /** A tab's title: its label, else its focused pane's. */
    fun titleParts(w: WindowInfo): Pair<String, String> =
        if (w.label.isNotEmpty()) w.label to "" else (blocks[w.focused ?: ""] ?: w.blocks.firstNotNullOfOrNull { blocks[it] })?.titleParts ?: ("shell" to "")

    val visibleBlock: String? get() = windows.getOrNull(windowIndex)?.blocks?.getOrNull(paneIndex)

    private fun reloadSessions(h: HostConnection) {
        h.call("session.list") { r ->
            r.onSuccess { j ->
                val arr = j.optJSONArray("sessions") ?: JSONArray()
                h.sessions = (0 until arr.length()).map { i ->
                    val s = arr.getJSONObject(i)
                    SessionInfo(s.optString("session_id"), h.id, s.optString("label"), s.optInt("blocks"), s.optInt("agents"))
                }
                if (current === h && currentSession != null && h.sessions.none { it.id == currentSession }) {
                    // The session closed: another on this host, if any.
                    h.sessions.firstOrNull()?.let { select(h, it.id) } ?: run { detach(); currentSession = null; windows = emptyList() }
                }
            }
        }
        reloadAgents(h)
    }

    private fun reloadAgents(h: HostConnection) {
        h.call("agent.list") { r ->
            r.onSuccess { j ->
                val arr = j.optJSONArray("agents") ?: JSONArray()
                val list = (0 until arr.length()).mapNotNull { i ->
                    val a = arr.getJSONObject(i)
                    val agent = a.optJSONObject("agent") ?: JSONObject()
                    val state = agent.optString("state")
                    if (state.isEmpty() || state == "ended") return@mapNotNull null
                    Agent(
                        blockID = a.optString("block_id"), sessionID = a.optString("session_id"), host = h.id,
                        sessionLabel = a.optString("session_label"), harness = agent.optString("harness"),
                        state = state, message = agent.optString("message"), title = a.optString("title"),
                    )
                }
                val news = Notices.changes(h.lastAgentStates, list)
                h.lastAgentStates = list.associate { it.blockID to it.state }
                h.agents = list
                if (current === h) {
                    val byBlock = list.associateBy { it.blockID }
                    blocks = blocks.mapValues { (id, b) ->
                        val a = byBlock[id]
                        // Off the list: the agent ended, or its shell is back.
                        b.copy(harness = a?.harness, agentState = a?.state)
                    }
                }
                // As the Mac's notify setting: in the background (the default), always, or never.
                val now = when (notify) { "never" -> false; "always" -> true; else -> !foreground }
                if (now) news.forEach { onNotify?.invoke(h, it) }
            }
        }
    }

    private var reloadPending = false
    private fun scheduleReload() {
        if (reloadPending) return
        reloadPending = true
        main.postDelayed({ reloadPending = false; reloadView() }, 30)
    }

    private val agentReloadPending = HashSet<String>()
    private fun scheduleAgentReload(h: HostConnection) {
        if (!agentReloadPending.add(h.id)) return
        main.postDelayed({ agentReloadPending.remove(h.id); reloadAgents(h) }, 250)
    }

    /** focusServer: start on the session's own active tab and focused pane. */
    private fun reloadView(focusServer: Boolean = false) {
        val h = current ?: return
        val sid = currentSession ?: return
        h.call("session.view", JSONObject().put("session_id", sid)) { r ->
            if (current !== h || currentSession != sid) return@call
            r.onFailure { if ((it as? xyz.matm.hive.net.HiveError)?.code == "not_found") reloadSessions(h) }
            r.onSuccess { j ->
                sessionLabel = j.optString("label")
                val active = j.optString("active_window_id")
                val ws = j.optJSONArray("windows") ?: JSONArray()
                val newWindows = (0 until ws.length()).map { i ->
                    val w = ws.getJSONObject(i)
                    WindowInfo(w.optString("window_id"), w.optString("label"), layoutBlocks(w.optJSONObject("layout")), w.optString("focused_block_id").ifEmpty { null })
                }
                val bj = j.optJSONObject("blocks") ?: JSONObject()
                val agentsByBlock = h.agents.associateBy { it.blockID }
                val newBlocks = HashMap<String, BlockInfo>()
                for (id in bj.keys()) {
                    val b = bj.getJSONObject(id)
                    val fg = b.optJSONObject("foreground") ?: JSONObject()
                    val agent = b.optJSONObject("agent")
                    var cwd = fg.optString("cwd")
                    val pwd = b.optString("pwd")
                    if (pwd.startsWith("file://")) cwd = android.net.Uri.parse(pwd).path ?: cwd
                    if (cwd.isEmpty()) cwd = agent?.optString("cwd").orEmpty().ifEmpty { b.optString("cwd") }
                    val st = agent?.optString("state").orEmpty()
                    val live = agentsByBlock[id]
                    val harness = live?.harness ?: agent?.optString("harness")?.takeIf { it.isNotEmpty() && st != "ended" }
                    newBlocks[id] = BlockInfo(id, b.optString("title"), cwd, fg.optString("program").ifEmpty { fg.optString("name") },
                        harness, live?.state ?: st.takeIf { it.isNotEmpty() && it != "ended" }, b.optString("label"))
                }
                val keepWindow = windows.getOrNull(windowIndex)?.id
                val keepBlock = visibleBlock
                windows = newWindows
                blocks = newBlocks
                val wi = if (focusServer || keepWindow == null) newWindows.indexOfFirst { it.id == active }
                else newWindows.indexOfFirst { it.id == keepWindow }.takeIf { it >= 0 } ?: newWindows.indexOfFirst { it.id == active }
                val pending = (pendingWindow?.let { id -> newWindows.indexOfFirst { it.id == id } }
                    ?: pendingBlock?.let { b -> newWindows.indexOfFirst { b in it.blocks } })?.takeIf { it >= 0 }
                if (pending != null) pendingWindow = null
                val showBlock = pendingBlock?.takeIf { pending != null }
                pendingBlock = null
                windowIndex = maxOf(0, pending ?: wi)
                val w = newWindows.getOrNull(windowIndex)
                paneIndex = when {
                    w == null -> 0
                    showBlock != null && showBlock in w.blocks -> w.blocks.indexOf(showBlock)
                    pending == null && !focusServer && keepBlock != null && keepBlock in w.blocks -> w.blocks.indexOf(keepBlock)
                    else -> maxOf(0, w.blocks.indexOf(w.focused))
                }
                attachVisible()
            }
        }
    }

    private fun layoutBlocks(l: JSONObject?): List<String> = when (l?.optString("type")) {
        "block" -> listOf(l.optString("block_id"))
        "split" -> layoutBlocks(l.optJSONObject("a")) + layoutBlocks(l.optJSONObject("b"))
        else -> emptyList()
    }

    /** Subscribes to the pane on screen, and only it. */
    private fun attachVisible() {
        val h = current
        val c = h?.client
        val b = visibleBlock
        if (stream?.blockID == b && stream?.client === c && c != null) return
        // Leaving a fitted pane gives it back its size; the next one starts at the Mac's.
        if (fitted) unfit()
        detach()
        if (c == null || b == null || !h.connected) return
        val s = PaneStream(c, b, termTheme) { f -> if (stream?.blockID == b) onFrame(f) }
        stream = s
        s.start()
    }

    private fun detach() {
        stream?.stop()
        stream = null
        frame = null
    }

    /** Panes fitted on opening (Settings), once each: Mac size afterwards sticks. */
    private val autoFitted = HashSet<String>()
    private var lastPhone: IntArray? = null

    private fun onFrame(f: Frame) {
        // Open fitted: on the pane's first frame, so its own size is known and Mac size can give it back.
        val s = stream
        val ph = lastPhone
        if (openFitted && !fitted && s != null && s.cols > 0 && ph != null && autoFitted.add(s.blockID)) fit(ph[0], ph[1], ph[2], ph[3])
        // A size the phone didn't ask for, after one it did: the Mac took it back. (The phone's own
        // size changes as the keyboard or the agent's card comes and goes: those are asked for too.)
        val mine = { c: Int, r: Int -> asked.any { it.first == c && it.second == r } }
        if (fitted && !mine(f.cols, f.rows) && frame?.let { mine(it.cols, it.rows) } == true) fitted = false
        frame = f
    }

    // MARK: size

    /** The size hint goes once the user has fitted or pinched a pane. */
    var sizeHintSeen by mutableStateOf(prefs.getBoolean("sizeHintSeen", false))
        private set

    fun sizeLearned() {
        if (sizeHintSeen) return
        sizeHintSeen = true
        prefs.edit().putBoolean("sizeHintSeen", true).apply()
    }

    /** Fit to phone: the pane takes this size (take: the Mac letterboxes). */
    fun fit(cols: Int, rows: Int, cellWidth: Int, cellHeight: Int) {
        val s = stream ?: return
        // The size to give back with Mac size: the pane's own, before the phone took it.
        if (!fitted && s.cols > 0) {
            macSize = Triple(s.blockID, intArrayOf(s.cols, s.rows), intArrayOf(s.cellWidth.takeIf { it > 0 } ?: cellWidth, s.cellHeight.takeIf { it > 0 } ?: cellHeight))
        }
        fitSize = intArrayOf(cols, rows, cellWidth, cellHeight)
        asked.addLast(cols to rows)
        while (asked.size > 6) asked.removeFirst()
        fitted = true
        sizeLearned()
        s.client.resize(s.blockID, cols, rows, cellWidth, cellHeight, take = true)
    }

    private var macSize: Triple<String, IntArray, IntArray>? = null
    /** The sizes the phone asked for lately. */
    private val asked = ArrayDeque<Pair<Int, Int>>()

    /** Back to the Mac's size, at once: the size the pane had before Fit to phone. */
    fun unfit() {
        fitted = false
        fitSize = null
        asked.clear()
        val s = stream
        val m = macSize
        macSize = null
        if (s != null && m != null && m.first == s.blockID) s.client.resize(s.blockID, m.second[0], m.second[1], m.third[0], m.third[1], take = true)
    }

    /** The phone's size changed (rotation, the keyboard): a fitted pane follows. */
    fun phoneSize(cols: Int, rows: Int, cellWidth: Int, cellHeight: Int) {
        lastPhone = intArrayOf(cols, rows, cellWidth, cellHeight)
        if (!fitted) return
        val f = fitSize
        if (f != null && f[0] == cols && f[1] == rows) return
        fit(cols, rows, cellWidth, cellHeight)
    }

    // MARK: answers

    /** Answers a blocked agent with a choice's key (Claude Code picks by number). */
    fun answer(key: String) = stream?.text(key)

    /** "Tell Claude what to do": leave the dialog, then send the words. */
    fun reply(text: String) {
        val s = stream ?: return
        s.key("escape")
        main.postDelayed({ s.text(text); main.postDelayed({ s.key("enter") }, 80) }, 250)
    }

    /**
     * From a notification: answers on [hostID]'s [blockID] if its agent is
     * still blocked, connecting first if needed. [key] is a choice's key,
     * "escape" to deny, or null with [text] for a reply.
     */
    fun answerRemotely(hostID: String, blockID: String, key: String?, text: String?, described: String, finished: (Boolean) -> Unit) {
        var answered = false
        val done = { ok: Boolean -> if (!answered) { answered = true; finished(ok) } }
        val h = hosts.firstOrNull { it.id == hostID } ?: return done(false)
        fun go() {
            h.call("agent.list") { r ->
                val arr = r.getOrNull()?.optJSONArray("agents") ?: return@call done(false)
                val now = (0 until arr.length()).map { arr.getJSONObject(it) }.firstOrNull { it.optString("block_id") == blockID }?.let { a ->
                    val ag = a.optJSONObject("agent") ?: JSONObject()
                    Agent(blockID, a.optString("session_id"), h.id, a.optString("session_label"), ag.optString("harness"),
                        ag.optString("state"), ag.optString("message"), a.optString("title"))
                }
                // The same question the notification described, or nothing is sent.
                if (now == null || now.state != "blocked" || now.message != described) return@call done(false)
                h.call("hive.terminal.format", JSONObject().put("block_id", blockID).put("args", JSONObject().put("trim", true))) { t ->
                    val pane = t.getOrNull()?.optString("content")?.lines() ?: return@call done(false)
                    val c = h.client ?: return@call done(false)
                    when {
                        key == "escape" -> c.key(blockID, "escape")
                        key != null -> { if (!Notices.allowable(described, now, pane)) return@call done(false); c.input(blockID, key.toByteArray()) }
                        text != null -> {
                            c.key(blockID, "escape")
                            main.postDelayed({ c.input(blockID, text.toByteArray()); main.postDelayed({ c.key(blockID, "enter") }, 80) }, 250)
                        }
                    }
                    done(true)
                }
            }
        }
        if (h.connected) go() else {
            h.start()
            whenConnected(h, tries = 25) { go() } // about 10 s, then it says it couldn't
            main.postDelayed({ if (!h.connected) done(false) }, 10_500)
        }
    }

    // MARK: HostConnection.Listener

    override fun connected(h: HostConnection) {
        reloadSessions(h)
        if (current === h && currentSession != null) reloadView(focusServer = stream == null && windows.isEmpty())
    }

    override fun disconnected(h: HostConnection) {
        if (current === h) { stream?.stop(); stream = null }
    }

    override fun changed(h: HostConnection) { hosts = hosts.toList() }

    /** Events were lost (the server's backlog overflowed): read everything again. */
    override fun resynced(h: HostConnection) {
        reloadSessions(h)
        if (current === h) reloadView()
    }

    override fun event(h: HostConnection, ev: JSONObject) {
        val here = h === current
        when (ev.optString("type")) {
            "session_created", "session_destroyed" -> reloadSessions(h)
            "session_event" -> {
                val payload = ev.optJSONObject("payload") ?: return
                val sid = if (here) payload.optString("session_id") else null
                val inner = payload.optJSONObject("event") ?: return
                val type = inner.optString("type")
                if (type == "block_event") {
                    val name = inner.optJSONObject("payload")?.optString("name")
                    if (name == "agent_changed") reloadAgents(h)
                    if (name == "program_status_changed") { scheduleAgentReload(h); if (sid == currentSession) scheduleReload() }
                    if (name in setOf("title_changed", "size_changed", "process_changed", "child_exited", "pwd_changed") && sid == currentSession) scheduleReload()
                } else if (sid == currentSession) scheduleReload()
                if (type == "block_closed" || type == "block_created" || type == "session_view_changed") reloadSessions(h)
            }
        }
    }

    /** The app is back on screen: reconnect at once instead of waiting out the backoff. */
    fun resumed() {
        foreground = true
        for (h in hosts) { h.background = false; h.retryNow() }
    }

    /** Leaving: hosts that are away retry less often, to spare the battery. */
    fun paused() {
        foreground = false
        for (h in hosts) h.background = true
    }

    /** The agents of every host, blocked first. */
    val allAgents: List<Agent> get() = hosts.flatMap { it.agents }
}
