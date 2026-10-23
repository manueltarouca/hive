package xyz.matm.hive.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicText
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import org.json.JSONObject
import xyz.matm.hive.model.ActivityEntry
import xyz.matm.hive.model.AppModel
import xyz.matm.hive.model.HostConnection
import xyz.matm.hive.model.Resident
import xyz.matm.hive.model.ScriptOption
import xyz.matm.hive.model.ScriptWords

/**
 * A host's scripts: what each may do, what it did lately, and the switches
 * to turn it off, let it only watch, or change its options. [focus]: a
 * script to scroll to (a pane's chip opened it).
 */
@Composable
fun ScriptsScreen(model: AppModel, h: HostConnection, focus: String?, close: () -> Unit) {
    val p = LocalPalette.current
    DisposableEffect(h.id) {
        model.scriptsShown = h.id
        onDispose { if (model.scriptsShown == h.id) model.scriptsShown = null }
    }
    LaunchedEffect(h.id, h.connected) { model.loadScripts(h) }
    // Times read "2 min ago": they move on while the screen stays up.
    var tick by remember { mutableStateOf(0) }
    LaunchedEffect(Unit) { while (true) { delay(30_000); tick++ } }
    val page = model.scriptPages[h.id]
    val list = rememberLazyListState()
    var scrolled by remember { mutableStateOf(false) }
    LaunchedEffect(page?.list?.size, focus) {
        val i = page?.list?.indexOfFirst { it.name == focus } ?: -1
        if (!scrolled && i >= 0) { scrolled = true; list.animateScrollToItem(i) }
    }

    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding().imePadding()) {
        Row(Modifier.fillMaxWidth().padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.clip(RoundedCornerShape(50)).clickable(role = Role.Button, onClick = close).semantics { contentDescription = "Back" }.padding(8.dp)) {
                LineIcon(Icon.Back, p.muted, 20.dp)
            }
            Spacer(Modifier.width(6.dp))
            Column {
                BasicText("Scripts", style = Type.title.copy(color = p.text))
                FadeText(h.name, Type.tiny, p.muted)
            }
        }
        when {
            page == null || !page.loaded -> Middle {
                Hexagon("working", 14.dp)
                Spacer(Modifier.height(12.dp))
                BasicText("Reading the scripts on ${h.name}", style = Type.small.copy(color = p.muted))
            }
            page.error != null && page.list.isEmpty() -> Middle {
                Hexagon("blocked", 18.dp)
                Spacer(Modifier.height(12.dp))
                BasicText("Couldn't read the scripts", style = Type.title.copy(color = p.text))
                Spacer(Modifier.height(6.dp))
                BasicText(page.error, style = Type.small.copy(color = p.muted), modifier = Modifier.widthIn(max = 300.dp))
                Spacer(Modifier.height(16.dp))
                Pill("Try again", primary = true) { h.retryNow(); model.loadScripts(h) }
            }
            page.list.isEmpty() -> Middle {
                Hexagon("idle", 22.dp)
                Spacer(Modifier.height(14.dp))
                BasicText("No scripts on this server", style = Type.title.copy(color = p.text))
                Spacer(Modifier.height(6.dp))
                BasicText(
                    buildAnnotatedString {
                        append("Add one on the server with ")
                        withStyle(SpanStyle(fontFamily = LocalMono.current, color = p.text)) { append("hive script add") }
                        append(".")
                    },
                    style = Type.small.copy(color = p.muted), modifier = Modifier.widthIn(max = 300.dp),
                )
            }
            else -> LazyColumn(Modifier.weight(1f).fillMaxWidth(), state = list) {
                // Read before the host went away: still worth seeing, but the switches can't reach it.
                if (page.error != null) item(key = "away") {
                    Box(Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) { Problem(page.error.removeSuffix(" hive keeps trying.") + " This is what hive read last.") }
                }
                items(page.list, key = { it.name }) { s ->
                    ScriptCard(model, h, s, page.activity[s.name], page.problems, page.busy, tick)
                }
                item(key = "end") { Spacer(Modifier.height(24.dp)) }
            }
        }
    }
}

@Composable
private fun Middle(content: @Composable () -> Unit) {
    Column(Modifier.fillMaxSize().padding(24.dp), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
        content()
        Spacer(Modifier.height(64.dp)) // a little above the middle, as the desk's
    }
}

/** The dot before a script's state: sage running, honey waiting for you, coral failed or paused, an outline when off. */
@Composable
private fun StateDot(s: Resident) {
    val p = LocalPalette.current
    when (s.state) {
        "running" -> Hexagon("done", 9.dp, tint = if (s.watching) p.muted else p.sage)
        "pending" -> Hexagon("done", 9.dp, tint = p.honey)
        "failed", "quarantined" -> Hexagon("blocked", 9.dp)
        else -> Hexagon("idle", 9.dp)
    }
}

