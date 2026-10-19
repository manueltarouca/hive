package xyz.matm.hive.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicText
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import xyz.matm.hive.model.AppModel
import xyz.matm.hive.model.Host
import xyz.matm.hive.model.HostConnection
import xyz.matm.hive.model.Link
import xyz.matm.hive.model.SessionInfo

/** What a session's agents are up to, in the Mac's words: "1 blocked and 1 done", "7 agents". */
fun agentSummary(h: HostConnection, s: SessionInfo): Pair<String?, String?> {
    val agents = h.agents.filter { it.sessionID == s.id }
    val blocked = agents.count { it.state == "blocked" }
    val done = agents.count { it.state == "done" }
    val parts = listOfNotNull(blocked.takeIf { it > 0 }?.let { "$it blocked" }, done.takeIf { it > 0 }?.let { "$it done" })
    val state = when {
        blocked > 0 -> "blocked"
        agents.any { it.state == "working" } -> "working"
        done > 0 -> "done"
        else -> null
    }
    if (parts.isNotEmpty()) return state to parts.joinToString(" and ")
    val n = maxOf(agents.size, s.agents)
    return state to (if (n == 0) null else if (n == 1) "1 agent" else "$n agents")
}

/** The Mac's ⌘K list as a full screen: one section per host, the same words, the same check. */
@Composable
fun Switcher(model: AppModel, close: () -> Unit, addHost: () -> Unit, settings: () -> Unit, hosts: () -> Unit) {
    val p = LocalPalette.current
    var query by remember { mutableStateOf("") }
    var menuFor by remember { mutableStateOf<HostConnection?>(null) }
    val focus = remember { FocusRequester() }
    val q = query.trim()

    LaunchedEffect(Unit) { model.hosts.filter { it.connected }.forEach { model.refresh(it) } }

    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding().imePadding()) {
        Row(Modifier.fillMaxWidth().padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.clip(RoundedCornerShape(50)).clickable(role = Role.Button, onClick = close).semantics { contentDescription = "Back" }.padding(8.dp)) {
                LineIcon(Icon.Back, p.muted, 20.dp)
            }
            Spacer(Modifier.width(4.dp))
            Box(Modifier.weight(1f).clip(RoundedCornerShape(12.dp)).background(p.surface).padding(horizontal = 14.dp, vertical = 12.dp)) {
                if (query.isEmpty()) BasicText("Filter or create", style = Type.body.copy(color = p.muted))
                BasicTextField(
                    query, { query = it }, singleLine = true, textStyle = Type.body.copy(color = p.text), cursorBrush = SolidColor(p.honey),
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Go),
                    keyboardActions = KeyboardActions(onGo = {
                        val h = model.current ?: model.hosts.firstOrNull { it.connected }
                        val match = model.hosts.flatMap { hc -> hc.sessions.filter { it.label.contains(q, true) }.map { hc to it } }.firstOrNull()
                        when {
                            match != null -> { model.select(match.first, match.second.id); close() }
                            h != null && q.isNotEmpty() -> { model.newSession(h, q); close() }
                        }
                    }),
                    modifier = Modifier.fillMaxWidth().focusRequester(focus).semantics { contentDescription = "Filter or create" },
                )
            }
        }

        LazyColumn(Modifier.weight(1f).fillMaxWidth().padding(horizontal = 12.dp)) {
            for (h in model.hosts) {
                val sessions = h.sessions.filter { q.isEmpty() || it.label.contains(q, true) }
                item(key = "host:" + h.id) {
                    Row(
                        Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp))
                            .combinedClickable(onClick = {}, onLongClick = { menuFor = h }, onLongClickLabel = "Host options")
                            .padding(horizontal = 8.dp, vertical = 10.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        LineIcon(Icon.Globe, p.muted, 15.dp)
                        Spacer(Modifier.width(10.dp))
                        FadeText(h.name, Type.small, p.muted, Modifier.weight(1f))
                        if (h.host.always) BasicText("Stays connected", style = Type.tiny.copy(color = p.muted))
                    }
                }
                if (menuFor === h) item(key = "menu:" + h.id) { HostMenu(model, h) { menuFor = null } }
                when {
                    h.link == Link.Idle && !h.connected -> item(key = "connect:" + h.id) {
                        Row(rowModifier(false) { model.connect(h) }.semantics { contentDescription = "Connect to ${h.name}" }, verticalAlignment = Alignment.CenterVertically) {
                            LineIcon(Icon.Link, p.muted, 16.dp)
                            Spacer(Modifier.width(14.dp))
                            BasicText("Connect", style = Type.body.copy(color = p.text))
                        }
                    }
                    !h.connected -> item(key = "problem:" + h.id) {
                        Row(rowModifier(false) { h.retryNow() }, verticalAlignment = Alignment.CenterVertically) {
                            LineIcon(Icon.Alert, p.coral, 16.dp)
                            Spacer(Modifier.width(14.dp))
                            Column {
                                BasicText(h.shortProblem ?: "", style = Type.body.copy(color = p.text))
                                h.detail?.let { BasicText(it, style = Type.tiny.copy(color = p.muted)) }
                            }
                        }
                    }
                    else -> {
                        for (s in sessions) item(key = h.id + s.id) {
                            val current = model.current === h && model.currentSession == s.id
                            val (state, summary) = agentSummary(h, s)
                            Row(rowModifier(current) { model.select(h, s.id); close() }, verticalAlignment = Alignment.CenterVertically) {
                                Box(Modifier.width(26.dp)) { if (current) LineIcon(Icon.Check, p.honey, 16.dp) }
                                FadeText(s.label.ifEmpty { "session" }, Type.body, p.text, Modifier.weight(1f))
                                if (summary != null) {
                                    Spacer(Modifier.width(8.dp))
                                    if (state != null) { Hexagon(state, 9.dp); Spacer(Modifier.width(6.dp)) }
                                    BasicText(summary, style = Type.tiny.copy(color = p.muted))
                                }
                            }
                        }
                        item(key = "new:" + h.id) {
                            Row(rowModifier(false) { model.newSession(h, q.ifEmpty { null }); close() }, verticalAlignment = Alignment.CenterVertically) {
                                Box(Modifier.width(26.dp)) { LineIcon(Icon.Plus, p.muted, 15.dp) }
                                BasicText(if (q.isEmpty()) "New session on ${h.name}" else "New session “$q” on ${h.name}", style = Type.body.copy(color = p.text))
                            }
                        }
                    }
                }
                item(key = "gap:" + h.id) { Spacer(Modifier.height(8.dp)) }
            }
            item(key = "add") {
                Box(Modifier.fillMaxWidth().padding(vertical = 8.dp).height(1.dp).background(p.line))
                Row(rowModifier(false, onClick = addHost), verticalAlignment = Alignment.CenterVertically) {
                    Box(Modifier.width(26.dp)) { LineIcon(Icon.Globe, p.muted, 15.dp) }
                    BasicText("Add remote host", style = Type.body.copy(color = p.text))
                }
                Row(rowModifier(false, onClick = hosts), verticalAlignment = Alignment.CenterVertically) {
                    Box(Modifier.width(26.dp)) { LineIcon(Icon.Laptop, p.muted, 16.dp) }
                    BasicText("Hosts", style = Type.body.copy(color = p.text))
                }
                Row(rowModifier(false, onClick = settings), verticalAlignment = Alignment.CenterVertically) {
                    Box(Modifier.width(26.dp)) { LineIcon(Icon.Gear, p.muted, 16.dp) }
                    BasicText("Settings", style = Type.body.copy(color = p.text))
                }
            }
        }
    }
}

