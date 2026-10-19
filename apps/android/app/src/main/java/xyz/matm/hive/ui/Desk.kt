package xyz.matm.hive.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
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
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicText
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import xyz.matm.hive.agents.Ask
import xyz.matm.hive.agents.Harness
import xyz.matm.hive.model.AppModel
import xyz.matm.hive.model.Link

/** The pane's cwd as people read it: ~ for a home directory. */
fun shortPath(p: String): String {
    val m = Regex("^/(Users|home)/[^/]+").find(p) ?: return if (p == "/root" || p.startsWith("/root/")) "~" + p.removePrefix("/root") else p
    return "~" + p.removePrefix(m.value)
}

/** C Desk: the Mac app, small. The title chip, tabs as pills, one pane at a time, the key bar. */
@Composable
fun Desk(model: AppModel, openSwitcher: () -> Unit, openLink: (String) -> Unit) {
    val p = LocalPalette.current
    androidx.compose.runtime.SideEffect { model.setDark(p.night) }
    val h = model.current
    var phone by remember { mutableStateOf<IntArray?>(null) }
    var ctrl by remember { mutableStateOf(false) }
    var view by remember { mutableStateOf<TerminalView?>(null) }
    var confirmPaste by remember { mutableStateOf<Pair<Int, () -> Unit>?>(null) }
    val block = model.visibleBlock?.let { model.blocks[it] }
    val frame = model.frame
    val ask = remember(frame, block?.agentState) {
        if (frame != null && block?.agentState == "blocked") Ask.find((0 until frame.rows).map(frame::rowText)) else null
    }

    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding().imePadding()) {
        // The title chip: session and host; the switcher behind it.
        Row(Modifier.fillMaxWidth().padding(start = 12.dp, end = 12.dp, top = 6.dp, bottom = 4.dp), verticalAlignment = Alignment.CenterVertically) {
            Row(
                Modifier.weight(1f).clip(RoundedCornerShape(10.dp)).clickable(role = Role.Button, onClick = openSwitcher)
                    .semantics { contentDescription = "Switch session" }.padding(horizontal = 6.dp, vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                LineIcon(Icon.Globe, p.muted, 18.dp)
                Spacer(Modifier.width(10.dp))
                Column(Modifier.weight(1f, fill = false)) {
                    FadeText(model.sessionLabel.ifEmpty { if (model.currentSession == null) "No session" else "…" }, Type.title, p.text)
                    val sub = when {
                        h == null -> "Pick a session"
                        h.link == Link.Connected -> h.name
                        else -> h.shortProblem ?: h.name
                    }
                    FadeText(sub, Type.tiny, if (h != null && h.link != Link.Connected && h.link != Link.Idle) p.honey else p.muted)
                }
                Spacer(Modifier.width(4.dp))
                LineIcon(Icon.Chevron, p.muted, 16.dp)
            }
            if (model.stream != null) {
                Pill(if (model.fitted) "Mac size" else "Fit to phone", selected = model.fitted) {
                    val s = phone
                    if (model.fitted) model.unfit() else if (s != null) model.fit(s[0], s[1], s[2], s[3])
                }
            }
            Spacer(Modifier.width(4.dp))
            Box(
                Modifier.size(40.dp).clip(CircleShape).clickable(role = Role.Button) { model.newTab() }
                    .semantics { contentDescription = "New tab" },
                contentAlignment = Alignment.Center,
            ) { LineIcon(Icon.Plus, p.muted, 20.dp) }
        }

        // Tabs, with their agents' state.
        if (model.windows.size > 1) {
            Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 12.dp, vertical = 4.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                model.windows.forEachIndexed { i, w ->
                    val bs = w.blocks.mapNotNull { model.blocks[it] }
                    val state = listOf("blocked", "working", "done").firstOrNull { s -> bs.any { it.agentState == s } }
                    val label = model.titleParts(w).first
                    val sel = i == model.windowIndex
                    Row(
                        Modifier.clip(RoundedCornerShape(50))
                            .background(if (sel) p.raised else p.surface.copy(alpha = if (p.night) 0.6f else 1f))
                            .then(if (sel) Modifier.border(1.dp, p.line, RoundedCornerShape(50)) else Modifier)
                            .clickable(role = Role.Tab) { model.selectWindow(i) }
                            .padding(start = 7.dp, end = 12.dp, top = 5.dp, bottom = 5.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        // The Mac's tab: the program's cell (a small comb for a split), the name, the state.
                        ProgramTile(model.program(w), 20.dp, model.otherPrograms(w))
                        Spacer(Modifier.width(7.dp))
                        FadeText(label, Type.small, if (sel) p.text else p.muted, Modifier.widthIn(max = 140.dp))
                        if (state != null) { Spacer(Modifier.width(7.dp)); Hexagon(state, 8.dp) }
                    }
                }
            }
        }

        // The pane.
        val paneShape = RoundedCornerShape(12.dp)
        Box(
            Modifier.weight(1f).fillMaxWidth().padding(horizontal = 8.dp, vertical = 6.dp).clip(paneShape)
                .border(1.dp, if (ask != null) p.coral.copy(alpha = 0.7f) else p.honey.copy(alpha = 0.55f), paneShape),
        ) {
            Column(Modifier.fillMaxSize()) {
                if (block != null) {
                    Row(Modifier.fillMaxWidth().background(p.surface).padding(horizontal = 12.dp, vertical = 7.dp), verticalAlignment = Alignment.CenterVertically) {
                        // The Mac's pane header: the program's mark, the title and where, the state.
                        val (title, where) = block.titleParts
                        ProgramGlyph(block.program, p.text.copy(alpha = 0.9f), 14.dp)
                        Spacer(Modifier.width(8.dp))
                        // The title takes the room and fades; the folder stays short beside it.
                        FadeText(title, Type.small.copy(fontWeight = androidx.compose.ui.text.font.FontWeight(500)), p.text, Modifier.weight(1f, fill = false))
                        if (where.isNotEmpty()) { Spacer(Modifier.width(8.dp)); FadeText(where, Type.small, p.muted, Modifier.widthIn(max = 110.dp)) }
                        Spacer(Modifier.weight(0.001f))
                        if (block.agentState in setOf("working", "blocked", "done")) { Spacer(Modifier.width(8.dp)); StateBadge(block.agentState!!) }
                    }
                }
                AndroidView(
                    factory = { ctx -> TerminalView(ctx).also { view = it } },
                    modifier = Modifier.weight(1f).fillMaxWidth(),
                    update = { v ->
                        v.theme = model.termTheme
                        v.textSp = model.textSp
                        v.stream = model.stream
                        v.fitted = model.fitted
                        v.show(model.frame)
                        v.onPhoneSize = { c, r, cw, ch -> phone = intArrayOf(c, r, cw, ch); model.phoneSize(c, r, cw, ch) }
                        v.onLink = openLink
                        v.onSwipe = { d -> model.selectPane(model.paneIndex + d) }
                        v.onPinched = { model.sizeLearned() }
                        v.ctrlLatched = ctrl
                        v.onCtrlUsed = { ctrl = false }
                    },
                )
            }
            if (model.currentSession == null) EmptyDesk(model, openSwitcher)
            // Away: the Mac's card, over the pane, while its host can't be reached.
            Box(Modifier.align(Alignment.Center)) { Away(h, show = h != null && model.currentSession != null && h.link != Link.Connected && h.link != Link.Idle) }
        }

        // The dots: the panes of a split; swipe sideways between them.
        val panes = model.windows.getOrNull(model.windowIndex)?.blocks?.size ?: 0
        if (panes > 1) {
            Row(Modifier.fillMaxWidth().padding(bottom = 4.dp), horizontalArrangement = Arrangement.Center) {
                for (i in 0 until panes) {
                    val sel = i == model.paneIndex
                    Box(
                        Modifier.padding(horizontal = 3.dp).size(width = if (sel) 16.dp else 6.dp, height = 6.dp).clip(RoundedCornerShape(50))
                            .background(if (sel) p.honey else p.muted.copy(alpha = 0.5f))
                            .clickable(role = Role.Button) { model.selectPane(i) }
                            .semantics { contentDescription = "Pane ${i + 1} of $panes" },
                    )
                }
            }
        }

        if (!model.sizeHintSeen && !model.fitted && model.stream != null && frame != null && phone != null && frame.cols > (phone?.get(0) ?: 0) + 4) {
            BasicText("This pane is your Mac's size, zoomed to fit. Pinch to read closer, or fit it to the phone.",
                style = Type.tiny.copy(color = p.muted), modifier = Modifier.padding(horizontal = 16.dp, vertical = 2.dp))
        }

        if (ask != null && block != null) AskCard(ask, Harness.name(block.harness ?: ""), model)

        confirmPaste?.let { (lines, go) -> PasteCard(lines, paste = { go(); confirmPaste = null }, cancel = { confirmPaste = null }) }

        KeyBar(model, ask, ctrl, onCtrl = { ctrl = !ctrl }, onKeyboard = { view?.showKeyboard() },
            onConfirmPaste = { lines, go -> confirmPaste = lines to go })
    }
}

