package xyz.matm.hive.ui

import android.content.ClipboardManager
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import xyz.matm.hive.model.AppModel
import xyz.matm.hive.model.HostsCode
import xyz.matm.hive.model.Offered

/** B's Add a host: the Mac's prompt, word for word, plus a code to bring the Mac's whole inventory across. */
@Composable
fun AddHost(model: AppModel, close: () -> Unit, scan: () -> Unit, offer: (List<Offered>) -> Unit, done: () -> Unit) {
    val p = LocalPalette.current
    val ctx = LocalContext.current
    var address by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var problem by remember { mutableStateOf<Pair<String, String>?>(null) }
    val focus = remember { FocusRequester() }
    LaunchedEffect(Unit) { focus.requestFocus() }

    fun go() {
        val a = address.trim()
        if (a.isEmpty() || busy) return
        busy = true
        problem = null
        model.addHost(a) { r -> busy = false; if (r == null) done() else problem = r }
    }

    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding().imePadding().verticalScroll(rememberScrollState())) {
        Row(Modifier.fillMaxWidth().padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.clip(RoundedCornerShape(50)).clickable(role = Role.Button, onClick = close).semantics { contentDescription = "Back" }.padding(8.dp)) {
                LineIcon(Icon.Back, p.muted, 20.dp)
            }
            Spacer(Modifier.width(6.dp))
            BasicText("Add a host", style = Type.title.copy(color = p.text))
        }
        Column(Modifier.padding(horizontal = 16.dp)) {
            // The Mac's palette prompt: the token, then the address.
            Row(Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(p.surface).padding(horizontal = 12.dp, vertical = 12.dp),
                verticalAlignment = Alignment.CenterVertically) {
                Box(Modifier.clip(RoundedCornerShape(8.dp)).background(p.honey.copy(alpha = 0.18f)).padding(horizontal = 8.dp, vertical = 3.dp)) {
                    BasicText("Add Host", style = Type.small.copy(color = p.honey))
                }
                Spacer(Modifier.width(10.dp))
                Box(Modifier.weight(1f)) {
                    if (address.isEmpty()) BasicText("box.example.ts.net, or https://…", style = Type.body.copy(color = p.muted))
                    BasicTextField(
                        address, { address = it; problem = null }, singleLine = true, textStyle = Type.body.copy(color = p.text),
                        cursorBrush = SolidColor(p.honey),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go, autoCorrectEnabled = false),
                        keyboardActions = KeyboardActions(onGo = { go() }),
                        modifier = Modifier.fillMaxWidth().focusRequester(focus).semantics { contentDescription = "Host address" },
                    )
                }
            }
            val a = address.trim()
            if (a.isNotEmpty()) {
                Spacer(Modifier.height(6.dp))
                Row(
                    Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(p.honey.copy(alpha = 0.12f))
                        .clickable(enabled = !busy, role = Role.Button) { go() }.padding(horizontal = 12.dp, vertical = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    LineIcon(Icon.Globe, p.text, 16.dp)
                    Spacer(Modifier.width(12.dp))
                    FadeText(if (busy) "Connecting to “$a”…" else "Connect to “$a”", Type.body, p.text, Modifier.weight(1f))
                }
            }
            problem?.let { (title, detail) ->
                Spacer(Modifier.height(10.dp))
                Row {
                    LineIcon(Icon.Alert, p.coral, 16.dp)
                    Spacer(Modifier.width(10.dp))
                    Column {
                        BasicText(title, style = Type.label.copy(color = p.text))
                        if (detail.isNotEmpty()) BasicText(detail, style = Type.small.copy(color = p.muted))
                    }
                }
            }
            Spacer(Modifier.height(14.dp))
            BasicText("A hive server on your tailnet. The phone reaches it through the Tailscale app, as your computer does.",
                style = Type.small.copy(color = p.muted))

            Spacer(Modifier.height(28.dp))
            Column(
                Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).border(1.dp, p.line, RoundedCornerShape(12.dp)).padding(16.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                LineIcon(Icon.Camera, p.honey, 28.dp)
                Spacer(Modifier.height(10.dp))
                BasicText("Or scan the code from Hive on your Mac (Hive menu, Share Hosts with Phone): it brings every host you've saved.",
                    style = Type.small.copy(color = p.muted))
                Spacer(Modifier.height(14.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Pill("Scan the code", primary = true, onClick = scan)
                    Pill("Paste it") {
                        val clip = ctx.getSystemService(ClipboardManager::class.java)?.primaryClip?.getItemAt(0)?.coerceToText(ctx)?.toString().orEmpty()
                        val list = HostsCode.decode(clip)
                        if (list == null) problem = "That isn't a hosts code" to "Copy it from Hive on your Mac: Hive menu, Share Hosts with Phone." else offer(list)
                    }
                }
            }
        }
    }
}

