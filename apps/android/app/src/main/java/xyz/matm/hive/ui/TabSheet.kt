package xyz.matm.hive.ui

import androidx.compose.animation.core.animate
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.animation.core.tween
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.gestures.detectVerticalDragGestures
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.systemGestureExclusion
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
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
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import xyz.matm.hive.model.AppModel
import xyz.matm.hive.model.TabGroups
import xyz.matm.hive.model.WindowInfo
import kotlin.math.roundToInt

/**
 * B Tab sheet's strip: with many tabs, a count first (tap it, or swipe up),
 * then pins, what needs you, and where you are. Hold a tab for its actions.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun TabStrip(model: AppModel, openSheet: () -> Unit, openMenu: (WindowInfo) -> Unit) {
    val p = LocalPalette.current
    val tabs = model.tabs()
    val shown = TabGroups.strip(tabs, model.windowIndex)
    val all = tabs.size > TabGroups.ALL_IN_STRIP
    Row(
        Modifier.fillMaxWidth()
            .pointerInput(Unit) {
                var dy = 0f
                detectVerticalDragGestures(onDragStart = { dy = 0f }, onDragEnd = { if (dy < -40.dp.toPx()) openSheet() }) { _, d -> dy += d }
            }
            .horizontalScroll(rememberScrollState()).padding(horizontal = 12.dp, vertical = 4.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        if (all) {
            // The count wears the states of what's behind it, as a small comb.
            val states = listOf("blocked", "done", "working").filter { s -> tabs.any { it.state == s } }
            Row(
                Modifier.clip(RoundedCornerShape(50)).border(1.dp, p.line, RoundedCornerShape(50))
                    .clickable(role = Role.Button, onClick = openSheet)
                    .semantics { contentDescription = "All ${tabs.size} tabs" }
                    .heightIn(min = 34.dp).padding(horizontal = 11.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (states.isNotEmpty()) {
                    Row(horizontalArrangement = Arrangement.spacedBy(2.dp)) { for (s in states) Hexagon(s, 7.dp) }
                    Spacer(Modifier.width(7.dp))
                }
                BasicText("${tabs.size}", style = Type.small.copy(color = p.text, fontWeight = FontWeight(500)))
            }
        }
        for (i in shown) {
            val w = model.windows[i]
            val state = model.tabState(w)
            val label = model.titleParts(w).first
            val sel = i == model.windowIndex
            Row(
                Modifier.clip(RoundedCornerShape(50))
                    .background(if (sel) p.raised else p.surface.copy(alpha = if (p.night) 0.6f else 1f))
                    .then(if (sel) Modifier.border(1.dp, p.line, RoundedCornerShape(50)) else Modifier)
                    .combinedClickable(role = Role.Tab, onLongClickLabel = "Tab actions", onLongClick = { openMenu(w) }) { model.selectWindow(i) }
                    .semantics { if (w.pinned) contentDescription = "$label, pinned" }
                    .padding(start = 7.dp, end = 12.dp, top = 5.dp, bottom = 5.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (w.pinned) { LineIcon(Icon.Pin, p.honey, 12.dp); Spacer(Modifier.width(4.dp)) }
                // The Mac's tab: the program's cell (a small comb for a split), the name, the state.
                ProgramTile(model.program(w), 20.dp, model.otherPrograms(w))
                Spacer(Modifier.width(7.dp))
                FadeText(label, Type.small, if (sel) p.text else p.muted, Modifier.widthIn(max = 140.dp))
                if (state != null) { Spacer(Modifier.width(7.dp)); Hexagon(state, 8.dp) }
            }
        }
    }
}

/**
 * Every tab, one swipe up: Pinned, Needs you, Working, then the quiet ones
 * as a line of names. Each card shows its pane's last two lines. Swipe a
 * card right to pin it; hold it for the rest.
 */