@Composable
private fun ScriptCard(model: AppModel, h: HostConnection, s: Resident, activity: List<ActivityEntry>?, problems: Map<String, String>, busy: Set<String>, tick: Int) {
    val p = LocalPalette.current
    Column(
        Modifier.padding(horizontal = 16.dp, vertical = 6.dp).fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(p.surface)
            .border(1.dp, if (s.pending) p.honey.copy(alpha = 0.55f) else p.line, RoundedCornerShape(12.dp))
            .padding(start = 14.dp, end = 6.dp, top = 6.dp, bottom = 10.dp),
    ) {
        // The name and the on/off switch.
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f).padding(top = 6.dp)) {
                BasicText(s.name, style = Type.title.copy(color = p.text), modifier = Modifier.semantics { heading() })
                Spacer(Modifier.height(3.dp))
                Row(verticalAlignment = Alignment.CenterVertically) {
                    StateDot(s)
                    Spacer(Modifier.width(7.dp))
                    BasicText(s.stateLine, style = Type.small.copy(color = if (s.state == "failed" || s.state == "quarantined") p.coral else p.muted),
                        maxLines = 2)
                }
            }
            Switch(s.on, "${s.name} on") { on -> model.changeScript(h, s.name, if (on) "script.enable" else "script.disable") }
        }
        Column(Modifier.padding(end = 8.dp)) {
            if (s.description.isNotBlank()) {
                Spacer(Modifier.height(8.dp))
                BasicText(s.description, style = Type.small.copy(color = p.text))
            }
            problems[s.name]?.let { Problem(it) }

            if (s.pending) Allow(model, h, s, s.name in busy)
            else ScriptWords.sentence(s.permissions)?.let {
                Spacer(Modifier.height(8.dp))
                BasicText(it, style = Type.tiny.copy(color = p.muted))
            } ?: run {
                Spacer(Modifier.height(8.dp))
                BasicText("It may only look.", style = Type.tiny.copy(color = p.muted))
            }
        }

        Rule()
        ToggleRow("Watch only", "It may look, mark panes and notify. Every change it tries is refused and listed below.",
            s.watching, "Watch only, ${s.name}") { on -> model.changeScript(h, s.name, "script.set_mode", JSONObject().put("mode", if (on) "watch" else "act")) }

        for (o in s.options) {
            val key = s.name + "/" + o.key
            Rule()
            when (o.type) {
                "bool" -> ToggleRow(o.label, null, o.on, "${o.label}, ${s.name}") { on ->
                    model.changeScript(h, s.name, "script.set_option", JSONObject().put("key", o.key).put("value", on), problemKey = key)
                }
                "number" -> NumberOption(model, h, s, o, problems[key])
                else -> TextOption(model, h, s, o, problems[key])
            }
            if (o.type == "bool") problems[key]?.let { Box(Modifier.padding(end = 8.dp)) { Problem(it) } }
        }

        Rule()
        Column(Modifier.padding(end = 8.dp)) {
            BasicText("Recent activity", style = Type.tiny.copy(color = p.muted, fontWeight = FontWeight(500)), modifier = Modifier.padding(top = 10.dp, bottom = 4.dp))
            when {
                activity == null -> BasicText("Reading", style = Type.small.copy(color = p.muted))
                activity.isEmpty() -> BasicText("Nothing yet.", style = Type.small.copy(color = p.muted))
                else -> for (e in activity) Activity(e, tick, s.options)
            }
        }
    }
}

@Composable
private fun Rule() {
    val p = LocalPalette.current
    Box(Modifier.padding(top = 10.dp, end = 8.dp).fillMaxWidth().height(1.dp).background(p.line))
}

@Composable
private fun Problem(text: String) {
    val p = LocalPalette.current
    Row(Modifier.padding(top = 6.dp), verticalAlignment = Alignment.Top) {
        LineIcon(Icon.Alert, p.coral, 14.dp, Modifier.padding(top = 2.dp))
        Spacer(Modifier.width(6.dp))
        BasicText(text, style = Type.small.copy(color = p.coral))
    }
}

/** A pending script: what it asks for, the warning that matters, and Allow. */
@Composable
private fun Allow(model: AppModel, h: HostConnection, s: Resident, busy: Boolean) {
    val p = LocalPalette.current
    Column(
        Modifier.padding(top = 10.dp).fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(p.honey.copy(alpha = if (p.night) 0.10f else 0.09f))
            .padding(12.dp),
    ) {
        BasicText(ScriptWords.sentence(s.asks, lead = "It asks to") ?: "It asks only to look.", style = Type.small.copy(color = p.text))
        ScriptWords.warning(s.asks)?.let {
            Spacer(Modifier.height(6.dp))
            Row(verticalAlignment = Alignment.Top) {
                LineIcon(Icon.Alert, p.coral, 14.dp, Modifier.padding(top = 2.dp))
                Spacer(Modifier.width(6.dp))
                BasicText(it, style = Type.small.copy(color = p.coral))
            }
        }
        Spacer(Modifier.height(10.dp))
        Pill(if (busy) "Allowing" else "Allow", primary = true, modifier = Modifier.semantics { contentDescription = "Allow ${s.name}" }) {
            if (!busy) model.changeScript(h, s.name, "script.grant")
        }
    }
}