/** What a code offers, for a tap to save: every label and address in plain view, outsiders flagged. */
@Composable
fun Offer(model: AppModel, offered: List<Offered>, close: () -> Unit, done: () -> Unit) {
    val p = LocalPalette.current
    val existing = model.hosts.associate { it.host.label.lowercase() to it.host.address }
    // A host that would change an address you saved, or that isn't on a tailnet, starts unchecked.
    var picked by remember(offered) {
        mutableStateOf(offered.filter { o -> o.inTailnet && existing[o.host.label.lowercase()].let { it == null || it == o.host.address } }.map { it.host.label }.toSet())
    }
    // Each new one asked who it is: a server you already have, by another address or name, starts unchecked too.
    var same by remember(offered) { mutableStateOf<Map<String, String>>(emptyMap()) }
    var touched by remember(offered) { mutableStateOf(emptySet<String>()) }
    LaunchedEffect(offered) {
        // Only addresses on the tailnet, or on a machine you already connect to, are asked: a code
        // can name any address, and asking is connecting.
        val known = model.hosts.mapNotNull { xyz.matm.hive.net.Endpoint.candidates(it.host.address).firstOrNull()?.host?.lowercase() }.toSet()
        fun askable(o: Offered) = o.inTailnet || xyz.matm.hive.net.Endpoint.candidates(o.host.address).firstOrNull()?.host?.lowercase() in known
        for (o in offered) if (askable(o) && existing[o.host.label.lowercase()] == null) model.sameServer(o.host.address) { name ->
            if (name != null && !name.equals(o.host.label, true)) {
                same = same + (o.host.label to name)
                if (o.host.label !in touched) picked = picked - o.host.label
            }
        }
    }
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding().verticalScroll(rememberScrollState())) {
        Row(Modifier.fillMaxWidth().padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.clip(RoundedCornerShape(50)).clickable(role = Role.Button, onClick = close).semantics { contentDescription = "Back" }.padding(8.dp)) {
                LineIcon(Icon.Back, p.muted, 20.dp)
            }
            Spacer(Modifier.width(6.dp))
            BasicText("Hosts from your Mac", style = Type.title.copy(color = p.text))
        }
        Column(Modifier.padding(horizontal = 16.dp)) {
            if (offered.isEmpty()) BasicText("The code has no hosts this phone can use.", style = Type.body.copy(color = p.muted))
            for (o in offered) {
                val on = o.host.label in picked
                val old = existing[o.host.label.lowercase()]
                Row(
                    Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp))
                        .clickable(role = Role.Checkbox) { touched = touched + o.host.label; picked = if (on) picked - o.host.label else picked + o.host.label }
                        .semantics { contentDescription = "${o.host.label} ${o.host.address}" }
                        .padding(vertical = 10.dp, horizontal = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Box(Modifier.size(22.dp).clip(RoundedCornerShape(6.dp)).background(if (on) p.honey else p.surface)
                        .border(1.5.dp, if (on) p.honey else p.muted, RoundedCornerShape(6.dp)), contentAlignment = Alignment.Center) {
                        if (on) LineIcon(Icon.Check, p.onHoney, 16.dp)
                    }
                    Spacer(Modifier.width(14.dp))
                    Column(Modifier.weight(1f)) {
                        BasicText(o.host.label, style = Type.body.copy(color = p.text))
                        FadeText(o.host.address, Type.small, p.muted)
                        if (!o.inTailnet) BasicText("Not a tailnet address: whoever runs it could read what you type.", style = Type.tiny.copy(color = p.coral))
                        if (old != null && old != o.host.address) BasicText("Replaces $old", style = Type.tiny.copy(color = p.coral))
                        else if (old != null) BasicText("Already saved", style = Type.tiny.copy(color = p.muted))
                        same[o.host.label]?.let { BasicText("Same server as $it, by another address", style = Type.tiny.copy(color = p.honey)) }
                    }
                }
            }
            Spacer(Modifier.height(10.dp))
            BasicText("Added on demand: each connects when you pick it.", style = Type.small.copy(color = p.muted))
            Spacer(Modifier.height(16.dp))
            val n = picked.size
            Pill(if (n == 1) "Add 1 host" else "Add $n hosts", primary = n > 0) {
                if (n > 0) { model.addOffered(offered.filter { it.host.label in picked }.map { it.host }); done() }
            }
        }
    }
}