@Composable
private fun rowModifier(current: Boolean, onClick: () -> Unit): Modifier {
    val p = LocalPalette.current
    return Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp))
        .background(if (current) p.honey.copy(alpha = 0.14f) else androidx.compose.ui.graphics.Color.Transparent)
        .clickable(role = Role.Button, onClick = onClick).padding(horizontal = 8.dp, vertical = 12.dp)
}

/** A host's choices, as the Mac's host menu: connect on demand or stay connected, forget. */
@Composable
private fun HostMenu(model: AppModel, h: HostConnection, dismiss: () -> Unit) {
    val p = LocalPalette.current
    Column(Modifier.fillMaxWidth().padding(start = 26.dp, bottom = 6.dp).clip(RoundedCornerShape(12.dp)).background(p.surface).padding(4.dp)) {
        val always = h.host.always
        Row(rowModifier(false) { model.saveHost(h.host.copy(connect = if (always) Host.ON_DEMAND else Host.ALWAYS)); dismiss() }) {
            BasicText(if (always) "Connect on demand" else "Stay connected", style = Type.body.copy(color = p.text))
        }
        if (h.link != Link.Idle) Row(rowModifier(false) { model.disconnect(h); dismiss() }) { BasicText("Disconnect", style = Type.body.copy(color = p.text)) }
        Row(rowModifier(false) { model.removeHost(h); dismiss() }) { BasicText("Forget ${h.name}", style = Type.body.copy(color = p.coral)) }
    }
}