/** The Mac's StatusBadge: "Blocked" in words, else the state's hexagon. */
@Composable
fun StateBadge(state: String) {
    val p = LocalPalette.current
    if (state == "blocked") {
        Row(Modifier.clip(RoundedCornerShape(50)).background(p.coral.copy(alpha = 0.14f)).padding(horizontal = 7.dp, vertical = 2.dp),
            verticalAlignment = Alignment.CenterVertically) {
            Hexagon("blocked", 7.dp)
            Spacer(Modifier.width(4.dp))
            BasicText("Blocked", style = Type.tiny.copy(color = p.coral, fontWeight = androidx.compose.ui.text.font.FontWeight(600)))
        }
    } else Hexagon(state, 8.dp)
}

@Composable
private fun EmptyDesk(model: AppModel, openSwitcher: () -> Unit) {
    val p = LocalPalette.current
    Column(Modifier.fillMaxSize().background(p.bg).padding(24.dp), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
        Hexagon("idle", 22.dp)
        Spacer(Modifier.height(14.dp))
        BasicText(if (model.hosts.isEmpty()) "Add a host to begin" else "Open a session", style = Type.title.copy(color = p.text))
        Spacer(Modifier.height(6.dp))
        BasicText(
            if (model.hosts.isEmpty()) "A hive server on your tailnet. The phone reaches it through the Tailscale app, as your Mac does."
            else "Your hosts' sessions are in the switcher.",
            style = Type.small.copy(color = p.muted), modifier = Modifier.widthIn(max = 300.dp),
        )
        Spacer(Modifier.height(18.dp))
        Pill(if (model.hosts.isEmpty()) "Add a host" else "Open the switcher", primary = true, onClick = openSwitcher)
    }
}