@Composable
fun TabSheet(model: AppModel, close: () -> Unit, openMenu: (WindowInfo) -> Unit) {
    val p = LocalPalette.current
    var filter by remember { mutableStateOf("") }
    LaunchedEffect(Unit) { while (true) { model.loadPreviews(); delay(3000) } }
    fun go(i: Int) { model.selectWindow(i); close() }
    val tabs = model.tabs()
    Box(Modifier.fillMaxSize().background(Color.Black.copy(alpha = 0.42f))
        .clickable(indication = null, interactionSource = remember { MutableInteractionSource() }, onClick = close)) {
        BoxWithConstraints(Modifier.align(Alignment.BottomCenter).fillMaxWidth()) {
            Column(Modifier.fillMaxWidth().heightIn(max = maxHeight * 0.86f).clip(RoundedCornerShape(topStart = 22.dp, topEnd = 22.dp))
                .background(p.surface).clickable(indication = null, interactionSource = remember { MutableInteractionSource() }) {}
                .navigationBarsPadding().imePadding().padding(top = 10.dp)) {
                Box(Modifier.align(Alignment.CenterHorizontally).size(width = 36.dp, height = 4.dp).clip(CircleShape).background(p.muted.copy(alpha = 0.4f)))
                Row(Modifier.fillMaxWidth().padding(start = 20.dp, end = 12.dp, top = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        BasicText("${tabs.size} tabs", style = Type.title.copy(color = p.text))
                        FadeText(model.sessionLabel, Type.tiny, p.muted)
                    }
                    Pill("New tab") { model.newTab(); close() }
                }
                // Filter by name.
                Row(Modifier.padding(horizontal = 16.dp, vertical = 12.dp).fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(p.bg)
                    .padding(horizontal = 12.dp, vertical = 11.dp), verticalAlignment = Alignment.CenterVertically) {
                    LineIcon(Icon.Search, p.muted, 16.dp)
                    Spacer(Modifier.width(8.dp))
                    Box(Modifier.weight(1f)) {
                        if (filter.isEmpty()) BasicText("Filter tabs", style = Type.small.copy(color = p.muted))
                        BasicTextField(filter, { filter = it }, singleLine = true, textStyle = Type.small.copy(color = p.text), cursorBrush = SolidColor(p.honey),
                            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Go, autoCorrectEnabled = false),
                            keyboardActions = KeyboardActions(onGo = {
                                // The first one shown: pins first.
                                TabGroups.shown(tabs.map { it.pinned }).firstOrNull { matches(model, model.windows[it], filter) }?.let(::go)
                            }),
                            modifier = Modifier.fillMaxWidth().semantics { contentDescription = "Filter tabs" })
                    }
                }
                Column(Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).padding(bottom = 16.dp)) {
                    if (filter.isNotBlank()) {
                        val hits = TabGroups.shown(tabs.map { it.pinned }).filter { matches(model, model.windows[it], filter) }
                        if (hits.isEmpty()) BasicText("No tab is called that.", style = Type.small.copy(color = p.muted), modifier = Modifier.padding(vertical = 12.dp))
                        Cards(model, hits, ::go, openMenu)
                    } else {
                        val g = TabGroups.groups(tabs)
                        Group("Pinned", g.pinned.size, p.muted) { Cards(model, g.pinned, ::go, openMenu) }
                        Group("Needs you", g.needsYou.size, p.coral) { Cards(model, g.needsYou, ::go, openMenu) }
                        Group("Working", g.working.size, p.muted) { Cards(model, g.working, ::go, openMenu) }
                        Group("Quiet", g.quiet.size, p.muted, count = g.pinned.size + g.needsYou.size + g.working.size > 0) { Quiet(model, g.quiet, ::go, openMenu) }
                    }
                }
            }
        }
    }
}

private fun matches(model: AppModel, w: WindowInfo, q: String): Boolean {
    val (t, where) = model.titleParts(w)
    return q.isBlank() || t.contains(q.trim(), true) || where.contains(q.trim(), true)
}

