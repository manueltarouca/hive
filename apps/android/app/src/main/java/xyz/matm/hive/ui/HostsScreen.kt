package xyz.matm.hive.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicText
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
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
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import xyz.matm.hive.model.AppModel
import xyz.matm.hive.model.Host
import xyz.matm.hive.model.HostConnection
import xyz.matm.hive.model.Hosts
import xyz.matm.hive.model.Link

/**
 * Your hosts, each a card: where it is, how it is, and what you can do
 * with it (connect or disconnect, stay connected, rename, forget). The Mac's
 * host commands, in one place.
 */
@Composable
fun HostsScreen(model: AppModel, close: () -> Unit, addHost: () -> Unit, scan: () -> Unit) {
    val p = LocalPalette.current
    var renaming by remember { mutableStateOf<HostConnection?>(null) }
    var forgetting by remember { mutableStateOf<HostConnection?>(null) }
    Box(Modifier.fillMaxSize().background(p.bg)) {
        Column(Modifier.fillMaxSize().statusBarsPadding().navigationBarsPadding()) {
            Row(Modifier.fillMaxWidth().padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
                Box(Modifier.clip(RoundedCornerShape(50)).clickable(role = Role.Button, onClick = close).semantics { contentDescription = "Back" }.padding(8.dp)) {
                    LineIcon(Icon.Back, p.muted, 20.dp)
                }
                Spacer(Modifier.width(6.dp))
                BasicText("Hosts", style = Type.title.copy(color = p.text), modifier = Modifier.semantics { heading() })
            }
            Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = 16.dp)) {
                if (model.hosts.isEmpty()) {
                    BasicText("No hosts yet. Add one by its address, or bring your Mac's across with its code.",
                        style = Type.body.copy(color = p.muted), modifier = Modifier.padding(vertical = 16.dp))
                }
                for (h in model.hosts) {
                    HostCard(model, h, rename = { renaming = h }, forget = { forgetting = h })
                    Spacer(Modifier.height(12.dp))
                }
                Spacer(Modifier.height(4.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Pill("Add a host", primary = true, onClick = addHost)
                    Pill("Scan the code", onClick = scan)
                }
                BasicText("The code is on your Mac: Hive menu, Share Hosts with Phone.",
                    style = Type.tiny.copy(color = p.muted), modifier = Modifier.padding(top = 8.dp, bottom = 24.dp))
            }
        }
        renaming?.let { h -> RenameSheet(model, h) { renaming = null } }
        forgetting?.let { h ->
            Sheet(onDismiss = { forgetting = null }) {
                BasicText("Forget ${h.name}?", style = Type.title.copy(color = p.text))
                Spacer(Modifier.height(6.dp))
                BasicText("Its sessions keep running on the server. You can add it again.", style = Type.small.copy(color = p.muted))
                Spacer(Modifier.height(16.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Pill("Forget", primary = true, modifier = Modifier.semantics { contentDescription = "Forget ${h.name} now" }) {
                        model.removeHost(h); forgetting = null
                    }
                    Pill("Cancel") { forgetting = null }
                }
            }
        }
    }
}

/** A host's state in words, as the switcher and the Mac say it. */
private fun state(h: HostConnection): Pair<String, String> = when (h.link) {
    Link.Connected -> "working" to ("Connected" + (h.client?.serverVersion?.takeIf { it.isNotEmpty() }?.let { ", hive $it" } ?: ""))
    Link.Idle -> "idle" to if (h.host.always) "Not connected" else "Connects when you pick it"
    Link.Connecting -> "working" to "Connecting…"
    Link.Reconnecting -> "working" to "Reconnecting…"
    else -> "blocked" to (h.shortProblem ?: "Can't connect")
}

@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
private fun HostCard(model: AppModel, h: HostConnection, rename: () -> Unit, forget: () -> Unit) {
    val p = LocalPalette.current
    val (hex, words) = state(h)
    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(p.surface).border(1.dp, p.line, RoundedCornerShape(12.dp))
        .padding(14.dp).semantics(mergeDescendants = false) { contentDescription = "Host ${h.name}" }) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Hexagon(hex, 10.dp)
            Spacer(Modifier.width(10.dp))
            Column(Modifier.weight(1f)) {
                FadeText(h.name, Type.label.copy(fontSize = Type.body.fontSize), p.text)
                FadeText(words, Type.small, if (hex == "blocked") p.coral else p.muted)
            }
        }
        Spacer(Modifier.height(6.dp))
        FadeText(h.host.address, Type.mono.copy(fontFamily = LocalMono.current, fontSize = Type.tiny.fontSize), p.muted)
        h.detail?.let { BasicText(it, style = Type.tiny.copy(color = p.muted), modifier = Modifier.padding(top = 4.dp)) }
        Spacer(Modifier.height(12.dp))
        // Stay connected: a row of its own, since it decides the background.
        Row(Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp))
            .toggleable(value = h.host.always, role = Role.Switch) { model.saveHost(h.host.copy(connect = if (it) Host.ALWAYS else Host.ON_DEMAND)) }
            // Read with its help line: what the choice means.
            .semantics { contentDescription = "Stay connected to ${h.name}. " + if (h.host.always) "In the background too, so its agents can notify you." else "Only while you use hive." }
            .padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                BasicText("Stay connected", style = Type.body.copy(color = p.text))
                BasicText(if (h.host.always) "In the background too, so its agents can notify you." else "Only while you use hive: it lets go 30 seconds after you leave.",
                    style = Type.tiny.copy(color = p.muted))
            }
            Spacer(Modifier.width(10.dp))
            SwitchTrack(h.host.always)
        }
        Spacer(Modifier.height(10.dp))
        // Wraps at large text sizes rather than clipping.
        androidx.compose.foundation.layout.FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            if (h.link == Link.Idle) Pill("Connect", modifier = Modifier.semantics { contentDescription = "Connect to ${h.name}" }) { model.connect(h) }
            else Pill("Disconnect", modifier = Modifier.semantics { contentDescription = "Disconnect from ${h.name}" }) { model.disconnect(h) }
            Pill("Rename", modifier = Modifier.semantics { contentDescription = "Rename ${h.name}" }, onClick = rename)
            Pill("Forget", danger = true, modifier = Modifier.semantics { contentDescription = "Forget ${h.name}" }, onClick = forget)
        }
    }
}