@Composable
private fun Away(h: xyz.matm.hive.model.HostConnection?, show: Boolean) {
    AnimatedVisibility(show, enter = fadeIn(tween(200, easing = Quiet)), exit = fadeOut(tween(200, easing = Quiet))) {
        if (h != null) AwayCard(h.problem ?: "", h.detail, refused = h.link is Link.Refused)
    }
}

@Composable
fun AwayCard(title: String, detail: String?, refused: Boolean) {
    val p = LocalPalette.current
    Column(
        Modifier.padding(24.dp).widthIn(max = 320.dp).clip(RoundedCornerShape(12.dp)).background(p.surface)
            .border(1.dp, p.line, RoundedCornerShape(12.dp)).padding(16.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Hexagon(if (refused) "blocked" else "working", 11.dp)
            Spacer(Modifier.width(10.dp))
            BasicText(title.removeSuffix("…"), style = Type.label.copy(color = p.text))
        }
        if (detail != null) {
            Spacer(Modifier.height(6.dp))
            BasicText(detail, style = Type.small.copy(color = p.muted))
        }
    }
}

/** A from A Inbox: the choices a blocked agent offers, lifted into buttons; or words for it. */
@Composable
private fun AskCard(ask: Ask, agent: String, model: AppModel) {
    val p = LocalPalette.current
    var reply by remember(ask.question) { mutableStateOf("") }
    Column(
        Modifier.fillMaxWidth().padding(horizontal = 8.dp, vertical = 4.dp).clip(RoundedCornerShape(12.dp))
            .background(p.coral.copy(alpha = if (p.night) 0.10f else 0.08f)).border(1.dp, p.coral.copy(alpha = 0.35f), RoundedCornerShape(12.dp))
            .padding(12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Hexagon("blocked", 9.dp)
            Spacer(Modifier.width(8.dp))
            FadeText(ask.question, Type.label, p.text)
        }
        Spacer(Modifier.height(10.dp))
        Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            ask.choices.forEachIndexed { i, c ->
                Pill(c.short, primary = i == 0, modifier = Modifier.semantics { contentDescription = c.label }) { model.answer(c.key) }
            }
        }
        Spacer(Modifier.height(10.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.weight(1f).clip(RoundedCornerShape(50)).background(p.surface).padding(horizontal = 14.dp, vertical = 10.dp)) {
                if (reply.isEmpty()) BasicText("Tell ${agent.removeSuffix(" Code")} what to do", style = Type.small.copy(color = p.muted))
                BasicTextField(
                    reply, { reply = it }, singleLine = true, textStyle = Type.small.copy(color = p.text), cursorBrush = SolidColor(p.honey),
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
                    keyboardActions = KeyboardActions(onSend = { if (reply.isNotBlank()) { model.reply(reply); reply = "" } }),
                    modifier = Modifier.fillMaxWidth().semantics { contentDescription = "Reply" },
                )
            }
            Spacer(Modifier.width(8.dp))
            Box(
                Modifier.size(40.dp).clip(CircleShape).background(if (reply.isBlank()) p.raised else p.honey)
                    .clickable(enabled = reply.isNotBlank(), role = Role.Button) { model.reply(reply); reply = "" }
                    .semantics { contentDescription = "Send reply" },
                contentAlignment = Alignment.Center,
            ) { LineIcon(Icon.Send, if (reply.isBlank()) p.muted else p.onHoney, 18.dp) }
        }
    }
}