/** A switch row: the whole row toggles; [description] names it for TalkBack (and the tests) among several alike. */
@Composable
private fun ToggleRow(title: String, help: String?, on: Boolean, description: String, set: (Boolean) -> Unit) {
    val p = LocalPalette.current
    Row(
        Modifier.fillMaxWidth().toggleable(value = on, role = Role.Switch, onValueChange = set)
            .semantics { contentDescription = description }.padding(top = 10.dp, end = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            BasicText(title, style = Type.body.copy(color = p.text))
            if (help != null) BasicText(help, style = Type.tiny.copy(color = p.muted), modifier = Modifier.padding(top = 2.dp))
        }
        Spacer(Modifier.width(12.dp))
        Track(on)
    }
}

/** A number: − and + within its range, or typed and saved on Done. */
@Composable
private fun NumberOption(model: AppModel, h: HostConnection, s: Resident, o: ScriptOption, problem: String?) {
    val p = LocalPalette.current
    val key = s.name + "/" + o.key
    var text by remember(o.shown) { mutableStateOf(o.shown) }
    var local by remember(o.shown) { mutableStateOf<String?>(null) }
    // What Done sent: losing focus right after mustn't send it again.
    var sent by remember(o.shown) { mutableStateOf<String?>(null) }
    var focused by remember { mutableStateOf(false) }
    val focus = LocalFocusManager.current
    fun save(v: Double) {
        local = null
        sent = text.trim()
        val value: Any = if (v == Math.floor(v) && Math.abs(v) < 1e15) v.toLong() else v
        model.changeScript(h, s.name, "script.set_option", JSONObject().put("key", o.key).put("value", value), problemKey = key)
    }
    fun commit() {
        if (text.trim() == o.shown || text.trim() == sent) { local = null; return }
        val (v, why) = o.parseNumber(text)
        if (v == null) local = why else save(v)
    }
    Column(Modifier.padding(top = 10.dp, end = 8.dp)) {
        // The label has the whole width; the range and the controls share the line below,
        // so a long label never wraps beside the − and +.
        BasicText(o.label, style = Type.body.copy(color = p.text))
        Row(Modifier.padding(top = 6.dp), verticalAlignment = Alignment.CenterVertically) {
            BasicText(o.range ?: "", style = Type.tiny.copy(color = p.muted), modifier = Modifier.weight(1f))
            Spacer(Modifier.width(10.dp))
            // Steps from what's in the field, typed or not; the field shows the result.
            val now = o.parseNumber(text).first ?: o.parseNumber(o.shown).first ?: 0.0
            fun step(v: Double) { text = if (v == Math.floor(v)) v.toLong().toString() else v.toString(); save(v) }
            Step(Icon.Minus, "Less, ${o.label}", enabled = o.min == null || now > o.min) { step((now - 1).coerceAtLeast(o.min ?: Double.NEGATIVE_INFINITY)) }
            Spacer(Modifier.width(6.dp))
            Box(
                Modifier.width(64.dp).clip(RoundedCornerShape(8.dp)).background(p.bg)
                    .border(1.dp, if (local != null || problem != null) p.coral.copy(alpha = 0.7f) else if (focused) p.honey else p.line, RoundedCornerShape(8.dp))
                    .padding(horizontal = 8.dp, vertical = 9.dp),
                contentAlignment = Alignment.Center,
            ) {
                BasicTextField(
                    text, { text = it; local = null }, singleLine = true,
                    textStyle = Type.body.copy(color = p.text, textAlign = androidx.compose.ui.text.style.TextAlign.Center),
                    cursorBrush = SolidColor(p.honey),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal, imeAction = ImeAction.Done),
                    keyboardActions = KeyboardActions(onDone = { commit(); focus.clearFocus() }),
                    modifier = Modifier.fillMaxWidth().semantics { contentDescription = "${o.label}, ${s.name}" }
                        .onFocusChanged { focused = it.isFocused; if (!it.isFocused && text != o.shown) commit() },
                )
            }
            Spacer(Modifier.width(6.dp))
            Step(Icon.Plus, "More, ${o.label}", enabled = o.max == null || now < o.max) { step((now + 1).coerceAtMost(o.max ?: Double.POSITIVE_INFINITY)) }
        }
        (local ?: problem)?.let { Problem(it) }
    }
}