@Composable
private fun Group(title: String, n: Int, color: Color, count: Boolean = false, content: @Composable () -> Unit) {
    if (n == 0) return
    val p = LocalPalette.current
    Row(Modifier.padding(top = 10.dp, bottom = 8.dp, start = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        BasicText(title, style = Type.tiny.copy(color = color, fontWeight = FontWeight(500)))
        if (count) { Spacer(Modifier.width(6.dp)); BasicText("$n", style = Type.tiny.copy(color = p.muted)) }
    }
    content()
}

/** Cards two by two. */
@Composable
private fun Cards(model: AppModel, ids: List<Int>, go: (Int) -> Unit, openMenu: (WindowInfo) -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        for (row in ids.chunked(2)) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                for (i in row) Box(Modifier.weight(1f)) { Card(model, model.windows[i], i, go, openMenu) }
                if (row.size == 1) Spacer(Modifier.weight(1f))
            }
        }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun Card(model: AppModel, w: WindowInfo, i: Int, go: (Int) -> Unit, openMenu: (WindowInfo) -> Unit) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()
    // The drag, kept here as it happens: letting go reads it at once.
    var dx by remember(w.id) { mutableFloatStateOf(0f) }
    fun settle() { scope.launch { animate(dx, 0f, animationSpec = tween(180, easing = Quiet)) { v, _ -> dx = v } } }
    val reach = with(LocalDensity.current) { 88.dp.toPx() }
    val label = model.titleParts(w).first
    val sel = i == model.windowIndex
    val pinWord = if (w.pinned) "Unpin" else "Pin"
    val shape = RoundedCornerShape(12.dp)
    // A swipe from the screen's edge pins the card instead of going back.
    Box(Modifier.fillMaxWidth().systemGestureExclusion().clip(shape)) {
        // Under the card: what letting go does.
        Row(Modifier.matchParentSize().background(p.honey.copy(alpha = (dx / reach).coerceIn(0f, 1f))).padding(start = 14.dp),
            verticalAlignment = Alignment.CenterVertically) {
            LineIcon(Icon.Pin, p.onHoney, 14.dp)
            Spacer(Modifier.width(6.dp))
            BasicText(pinWord, style = Type.small.copy(color = p.onHoney, fontWeight = FontWeight(500)))
        }
        Column(
            Modifier.offset { IntOffset(dx.roundToInt(), 0) }.fillMaxWidth().clip(shape).background(p.bg)
                .border(1.dp, if (sel) p.honey.copy(alpha = 0.7f) else p.line, shape)
                .pointerInput(w.id, w.pinned) {
                    detectHorizontalDragGestures(
                        onDragEnd = {
                            if (dx >= reach) model.pinTab(w, !w.pinned)
                            settle()
                        },
                        onDragCancel = { settle() },
                    ) { change, d -> change.consume(); dx = (dx + d).coerceIn(0f, reach * 1.25f) }
                }
                .combinedClickable(role = Role.Button, onLongClickLabel = "Tab actions", onLongClick = { openMenu(w) }) { go(i) }
                .semantics {
                    contentDescription = label + if (w.pinned) ", pinned" else ""
                    customActions = listOf(CustomAccessibilityAction("$pinWord $label") { model.pinTab(w, !w.pinned); true })
                }
                .padding(horizontal = 10.dp, vertical = 9.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (w.pinned) { LineIcon(Icon.Pin, p.honey, 11.dp); Spacer(Modifier.width(4.dp)) }
                ProgramTile(model.program(w), 16.dp, model.otherPrograms(w))
                Spacer(Modifier.width(6.dp))
                FadeText(label, Type.small.copy(fontWeight = FontWeight(500)), p.text, Modifier.weight(1f, fill = false))
                model.tabState(w)?.let { Spacer(Modifier.weight(0.001f)); Spacer(Modifier.width(6.dp)); Hexagon(it, 8.dp) }
            }
            Spacer(Modifier.height(6.dp))
            val lines = model.previews[w.id] ?: listOf(model.titleParts(w).second.ifEmpty { " " })
            val mono = Type.mono.copy(fontFamily = LocalMono.current, fontSize = 11.sp, lineHeight = 15.sp)
            // Indents dropped: two lines read better from the edge.
            for (k in 0 until 2) FadeText(lines.getOrNull(k)?.trimStart() ?: " ", mono, p.muted)
        }
    }
}

/** The quiet tabs: a line of names. */
@OptIn(ExperimentalFoundationApi::class, ExperimentalLayoutApi::class)
@Composable
private fun Quiet(model: AppModel, ids: List<Int>, go: (Int) -> Unit, openMenu: (WindowInfo) -> Unit) {
    val p = LocalPalette.current
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        for (i in ids) {
            val w = model.windows[i]
            val sel = i == model.windowIndex
            Row(
                Modifier.clip(RoundedCornerShape(50)).background(p.bg)
                    .border(1.dp, if (sel) p.honey.copy(alpha = 0.7f) else p.line, RoundedCornerShape(50))
                    .combinedClickable(role = Role.Button, onLongClickLabel = "Tab actions", onLongClick = { openMenu(w) }) { go(i) }
                    .heightIn(min = 36.dp).padding(start = 6.dp, end = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                ProgramTile(model.program(w), 16.dp, model.otherPrograms(w))
                Spacer(Modifier.width(6.dp))
                FadeText(model.titleParts(w).first, Type.small, p.text, Modifier.widthIn(max = 150.dp))
            }
        }
    }
}