/** The key bar: what a phone keyboard lacks; the prompt's own answers while one is up. */
@Composable
private fun KeyBar(model: AppModel, ask: Ask?, ctrl: Boolean, onCtrl: () -> Unit, onKeyboard: () -> Unit,
                   onConfirmPaste: (Int, () -> Unit) -> Unit) {
    val p = LocalPalette.current
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val s = model.stream
    data class K(val label: String, val describe: String, val send: () -> Unit)
    val keys = buildList {
        ask?.choices?.forEach { c -> add(K(c.key, "Answer ${c.key}") { model.answer(c.key) }) }
        add(K("esc", "Escape") { s?.key("escape") })
        if (ask == null) {
            add(K("ctrl", "Control") { onCtrl() })
            add(K("tab", "Tab") { s?.key("tab") })
            add(K("⌃C", "Control C") { s?.key("ctrl+c") })
            add(K("paste", "Paste") { paste(ctx, s, onConfirmPaste, model.confirmPaste) })
            add(K("↑", "Up") { s?.key("up") })
            add(K("↓", "Down") { s?.key("down") })
            add(K("←", "Left") { s?.key("left") })
            add(K("→", "Right") { s?.key("right") })
        } else {
            add(K("↵", "Return") { s?.key("enter") })
            add(K("⌃C", "Control C") { s?.key("ctrl+c") })
        }
        if (ask == null) for (c in listOf("/", "@", "|", "~", "-")) add(K(c, c) { s?.text(c) })
    }
    Row(Modifier.fillMaxWidth().background(p.surface).padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Row(Modifier.weight(1f).horizontalScroll(rememberScrollState()).padding(start = 8.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            for (k in keys) {
                val on = k.label == "ctrl" && ctrl
                Box(
                    Modifier.clip(RoundedCornerShape(10.dp)).background(if (on) p.honey else p.raised)
                        .clickable(enabled = s != null, role = Role.Button, onClick = k.send)
                        .semantics { contentDescription = k.describe }
                        .widthIn(min = 42.dp).padding(horizontal = 9.dp, vertical = 9.dp),
                    contentAlignment = Alignment.Center,
                ) { BasicText(k.label, style = Type.mono.copy(fontFamily = LocalMono.current, fontSize = 15.sp, color = if (on) p.onHoney else p.text)) }
            }
        }
        Box(
            Modifier.padding(horizontal = 8.dp).size(40.dp).clip(RoundedCornerShape(10.dp)).background(p.raised)
                .clickable(enabled = s != null, role = Role.Button, onClick = onKeyboard).semantics { contentDescription = "Keyboard" },
            contentAlignment = Alignment.Center,
        ) { LineIcon(Icon.Keyboard, p.text, 20.dp) }
    }
}

/** Paste the clipboard's text; text that could run commands (lines, without bracketed paste) asks first, as on the Mac. */
private fun paste(ctx: android.content.Context, s: xyz.matm.hive.model.PaneStream?, confirm: (Int, () -> Unit) -> Unit, ask: Boolean = true) {
    s ?: return
    val text = ctx.getSystemService(android.content.ClipboardManager::class.java)?.primaryClip?.getItemAt(0)?.coerceToText(ctx)?.toString()
    if (text.isNullOrEmpty()) {
        android.widget.Toast.makeText(ctx, "Nothing to paste", android.widget.Toast.LENGTH_SHORT).show()
        return
    }
    s.paste(text) { lines, go -> if (ask) confirm(lines, go) else go() }
}

@Composable
private fun PasteCard(lines: Int, paste: () -> Unit, cancel: () -> Unit) {
    val p = LocalPalette.current
    Column(
        Modifier.fillMaxWidth().padding(horizontal = 8.dp, vertical = 4.dp).clip(RoundedCornerShape(12.dp)).background(p.surface)
            .border(1.dp, p.line, RoundedCornerShape(12.dp)).padding(12.dp),
    ) {
        BasicText(if (lines > 1) "Paste $lines lines?" else "Paste this?", style = Type.label.copy(color = p.text))
        Spacer(Modifier.height(4.dp))
        BasicText("The program didn't ask for pasted text, so each line may run as a command.", style = Type.small.copy(color = p.muted))
        Spacer(Modifier.height(10.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Pill("Paste", primary = true, modifier = Modifier.semantics { contentDescription = "Paste anyway" }, onClick = paste)
            Pill("Cancel", onClick = cancel)
        }
    }
}