@Composable
private fun RenameSheet(model: AppModel, h: HostConnection, done: () -> Unit) {
    val p = LocalPalette.current
    // The old name selected: typing replaces it.
    var field by remember { mutableStateOf(androidx.compose.ui.text.input.TextFieldValue(h.name, androidx.compose.ui.text.TextRange(0, h.name.length))) }
    val name = field.text
    val focus = remember { FocusRequester() }
    LaunchedEffect(Unit) { focus.requestFocus() }
    val taken = model.hosts.any { it !== h && it.name.equals(name.trim(), true) }
    val problem = Hosts.checkLabel(name.trim()) ?: if (taken) "Another host is called that" else null
    fun save() { if (problem == null) { model.renameHost(h, name.trim()); done() } }
    Sheet(onDismiss = done) {
        BasicText("Rename ${h.name}", style = Type.title.copy(color = p.text))
        Spacer(Modifier.height(12.dp))
        Box(Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(p.bg).padding(horizontal = 12.dp, vertical = 12.dp)) {
            BasicTextField(field, { field = it }, singleLine = true, textStyle = Type.body.copy(color = p.text), cursorBrush = SolidColor(p.honey),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done, autoCorrectEnabled = false),
                keyboardActions = KeyboardActions(onDone = { save() }),
                modifier = Modifier.fillMaxWidth().focusRequester(focus).semantics { contentDescription = "New name" })
        }
        BasicText(problem ?: "As the CLI and the Mac call it: hive -S ${name.trim().ifEmpty { "name" }} ls",
            style = Type.tiny.copy(color = if (problem != null && name.isNotBlank()) p.coral else p.muted), modifier = Modifier.padding(top = 6.dp))
        Spacer(Modifier.height(14.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Pill("Save", primary = problem == null, modifier = Modifier.semantics { contentDescription = "Save the name" }) { save() }
            Pill("Cancel", onClick = done)
        }
    }
}

/** A bottom sheet over a scrim: tap outside to dismiss. */
@Composable
fun Sheet(onDismiss: () -> Unit, content: @Composable () -> Unit) {
    val p = LocalPalette.current
    Box(Modifier.fillMaxSize().background(androidx.compose.ui.graphics.Color.Black.copy(alpha = 0.42f))
        .clickable(indication = null, interactionSource = remember { androidx.compose.foundation.interaction.MutableInteractionSource() }, onClick = onDismiss)) {
        Column(Modifier.align(Alignment.BottomCenter).fillMaxWidth().clip(RoundedCornerShape(topStart = 22.dp, topEnd = 22.dp))
            .background(p.surface).clickable(indication = null, interactionSource = remember { androidx.compose.foundation.interaction.MutableInteractionSource() }) {}
            .navigationBarsPadding().imePadding().padding(horizontal = 20.dp, vertical = 14.dp)) {
            Box(Modifier.align(Alignment.CenterHorizontally).size(width = 36.dp, height = 4.dp).clip(CircleShape).background(p.muted.copy(alpha = 0.4f)))
            Spacer(Modifier.height(14.dp))
            content()
        }
    }
}

/** The on/off look (the switch's semantics belong to its row). */
@Composable
fun SwitchTrack(on: Boolean) {
    val p = LocalPalette.current
    Box(Modifier.size(width = 44.dp, height = 26.dp).clip(RoundedCornerShape(50)).background(if (on) p.honey else p.raised)
        .then(if (on) Modifier else Modifier.border(1.dp, p.muted.copy(alpha = 0.6f), RoundedCornerShape(50)))
        .padding(3.dp), contentAlignment = if (on) Alignment.CenterEnd else Alignment.CenterStart) {
        Box(Modifier.size(20.dp).clip(CircleShape).background(if (on) p.onHoney else p.muted))
    }
}