/** Hold a tab: pin it, rename it, close it (asked first: it ends what runs there). */
@Composable
fun TabMenu(model: AppModel, w: WindowInfo, done: () -> Unit) {
    val p = LocalPalette.current
    var step by remember(w.id) { mutableStateOf("menu") }
    val label = model.titleParts(w).first
    if (step == "rename") { TabRename(model, w, done); return }
    Sheet(onDismiss = done) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            ProgramTile(model.program(w), 22.dp, model.otherPrograms(w))
            Spacer(Modifier.width(10.dp))
            FadeText(label, Type.title, p.text)
        }
        if (step == "close") {
            val n = w.blocks.size
            Spacer(Modifier.height(8.dp))
            BasicText(if (n > 1) "Close $label? Its $n panes end, and what runs in them." else "Close $label? What runs in it ends.",
                style = Type.small.copy(color = p.muted))
            Spacer(Modifier.height(14.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Pill("Close tab", danger = true, modifier = Modifier.semantics { contentDescription = "Close $label now" }) { model.closeTab(w); done() }
                Pill("Keep it", onClick = done)
            }
            return@Sheet
        }
        Spacer(Modifier.height(10.dp))
        MenuRow(if (w.pinned) "Unpin tab" else "Pin tab", if (w.pinned) "Back among the others, here and on the Mac." else "First in the tabs, here and on the Mac.",
            Icon.Pin) { model.pinTab(w, !w.pinned); done() }
        MenuRow("Rename", "As the Mac and the CLI show it.", null) { step = "rename" }
        MenuRow("Close tab", null, null, danger = true) { step = "close" }
    }
}

@Composable
private fun MenuRow(title: String, detail: String?, icon: Icon?, danger: Boolean = false, onClick: () -> Unit) {
    val p = LocalPalette.current
    Row(Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp)).clickable(role = Role.Button, onClick = onClick)
        .heightIn(min = 52.dp).padding(horizontal = 4.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.width(26.dp)) { if (icon != null) LineIcon(icon, p.honey, 16.dp) }
        Column(Modifier.weight(1f)) {
            BasicText(title, style = Type.body.copy(color = if (danger) p.coral else p.text))
            if (detail != null) BasicText(detail, style = Type.tiny.copy(color = p.muted))
        }
    }
}

@Composable
private fun TabRename(model: AppModel, w: WindowInfo, done: () -> Unit) {
    val p = LocalPalette.current
    val old = w.label.ifEmpty { model.titleParts(w).first }
    // The old name selected: typing replaces it.
    var field by remember { mutableStateOf(TextFieldValue(old, TextRange(0, old.length))) }
    val focus = remember { FocusRequester() }
    LaunchedEffect(Unit) { focus.requestFocus() }
    fun save() { model.renameTab(w, field.text); done() }
    Sheet(onDismiss = done) {
        BasicText("Rename $old", style = Type.title.copy(color = p.text))
        Spacer(Modifier.height(12.dp))
        Box(Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(p.bg).padding(horizontal = 12.dp, vertical = 12.dp)) {
            BasicTextField(field, { field = it }, singleLine = true, textStyle = Type.body.copy(color = p.text), cursorBrush = SolidColor(p.honey),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                keyboardActions = KeyboardActions(onDone = { save() }),
                modifier = Modifier.fillMaxWidth().focusRequester(focus).semantics { contentDescription = "Tab name" })
        }
        BasicText("Empty: named by what runs in it.", style = Type.tiny.copy(color = p.muted), modifier = Modifier.padding(top = 6.dp))
        Spacer(Modifier.height(14.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Pill("Save", primary = true, modifier = Modifier.semantics { contentDescription = "Save the tab name" }) { save() }
            Pill("Cancel", onClick = done)
        }
    }
}