@Composable
private fun Step(icon: Icon, description: String, enabled: Boolean, onClick: () -> Unit) {
    val p = LocalPalette.current
    Box(
        Modifier.size(40.dp).clip(CircleShape).background(p.raised).clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .semantics { contentDescription = description },
        contentAlignment = Alignment.Center,
    ) { LineIcon(icon, if (enabled) p.text else p.muted.copy(alpha = 0.5f), 16.dp) }
}

/** Text: saved on Done. */
@Composable
private fun TextOption(model: AppModel, h: HostConnection, s: Resident, o: ScriptOption, problem: String?) {
    val p = LocalPalette.current
    var text by remember(o.shown) { mutableStateOf(o.shown) }
    var sent by remember(o.shown) { mutableStateOf<String?>(null) }
    var focused by remember { mutableStateOf(false) }
    val focus = LocalFocusManager.current
    fun commit() {
        if (text == o.shown || text == sent) return
        sent = text
        model.changeScript(h, s.name, "script.set_option", JSONObject().put("key", o.key).put("value", text), problemKey = s.name + "/" + o.key)
    }
    Column(Modifier.padding(top = 10.dp, end = 8.dp)) {
        BasicText(o.label, style = Type.body.copy(color = p.text))
        Spacer(Modifier.height(6.dp))
        Box(
            Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).background(p.bg)
                .border(1.dp, if (problem != null) p.coral.copy(alpha = 0.7f) else if (focused) p.honey else p.line, RoundedCornerShape(8.dp))
                .padding(horizontal = 12.dp, vertical = 10.dp),
        ) {
            if (text.isEmpty()) BasicText("Not set", style = Type.body.copy(color = p.muted))
            BasicTextField(
                text, { text = it }, singleLine = true, textStyle = Type.body.copy(color = p.text), cursorBrush = SolidColor(p.honey),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                keyboardActions = KeyboardActions(onDone = { commit(); focus.clearFocus() }),
                modifier = Modifier.fillMaxWidth().semantics { contentDescription = "${o.label}, ${s.name}" }
                    .onFocusChanged { focused = it.isFocused; if (!it.isFocused) commit() },
            )
        }
        problem?.let { Problem(it) }
    }
}

/** One entry: when, then what (and who, and how it went). */
@Composable
private fun Activity(e: ActivityEntry, tick: Int, options: List<ScriptOption>) {
    val p = LocalPalette.current
    val refused = e.result.isNotEmpty() && e.result != "ok"
    Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalAlignment = Alignment.Top) {
        val ago = remember(e.time, tick) { ScriptWords.ago(e.time) }
        BasicText(ago, style = Type.tiny.copy(color = p.muted), modifier = Modifier.width(78.dp).padding(top = 1.dp))
        Row(Modifier.weight(1f), verticalAlignment = Alignment.Top) {
            if (e.byScript) {
                LineIcon(Icon.Bolt, if (refused) p.coral else p.honey, 14.dp, Modifier.padding(top = 2.dp))
                Spacer(Modifier.width(4.dp))
            }
            BasicText(ScriptWords.entry(e, options), style = Type.small.copy(color = if (refused) p.coral else p.text), maxLines = 3)
        }
    }
}

/** A pane's chip: a script just acted here. Tapping it opens the script. */
@Composable
fun ActedChipView(text: String, refused: Boolean, compact: Boolean = false, onClick: () -> Unit) {
    val p = LocalPalette.current
    val c = if (refused) p.coral else p.honey
    val tap = Modifier.clickable(role = Role.Button, onClick = onClick).semantics(mergeDescendants = true) { contentDescription = text }
    // Compact: in a pane header, as tall as the badges beside it, tapped anywhere in the header's
    // height; it fades in the room it has.
    Box(if (compact) Modifier.fillMaxHeight().then(tap) else Modifier, contentAlignment = Alignment.CenterStart) {
        Row(
            Modifier.clip(RoundedCornerShape(50)).background(if (compact) c.copy(alpha = 0.13f) else p.surface).border(1.dp, c.copy(alpha = 0.6f), RoundedCornerShape(50))
                .then(if (compact) Modifier else tap)
                .padding(start = if (compact) 7.dp else 9.dp, end = if (compact) 9.dp else 12.dp, top = if (compact) 2.dp else 6.dp, bottom = if (compact) 2.dp else 6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            LineIcon(Icon.Bolt, c, if (compact) 12.dp else 14.dp)
            Spacer(Modifier.width(if (compact) 4.dp else 5.dp))
            FadeText(text, Type.tiny.copy(fontWeight = FontWeight(500)), p.text, if (compact) Modifier else Modifier.widthIn(max = 260.dp))
        }
    }
}
