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
import androidx.compose.foundation.layout.fillMaxHeight
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
import androidx.compose.foundation.layout.isImeVisible
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.layout.heightIn
import androidx.compose.ui.semantics.onClick
import androidx.compose.ui.semantics.role
import androidx.compose.foundation.gestures.waitForUpOrCancellation
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.runtime.key
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

/** Modifier latches on the key bar: off, once (the next key), locked (until tapped off). */
enum class Latch { Off, Once, Locked }

/**
 * C Desk: the Mac app, sized to the screen. The title chip, tabs as pills, the pane (or a split's
 * panes side by side when there's room), the key bar. One layout for phones and tablets: width and
 * the keyboard decide what shows, never a separate tablet screen.
 */
@Composable
@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
fun Desk(model: AppModel, openSwitcher: () -> Unit, openLink: (String) -> Unit, openScript: (xyz.matm.hive.model.HostConnection, String) -> Unit = { _, _ -> },
         addHost: () -> Unit = {}, settings: () -> Unit = {}, hosts: () -> Unit = {}) {
    val p = LocalPalette.current
    androidx.compose.runtime.SideEffect { model.setDark(p.night) }
    val h = model.current
    val conf = androidx.compose.ui.platform.LocalConfiguration.current
    val space = Space.of(conf.screenWidthDp.toFloat())
    // What the device is, not how it's held: a phone on its side stays a phone (it doesn't take panes on use).
    val tablet = conf.smallestScreenWidthDp >= 600
    val device = if (tablet) "tablet" else "phone"
    androidx.compose.runtime.SideEffect { model.wide = tablet }
    val imeUp = WindowInsets.isImeVisible
    val views = remember { androidx.compose.runtime.mutableStateMapOf<String, TerminalView>() }
    var ctrl by remember { mutableStateOf(Latch.Off) }
    var alt by remember { mutableStateOf(Latch.Off) }
    var ctrlTap by remember { androidx.compose.runtime.mutableLongStateOf(0L) }
    var altTap by remember { androidx.compose.runtime.mutableLongStateOf(0L) }
    var confirmPaste by remember { mutableStateOf<Pair<Int, () -> Unit>?>(null) }
    var more by remember { mutableStateOf(false) }
    val block = model.visibleBlock?.let { model.blocks[it] }
    val focusedView = model.visibleBlock?.let { views[it] }
    // The keyboard went away by itself (Back, the system's gesture): the pane stops asking for it.
    androidx.compose.runtime.LaunchedEffect(imeUp) { if (!imeUp) views.values.forEach { it.keyboardGone() } }
    // Focus follows the focused pane, so a hardware keyboard types there; the on-screen keyboard moves with it.
    androidx.compose.runtime.LaunchedEffect(focusedView) {
        val v = focusedView ?: return@LaunchedEffect
        val wanted = views.values.any { it !== v && it.keyboardWanted }
        views.values.filter { it !== v && it.keyboardWanted }.forEach { it.keyboardGone() }
        if (wanted) v.showKeyboard() else v.takeFocus()
    }
    val mods = { listOfNotNull("ctrl".takeIf { ctrl != Latch.Off }, "alt".takeIf { alt != Latch.Off }).joinToString("+") }
    val modsUsed = { if (ctrl == Latch.Once) ctrl = Latch.Off; if (alt == Latch.Once) alt = Latch.Off }
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val focusManager = androidx.compose.ui.platform.LocalFocusManager.current
    val doPaste = { paste(ctx, model.stream, { lines, go -> confirmPaste = lines to go }, model.confirmPaste) }

    var sheet by remember { mutableStateOf(false) }
    var menu by remember { mutableStateOf<xyz.matm.hive.model.WindowInfo?>(null) }
    // The sessions rail, on a screen wide enough to keep 900 dp of terminal beside it (a big tablet in
    // landscape): open or not is remembered; it steps aside for the keyboard. Narrower, the switcher.
    var railOpen by androidx.compose.runtime.saveable.rememberSaveable { mutableStateOf(false) }
    val railRoom = conf.screenWidthDp >= 1200
    val rail = railRoom && railOpen && !imeUp
    androidx.activity.compose.BackHandler(enabled = sheet || menu != null || more) { if (menu != null) menu = null else if (more) more = false else sheet = false }
    // The file viewer: Back goes up to its folder, then closes it.
    val viewing = model.files.shown != null
    // Registered after the sheets' handler, so it runs first: only when none of them is open.
    androidx.activity.compose.BackHandler(enabled = viewing && !sheet && menu == null && !more) { if (!model.files.goBack()) model.files.close() }
    // Closed: the path it showed is no longer lit.
    androidx.compose.runtime.LaunchedEffect(viewing) { if (!viewing) views.values.forEach { it.lit = null } }
    // A tap on what only looked like a path: that pane goes back to lighting what's shown, if anything.
    androidx.compose.runtime.LaunchedEffect(model.files.misses) { views.values.forEach { it.unlight() } }
    // Back online: a file that couldn't come because of it tries again.
    androidx.compose.runtime.LaunchedEffect(h?.link) { if (h != null && h.link == Link.Connected) model.files.reconnected(h) }
    // Open in…: the fetched copy goes to an app that opens its type.
    model.files.handOff?.let { (file, type) ->
        androidx.compose.runtime.LaunchedEffect(file) {
            model.files.handOff = null
            openIn(ctx, file, type)
        }
    }
    val panel = space == Space.Expanded
    Box(Modifier.fillMaxSize()) {
    Row(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding().imePadding()) {
        AnimatedVisibility(rail, enter = androidx.compose.animation.expandHorizontally(tween(220, easing = Quiet)) + fadeIn(tween(220, easing = Quiet)),
            exit = androidx.compose.animation.shrinkHorizontally(tween(200, easing = Quiet)) + fadeOut(tween(160, easing = Quiet))) {
            Row(Modifier.width(300.dp).fillMaxHeight()) {
                Switcher(model, close = { railOpen = false }, addHost = addHost, settings = settings, hosts = hosts, rail = true)
                Box(Modifier.width(1.dp).fillMaxHeight().background(p.line))
            }
        }
    Column(Modifier.weight(1f).fillMaxHeight()) {
        // The title chip: session and host; the switcher behind it (the rail, on a wide screen).
        Row(Modifier.fillMaxWidth().padding(start = 12.dp, end = 12.dp, top = 6.dp, bottom = 4.dp), verticalAlignment = Alignment.CenterVertically) {
            if (railRoom && !railOpen && !imeUp) {
                Box(
                    Modifier.size(40.dp).clip(CircleShape).clickable(role = Role.Button) { railOpen = true }.semantics { contentDescription = "Show sessions" },
                    contentAlignment = Alignment.Center,
                ) { LineIcon(Icon.Sidebar, p.muted, 20.dp) }
                Spacer(Modifier.width(4.dp))
            }
            Row(
                Modifier.weight(1f).clip(RoundedCornerShape(10.dp)).clickable(role = Role.Button, onClick = if (railRoom && !imeUp) ({ railOpen = !railOpen }) else openSwitcher)
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
                    FadeText(sub, Type.tiny, if (h != null && h.link != Link.Connected && h.link != Link.Idle) p.honeyText else p.muted)
                }
                Spacer(Modifier.width(4.dp))
                LineIcon(Icon.Chevron, p.muted, 16.dp)
            }
            if (model.stream != null) {
                val back = model.previousHolder?.let(model::sizeHolderShort)
                // The size it has says itself; the button only offers the other. Quiet: it's not the screen's main thing.
                Pill(if (!model.fitted) "Fit to $device" else if (back != null) "Back to $back" else "Give back") {
                    if (model.fitted) model.unfit() else model.fit()
                }
            }
            Spacer(Modifier.width(4.dp))
            Box(
                Modifier.size(40.dp).clip(CircleShape).clickable(role = Role.Button) { model.newTab() }
                    .semantics { contentDescription = "New tab" },
                contentAlignment = Alignment.Center,
            ) { LineIcon(Icon.Plus, p.muted, 20.dp) }
        }

        // Tabs, with their agents' state: pins first; with many, the count opens them all. Not with
        // the keyboard up: then the terminal needs every row.
        if (model.windows.size > 1 && !imeUp) TabStrip(model, openSheet = { sheet = true }, openMenu = { menu = it }, all = space != Space.Compact)

        // The panes: a split side by side when every pane gets a usable size, else one at a time.
        val window = model.windows.getOrNull(model.windowIndex)
        androidx.compose.foundation.layout.BoxWithConstraints(Modifier.weight(1f).fillMaxWidth().padding(horizontal = 8.dp, vertical = 6.dp)) {
            val layout = window?.layout
            // Judged on the height without the keyboard, so it coming up doesn't regroup the panes.
            val roomH = if (imeUp) (conf.screenHeightDp * 0.55f).dp else maxHeight
            val side = space != Space.Compact && layout is xyz.matm.hive.model.Layout.Split && fitsSideBySide(layout, maxWidth.value, roomH.value, model.textSp)
            androidx.compose.runtime.SideEffect { model.showSideBySide(side) }
            if (side && layout != null) {
                LayoutPanes(layout, model, views, openLink, openScript, mods, modsUsed, doPaste)
            } else if (block != null) {
                PaneBox(model, block.id, focused = true, single = true, views, openLink, openScript, mods, modsUsed, doPaste, Modifier.fillMaxSize())
            }
            if (model.currentSession == null) EmptyDesk(model, openSwitcher)
            // Away: the Mac's card, over the pane, while its host can't be reached.
            Box(Modifier.align(Alignment.Center)) { Away(h, show = h != null && model.currentSession != null && h.link != Link.Connected, connect = { h?.let(model::connect) }) }
            // No pane header to show it in: a script acted elsewhere, so the chip floats here, over
            // the empty desk.
            val chip = if (block == null) model.chipElsewhere()?.let { it.copy(text = "${it.text} in ${it.place}") } else null
            var lastChip by remember { mutableStateOf(chip) }
            if (chip != null) lastChip = chip
            // Over the pane's header, on the desk's own colour: nothing of the pane peeks around it.
            model.finding?.let { f -> Box(Modifier.align(Alignment.TopCenter).fillMaxWidth().background(p.bg).padding(top = 6.dp, bottom = 6.dp, start = 10.dp, end = 10.dp)) { FindBar(model, f) } }
            Box(Modifier.align(Alignment.TopCenter).padding(top = 12.dp, start = 12.dp, end = 12.dp)) {
                androidx.compose.animation.AnimatedVisibility(visible = chip != null, enter = fadeIn(tween(180, easing = Quiet)), exit = fadeOut(tween(300, easing = Quiet))) {
                    lastChip?.let { c -> ActedChipView(c.text, c.refused) { model.hosts.firstOrNull { it.id == c.host }?.let { openScript(it, c.script) } } }
                }
            }
        }

        // The dots: one pane of a split at a time; swipe sideways between them.
        val panes = window?.blocks?.size ?: 0
        if (panes > 1 && !model.sideBySide) {
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

        // Who holds the pane's size: the device you use sizes it. One pane on screen and the keyboard down.
        val frame = model.frame
        val room = model.pane?.room
        if (model.stream != null && block != null && !model.sideBySide && !imeUp) {
            val holder = model.sizeHolder(block.sizeOwner)
            val zoomed = !model.fitted && frame != null && room != null && frame.cols > room[0] + 4
            val line = when {
                holder == null -> "Nobody holds this pane's size."
                holder == "this phone" -> "Sized by this $device."
                else -> "Sized by $holder."
            } + if (zoomed && !model.sizeHintSeen) " Zoomed to fit: pinch to read closer, or fit it here." else ""
            BasicText(line, style = Type.tiny.copy(color = p.muted), modifier = Modifier.padding(horizontal = 16.dp, vertical = 2.dp))
        }

        confirmPaste?.let { (lines, go) -> PasteCard(lines, paste = { go(); confirmPaste = null }, cancel = { confirmPaste = null }) }

        val ask = remember(frame, block?.agentState) {
            if (frame != null && block?.agentState == "blocked") Ask.find((0 until frame.rows).map(frame::rowText), frame.cols) else null
        }
        AnimatedVisibility(more, enter = androidx.compose.animation.expandVertically(tween(200, easing = Quiet)) + fadeIn(tween(200, easing = Quiet)),
            exit = androidx.compose.animation.shrinkVertically(tween(180, easing = Quiet)) + fadeOut(tween(140, easing = Quiet))) {
            MoreKeys(model, mods, modsUsed, find = { more = false; model.visibleBlock?.let(model::startFind) })
        }
        KeyBar(model, ask, space, ctrl, alt, imeUp, more,
            onCtrl = { ctrl = latch(ctrl, ctrlTap); ctrlTap = System.currentTimeMillis() },
            onAlt = { alt = latch(alt, altTap); altTap = System.currentTimeMillis() },
            // Up for any field (a reply too), the key puts it away; down, it brings it up for the pane.
            onKeyboard = { if (imeUp) { focusManager.clearFocus(); focusedView?.hideKeyboard() } else focusedView?.showKeyboard() },
            onMore = { more = !more },
            onPaste = doPaste, mods = mods, modsUsed = modsUsed)
    }
        // On a wide screen the file opens beside the panes, which narrow to make room.
        AnimatedVisibility(panel && viewing, enter = androidx.compose.animation.expandHorizontally(tween(220, easing = Quiet)) + fadeIn(tween(220, easing = Quiet)),
            exit = androidx.compose.animation.shrinkHorizontally(tween(200, easing = Quiet)) + fadeOut(tween(160, easing = Quiet))) {
            FileView(model.files, panel = true, close = { model.files.close() },
                openEntry = { e -> model.current?.let { model.files.openEntry(it, e) } }, retry = { model.current?.let(model.files::retry) })
        }
    }
    if (sheet) TabSheet(model, close = { sheet = false }, openMenu = { menu = it })
    if (viewing && !panel) FileView(model.files, panel = false, close = { model.files.close() },
        openEntry = { e -> model.current?.let { model.files.openEntry(it, e) } }, retry = { model.current?.let(model.files::retry) })
    // The tab may have closed meanwhile: its menu goes with it.
    menu?.let { m -> model.windows.firstOrNull { it.id == m.id }?.let { w -> TabMenu(model, w) { menu = null } } ?: run { menu = null } }
    }
}

/** A latch's next state: off to once; once to locked when tapped again quickly, else off; locked to off. */
private fun latch(now: Latch, lastTap: Long): Latch = when (now) {
    Latch.Off -> Latch.Once
    Latch.Once -> if (System.currentTimeMillis() - lastTap < 400) Latch.Locked else Latch.Off
    Latch.Locked -> Latch.Off
}

/**
 * Whether every pane of a split gets at least 60 columns and 12 rows at the text size, plus its
 * header: side by side only then, else one at a time. Cell sizes are JetBrains Mono's at [textSp].
 */
fun fitsSideBySide(l: xyz.matm.hive.model.Layout, w: Float, h: Float, textSp: Float): Boolean = when (l) {
    // 60 columns: an agent's output reads; fewer wraps mid-word. 12 rows: a screenful of it.
    is xyz.matm.hive.model.Layout.Leaf -> w >= 60 * textSp * 0.6f + 16 && h >= 12 * textSp * 1.32f + 34 + 8
    is xyz.matm.hive.model.Layout.Split ->
        if (l.horizontal) fitsSideBySide(l.a, w * l.ratio - 4, h, textSp) && fitsSideBySide(l.b, w * (1 - l.ratio) - 4, h, textSp)
        else fitsSideBySide(l.a, w, h * l.ratio - 4, textSp) && fitsSideBySide(l.b, w, h * (1 - l.ratio) - 4, textSp)
}

/** A split's panes as its layout says: beside or above each other, at their shares. */
@Composable
private fun LayoutPanes(l: xyz.matm.hive.model.Layout, model: AppModel, views: MutableMap<String, TerminalView>, openLink: (String) -> Unit,
                        openScript: (xyz.matm.hive.model.HostConnection, String) -> Unit, mods: () -> String, modsUsed: () -> Unit, paste: () -> Unit,
                        modifier: Modifier = Modifier.fillMaxSize()) {
    when (l) {
        is xyz.matm.hive.model.Layout.Leaf -> PaneBox(model, l.block, focused = l.block == model.visibleBlock, single = false, views, openLink, openScript, mods, modsUsed, paste, modifier)
        is xyz.matm.hive.model.Layout.Split -> if (l.horizontal) Row(modifier, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            LayoutPanes(l.a, model, views, openLink, openScript, mods, modsUsed, paste, Modifier.weight(l.ratio).fillMaxHeight())
            LayoutPanes(l.b, model, views, openLink, openScript, mods, modsUsed, paste, Modifier.weight(1 - l.ratio).fillMaxHeight())
        } else Column(modifier, verticalArrangement = Arrangement.spacedBy(8.dp)) {
            LayoutPanes(l.a, model, views, openLink, openScript, mods, modsUsed, paste, Modifier.weight(l.ratio).fillMaxWidth())
            LayoutPanes(l.b, model, views, openLink, openScript, mods, modsUsed, paste, Modifier.weight(1 - l.ratio).fillMaxWidth())
        }
    }
}

/**
 * One pane: its header (program, title, state, badges, a script's chip), its terminal, and under
 * it what its agent asks or why it stopped. The focused one wears honey; tapping another focuses it.
 */
@Composable
private fun PaneBox(model: AppModel, id: String, focused: Boolean, single: Boolean, views: MutableMap<String, TerminalView>, openLink: (String) -> Unit,
                    openScript: (xyz.matm.hive.model.HostConnection, String) -> Unit, mods: () -> String, modsUsed: () -> Unit, paste: () -> Unit,
                    modifier: Modifier) {
    val p = LocalPalette.current
    val block = model.blocks[id] ?: return
    val pane = model.panes[id]
    val frame = pane?.frame
    val ask = remember(frame, block.agentState) {
        if (frame != null && block.agentState == "blocked") Ask.find((0 until frame.rows).map(frame::rowText), frame.cols) else null
    }
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val paneShape = RoundedCornerShape(12.dp)
    val border = when {
        ask != null -> p.coral.copy(alpha = if (focused) 0.8f else 0.45f)
        focused && !single -> p.honey.copy(alpha = 0.85f)
        focused -> p.honey.copy(alpha = 0.55f)
        else -> p.line
    }
    Column(modifier) {
        Box(Modifier.weight(1f).fillMaxWidth().clip(paneShape).border(if (focused && !single) 1.5.dp else 1.dp, border, paneShape)) {
            Column(Modifier.fillMaxSize()) {
                // A fixed height: a badge or a chip showing never resizes the terminal. It follows
                // the text size setting (34 dp at the default), so large text isn't clipped.
                val headerHeight = with(androidx.compose.ui.platform.LocalDensity.current) {
                    maxOf(Type.tiny.lineHeight.toDp() + 18.dp, Type.small.lineHeight.toDp() + 14.dp)
                }
                // The focused pane's header on the surface; another's sinks toward the page, so focus reads at a glance.
                Row(Modifier.fillMaxWidth().background(if (focused || single) p.surface else p.surface.copy(alpha = 0.45f)).height(headerHeight)
                    .then(if (single) Modifier else Modifier.clickable(role = Role.Button, onClickLabel = "Focus this pane") { model.focusPane(id) })
                    .padding(horizontal = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                    // The Mac's pane header: the program's mark, the title and where, the state.
                    val (title, where) = block.titleParts
                    ProgramGlyph(block.program, if (focused && !single) p.honeyText else p.text.copy(alpha = if (focused) 0.9f else 0.55f), 14.dp)
                    Spacer(Modifier.width(8.dp))
                    // A script just acted (here, or in another session or host): the chip takes
                    // the title's place for a few seconds, so it covers none of the terminal.
                    val chip = model.chipFor(id) ?: (if (focused) model.chipElsewhere()?.let { it.copy(text = "${it.text} in ${it.place}") } else null)
                    androidx.compose.animation.Crossfade(chip, Modifier.weight(1f).fillMaxHeight(), animationSpec = tween(220, easing = Quiet), label = "acted") { c ->
                        Row(Modifier.fillMaxHeight(), verticalAlignment = Alignment.CenterVertically) {
                            if (c != null) ActedChipView(c.text, c.refused, compact = true) { model.hosts.firstOrNull { it.id == c.host }?.let { openScript(it, c.script) } }
                            else {
                                // The title takes the room and fades; the folder stays short beside it.
                                FadeText(title, Type.small.copy(fontWeight = androidx.compose.ui.text.font.FontWeight(500)), if (focused) p.text else p.muted, Modifier.weight(1f, fill = false))
                                if (where.isNotEmpty()) { Spacer(Modifier.width(8.dp)); FadeText(where, Type.small, p.muted, Modifier.widthIn(max = 110.dp)) }
                            }
                        }
                    }
                    if (block.agentState in setOf("working", "blocked", "done", "stopped")) { Spacer(Modifier.width(8.dp)); StateBadge(block.agentState!!) }
                    // Scripts' badges: the first two, those that need you first. They step aside
                    // while a chip shows, so its few words have the room.
                    val badges = if (chip != null) emptyList() else model.paneBadges.badges(id)
                    for (b in badges.take(2)) { Spacer(Modifier.width(6.dp)); ScriptBadgeChip(b) }
                    if (badges.size > 2) { Spacer(Modifier.width(6.dp)); BasicText("+${badges.size - 2}", style = Type.tiny.copy(color = LocalPalette.current.muted)) }
                }
                key(id) {
                    androidx.compose.runtime.DisposableEffect(id) { onDispose { views.remove(id) } }
                    AndroidView(
                        factory = { c -> TerminalView(c).also { views[id] = it } },
                        modifier = Modifier.weight(1f).fillMaxWidth(),
                        update = { v ->
                            views[id] = v
                            v.theme = model.termTheme
                            v.textSp = model.textSp
                            v.selectionColor = (if (p.night) 0x59000000 else 0x47000000) or 0xF2A93B
                            v.handleColor = 0xFF000000.toInt() or (if (p.night) 0xF2A93B else 0xD98A12)
                            v.stream = pane?.stream
                            v.fitted = pane?.fitted == true
                            v.show(frame)
                            v.onPhoneSize = { c, r, cw, ch -> model.paneRoom(id, c, r, cw, ch) }
                            v.onLink = openLink
                            v.onPath = { path -> model.openFile(id, path) }
                            v.litColor = (if (p.night) 0x38000000 else 0x2E000000) or (if (p.night) 0xF2A93B else 0xD98A12)
                            v.onSwipe = if (single) ({ d -> model.selectPane(model.paneIndex + d) }) else null
                            v.onPinched = { model.sizeLearned() }
                            v.onUsed = { model.focusPane(id); model.used(id) }
                            v.onTwoFingerTap = { v.toggleKeyboard() }
                            v.onFocus = { model.focusPane(id) }
                            v.mods = mods
                            v.onModsUsed = modsUsed
                            v.onCopy = { text ->
                                ctx.getSystemService(android.content.ClipboardManager::class.java)
                                    ?.setPrimaryClip(android.content.ClipData.newPlainText("hive", text))
                            }
                            v.onPaste = paste
                        },
                    )
                }
            }
        }
        if (ask != null) AskCard(ask, Harness.name(block.harness ?: ""), model, id)
        // A stopped agent says why, where you'd answer one that asks.
        if (block.agentState == "stopped") model.current?.agents?.firstOrNull { it.blockID == id }?.let { StoppedCard(Harness.name(it.harness), it.message) }
    }
}

/** The Mac's StatusBadge: "Blocked" or "Stopped" in words, else the state's hexagon. */
@Composable
fun StateBadge(state: String) {
    val p = LocalPalette.current
    if (state == "blocked" || state == "stopped") {
        Row(Modifier.clip(RoundedCornerShape(50)).background(p.coral.copy(alpha = 0.14f)).padding(horizontal = 7.dp, vertical = 2.dp),
            verticalAlignment = Alignment.CenterVertically) {
            Hexagon(state, 7.dp)
            Spacer(Modifier.width(4.dp))
            BasicText(if (state == "blocked") "Blocked" else "Stopped", style = Type.tiny.copy(color = p.coralText, fontWeight = androidx.compose.ui.text.font.FontWeight(600)))
        }
    } else Hexagon(state, 8.dp)
}

/** A script's badge: a hexagon in its state's color and its message, fading when long. */
@Composable
fun ScriptBadgeChip(b: xyz.matm.hive.model.ScriptBadge) {
    val p = LocalPalette.current
    val state = when (b.state) { "error" -> "blocked"; else -> b.state }
    val color = when (state) { "blocked" -> p.coral; "done" -> p.sage; "working" -> p.honey; else -> p.muted }
    // Its words in the state's text colour, which reads on the tint in Day too.
    val ink = when (state) { "blocked" -> p.coralText; "done" -> p.sageText; "working" -> p.honeyText; else -> p.muted }
    Row(Modifier.clip(RoundedCornerShape(50)).background(color.copy(alpha = 0.13f)).padding(horizontal = 7.dp, vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically) {
        Hexagon(state, 7.dp)
        Spacer(Modifier.width(4.dp))
        FadeText(b.progress?.let { "${b.text} $it%" } ?: b.text, Type.tiny.copy(fontWeight = androidx.compose.ui.text.font.FontWeight(500)), ink, Modifier.widthIn(max = 96.dp))
    }
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
            if (model.hosts.isEmpty()) "A hive server on your tailnet. The phone reaches it through the Tailscale app, as your computer does."
            else "Your hosts' sessions are in the switcher.",
            style = Type.small.copy(color = p.muted), modifier = Modifier.widthIn(max = 300.dp),
        )
        Spacer(Modifier.height(18.dp))
        Pill(if (model.hosts.isEmpty()) "Add a host" else "Open the switcher", primary = true, onClick = openSwitcher)
    }
}

@Composable
private fun Away(h: xyz.matm.hive.model.HostConnection?, show: Boolean, connect: () -> Unit) {
    AnimatedVisibility(show, enter = fadeIn(tween(200, easing = Quiet)), exit = fadeOut(tween(200, easing = Quiet))) {
        if (h == null) return@AnimatedVisibility
        // Disconnected by hand: say so, and the way back.
        if (h.link == Link.Idle) AwayCard("Disconnected from ${h.name}", "Its sessions keep running on the server.", refused = false, action = "Connect" to connect, state = "idle")
        else AwayCard(h.problem ?: "", h.detail, refused = h.link is Link.Refused)
    }
}

@Composable
fun AwayCard(title: String, detail: String?, refused: Boolean, action: Pair<String, () -> Unit>? = null, state: String? = null) {
    val p = LocalPalette.current
    Column(
        Modifier.padding(24.dp).widthIn(max = 320.dp).clip(RoundedCornerShape(12.dp)).background(p.surface)
            .border(1.dp, p.line, RoundedCornerShape(12.dp)).padding(16.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Hexagon(state ?: if (refused) "blocked" else "working", 11.dp)
            Spacer(Modifier.width(10.dp))
            BasicText(title.removeSuffix("…"), style = Type.label.copy(color = p.text))
        }
        if (detail != null) {
            Spacer(Modifier.height(6.dp))
            BasicText(detail, style = Type.small.copy(color = p.muted))
        }
        if (action != null) {
            Spacer(Modifier.height(12.dp))
            Pill(action.first, primary = true, onClick = action.second)
        }
    }
}

/** A from A Inbox: the choices a blocked agent offers, lifted into buttons; or words for it. */
@Composable
private fun AskCard(ask: Ask, agent: String, model: AppModel, block: String) {
    val p = LocalPalette.current
    var reply by remember(ask.question) { mutableStateOf("") }
    Column(
        Modifier.fillMaxWidth().padding(top = 6.dp).clip(RoundedCornerShape(12.dp))
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
                Pill(c.short, primary = i == 0, modifier = Modifier.heightIn(min = 44.dp).semantics { contentDescription = c.label }) { model.answer(c.key, block) }
            }
        }
        Spacer(Modifier.height(10.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.weight(1f).clip(RoundedCornerShape(50)).background(p.surface).padding(horizontal = 14.dp, vertical = 10.dp)) {
                if (reply.isEmpty()) BasicText("Tell ${agent.removeSuffix(" Code")} what to do", style = Type.small.copy(color = p.muted))
                BasicTextField(
                    reply, { reply = it }, singleLine = true, textStyle = Type.small.copy(color = p.text), cursorBrush = SolidColor(p.honey),
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
                    keyboardActions = KeyboardActions(onSend = { if (reply.isNotBlank()) { model.reply(reply, block); reply = "" } }),
                    modifier = Modifier.fillMaxWidth().semantics { contentDescription = "Reply" },
                )
            }
            Spacer(Modifier.width(8.dp))
            Box(
                Modifier.size(40.dp).clip(CircleShape).background(if (reply.isBlank()) p.raised else p.honey)
                    .clickable(enabled = reply.isNotBlank(), role = Role.Button) { model.reply(reply, block); reply = "" }
                    .semantics { contentDescription = "Send reply" },
                contentAlignment = Alignment.Center,
            ) { LineIcon(Icon.Send, if (reply.isBlank()) p.muted else p.onHoney, 18.dp) }
        }
    }
}

/** An agent that failed and stopped (a usage limit, a crash): it won't go on by itself. */
@Composable
private fun StoppedCard(agent: String, why: String) {
    val p = LocalPalette.current
    Column(
        Modifier.fillMaxWidth().padding(top = 6.dp).clip(RoundedCornerShape(12.dp))
            .background(p.coral.copy(alpha = if (p.night) 0.10f else 0.08f)).border(1.dp, p.coral.copy(alpha = 0.35f), RoundedCornerShape(12.dp))
            .padding(horizontal = 12.dp, vertical = 10.dp).semantics(mergeDescendants = true) {},
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Hexagon("stopped", 9.dp)
            Spacer(Modifier.width(8.dp))
            BasicText("$agent stopped", style = Type.label.copy(color = p.text))
        }
        if (why.isNotBlank()) {
            Spacer(Modifier.height(4.dp))
            BasicText(why, style = Type.small.copy(color = p.muted), maxLines = 3)
        }
    }
}

/** A key on the bar or in the more panel: what it shows, what TalkBack says, what it sends (a key name or text). */
private data class K(val label: String, val describe: String, val name: String? = null, val text: String? = null, val repeat: Boolean = false)

/** Sends a key with the latched modifiers: names go to the server, which encodes them for the program's modes. */
private fun send(model: AppModel, k: K, mods: () -> String, modsUsed: () -> Unit) {
    val s = model.stream ?: return
    model.visibleBlock?.let { model.used(it) }
    val m = mods()
    val key = k.name ?: k.text?.let { t -> if (m.isNotEmpty() && t.length == 1) t else null }
    when {
        key != null && m.isNotEmpty() -> { s.key("$m+$key"); modsUsed() }
        k.name != null -> s.key(k.name)
        k.text != null -> s.text(k.text)
    }
}

private val ESC = K("esc", "Escape", "escape")
private val TAB = K("tab", "Tab", "tab")
private val BACKTAB = K("⇧tab", "Shift Tab", "shift+tab")
private val UP = K("↑", "Up", "up", repeat = true)
private val DOWN = K("↓", "Down", "down", repeat = true)
private val LEFT = K("←", "Left", "left", repeat = true)
private val RIGHT = K("→", "Right", "right", repeat = true)
private val CTRL_C = K("^C", "Control C", "ctrl+c")
private val HOME = K("home", "Home", "home")
private val END = K("end", "End", "end")
private val PGUP = K("pgup", "Page up", "pageup", repeat = true)
private val PGDN = K("pgdn", "Page down", "pagedown", repeat = true)
private fun sym(c: String) = K(c, c, text = c)

/**
 * The keys a wide bar adds, in order of use with an agent: interrupt, slash commands and mentions,
 * moving around, then the rest. Whatever doesn't fit is in More.
 */
private val ENTER = K("↵", "Return", "enter")
private val EXTRA = listOf(CTRL_C, sym("/"), sym("@"), HOME, END, PGUP, PGDN, sym("~"), sym("-"),
    K("^D", "Control D", "ctrl+d"), K("^Z", "Control Z", "ctrl+z"), K("^R", "Control R", "ctrl+r"), K("^L", "Control L", "ctrl+l"))

/**
 * The key bar: what a touch keyboard lacks. A phone has two rows, the arrows as an inverted T
 * (as Termux has them), so every key shows without scrolling; a wider screen has one row with
 * more keys, by priority. ctrl and alt latch for the next key, or lock with a second tap. The
 * agent's own answers replace it while one asks.
 */
@Composable
private fun KeyBar(model: AppModel, ask: Ask?, space: Space, ctrl: Latch, alt: Latch, imeUp: Boolean, more: Boolean,
                   onCtrl: () -> Unit, onAlt: () -> Unit, onKeyboard: () -> Unit, onMore: () -> Unit, onPaste: () -> Unit,
                   mods: () -> String, modsUsed: () -> Unit) {
    val p = LocalPalette.current
    val enabled = model.stream != null
    // 42 dp keys with 6 dp between: a 48 dp pitch for thumbs.
    val keyH = 42.dp
    @Composable
    fun Key(k: K, modifier: Modifier = Modifier) = BarKey(k.label, k.describe, enabled, modifier.height(keyH), repeat = k.repeat) { send(model, k, mods, modsUsed) }
    @Composable
    fun Mod(label: String, describe: String, l: Latch, onTap: () -> Unit, modifier: Modifier = Modifier) =
        BarKey(label, describe + when (l) { Latch.Off -> ""; Latch.Once -> ", on for the next key"; Latch.Locked -> ", locked" }, enabled,
            modifier.height(keyH), latch = l, onTap = onTap)
    @Composable
    fun Icon(icon: Icon, describe: String, on: Boolean, modifier: Modifier = Modifier, onTap: () -> Unit) {
        Box(
            modifier.height(keyH).clip(RoundedCornerShape(10.dp)).background(if (on) p.honey.copy(alpha = if (p.night) 0.18f else 0.30f) else p.raised)
                .clickable(enabled = enabled, role = Role.Button, onClick = onTap).semantics { contentDescription = describe },
            contentAlignment = Alignment.Center,
        ) { LineIcon(icon, if (on) p.honeyText else p.text, 20.dp) }
    }
    val kb = if (imeUp) Icon.KeyboardHide else Icon.Keyboard
    val kbSay = if (imeUp) "Hide keyboard" else "Keyboard"
    @Composable
    fun Answer(c: xyz.matm.hive.agents.Choice, modifier: Modifier) =
        BarKey(c.key, "Answer ${c.key}", enabled, modifier.height(keyH), latch = null) { model.answer(c.key) }
    Column(Modifier.fillMaxWidth().background(p.surface).padding(horizontal = 8.dp, vertical = 6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        androidx.compose.foundation.layout.BoxWithConstraints(Modifier.fillMaxWidth()) {
        // By the bar's own width, not the screen's: beside the rail a tablet's bar is a phone's.
        if (maxWidth < 720.dp) Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            // Two rows of eight: the arrows an inverted T in the middle, the keyboard key at the end.
            // An agent's question takes the top row's first keys for its answers; the rest stays put.
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(5.dp)) {
                val w = Modifier.weight(1f)
                if (ask != null) {
                    for (c in ask.choices.take(4)) Answer(c, w)
                    Key(ESC, w); Key(CTRL_C, w); Key(UP, w)
                } else {
                    Key(ESC, w); Key(CTRL_C, w); Key(BACKTAB, w); Key(HOME, w); Key(UP, w); Key(END, w)
                    BarKey("paste", "Paste", enabled, w.height(keyH), onTap = onPaste)
                }
                Icon(Icon.More, if (more) "Fewer keys" else "More keys", more, w, onMore)
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(5.dp)) {
                val w = Modifier.weight(1f)
                Key(TAB, w); Mod("ctrl", "Control", ctrl, onCtrl, w); Mod("alt", "Alt", alt, onAlt, w)
                Key(LEFT, w); Key(DOWN, w); Key(RIGHT, w); Key(ENTER, w)
                Icon(kb, kbSay, imeUp, w, onKeyboard)
            }
        } else {
            // One row: the essentials, then as many extras as fit, by priority; More and the keyboard at the end.
            // Each key is 44 dp and a 6 dp gap; More, the keyboard and the group gaps take 148.
            val unit = 50f
            val room = ((maxWidth.value - 148f) / unit).toInt() - 11
            val extras = EXTRA.take(room.coerceIn(0, EXTRA.size))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                val w = Modifier.widthIn(min = 44.dp)
                if (ask != null) {
                    // The answers first, then what leaves or confirms, beside the keyboard key.
                    for (c in ask.choices) Answer(c, Modifier.widthIn(min = 52.dp))
                    Spacer(Modifier.weight(1f))
                    Key(ESC, w); Key(ENTER, w); Key(CTRL_C, w); Key(UP, w); Key(DOWN, w)
                } else {
                    Key(ESC, w); Key(TAB, w); Key(BACKTAB, w)
                    Mod("ctrl", "Control", ctrl, onCtrl, w); Mod("alt", "Alt", alt, onAlt, w)
                    Spacer(Modifier.width(4.dp))
                    Key(LEFT, w); Key(UP, w); Key(DOWN, w); Key(RIGHT, w); Key(ENTER, w)
                    Spacer(Modifier.width(4.dp))
                    BarKey("paste", "Paste", enabled, w.height(keyH), onTap = onPaste)
                    for (k in extras) Key(k, w)
                    Spacer(Modifier.weight(1f))
                }
                Icon(Icon.More, if (more) "Fewer keys" else "More keys", more, Modifier.width(52.dp), onMore)
                Icon(kb, kbSay, imeUp, Modifier.width(52.dp), onKeyboard)
            }
        }
        }
    }
}

/**
 * One key. [latch]: a modifier's look, outlined when on for the next key, filled when locked.
 * [repeat]: held, it sends again after 350 ms, every 90 ms, then every 45 ms after a second.
 */
@Composable
private fun BarKey(label: String, describe: String, enabled: Boolean, modifier: Modifier, latch: Latch? = null, repeat: Boolean = false, onTap: () -> Unit) {
    val p = LocalPalette.current
    val shape = RoundedCornerShape(10.dp)
    val bg = when (latch) { Latch.Locked -> p.honey; else -> p.raised }
    val fg = when (latch) { Latch.Locked -> p.onHoney; Latch.Once -> p.honeyText; else -> p.text }
    var pressed by remember { mutableStateOf(false) }
    val tap = androidx.compose.runtime.rememberUpdatedState(onTap)
    val press = if (repeat) Modifier.pointerInput(enabled) {
        if (!enabled) return@pointerInput
        awaitEachGesture {
            awaitFirstDown()
            pressed = true
            tap.value()
            val start = System.currentTimeMillis()
            var next = start + 350
            while (true) {
                // Lifted (or the gesture went elsewhere) before the next repeat: done. Else send again.
                val ended = withTimeoutOrNull(maxOf(1L, next - System.currentTimeMillis())) { waitForUpOrCancellation(); true }
                if (ended != null) break
                tap.value()
                next += if (System.currentTimeMillis() - start > 1350) 45 else 90
            }
            pressed = false
        }
    }.semantics { contentDescription = describe; role = Role.Button; onClick { tap.value(); true } }
    else Modifier.clickable(enabled = enabled, role = Role.Button, onClick = onTap).semantics { contentDescription = describe }
    Box(
        modifier.clip(shape).background(if (pressed) p.honey.copy(alpha = 0.25f) else bg)
            .then(if (latch == Latch.Once) Modifier.border(1.5.dp, p.honey, shape) else Modifier)
            .then(press).padding(horizontal = 3.dp),
        contentAlignment = Alignment.Center,
    ) {
        // Longer words a little smaller, so "home" and "paste" fit a phone's eighth of the width whole.
        // Two sizes: a glyph or two (arrows, ^C, F5) at 15 sp, words at 13 sp.
        val size = if (label.length <= 2 || label.startsWith("F")) 15.sp else 13.sp
        BasicText(label, style = Type.mono.copy(fontFamily = LocalMono.current, fontSize = size, color = if (enabled) fg else p.muted), maxLines = 1, softWrap = false)
    }
}

/**
 * More keys: what the bar has no room for, grouped, in a grid of eight; open for several in a
 * row. Control first (Claude Code's own shortcuts among them), then symbols, moving, function keys.
 */
@Composable
private fun MoreKeys(model: AppModel, mods: () -> String, modsUsed: () -> Unit, find: () -> Unit) {
    val p = LocalPalette.current
    fun c(l: Char, what: String) = K("^$l", "Control $l, $what", "ctrl+${l.lowercaseChar()}")
    val groups = listOf(
        "Control" to listOf(c('D', "end of input"), c('Z', "suspend"), c('R', "search history"), c('L', "clear"),
            c('O', "transcript"), c('T', "todos"), c('B', "background"), c('G', "editor"),
            c('A', "line start"), c('E', "line end"), c('U', "delete line"), c('W', "delete word"), c('K', "delete to end"), c('J', "new line")),
        "Symbols" to listOf("/", "@", "|", "\\", "~", "`", "-", "_", "=", "+", "[", "]", "{", "}", "<", ">", "'", "\"", ";", ":", "!", "#", "$", "%", "^", "&", "*").map(::sym),
        "Move" to listOf(PGUP, PGDN, K("ins", "Insert", "insert"), K("del", "Delete", "delete")),
        "Function" to (1..12).map { K("F$it", "F$it", "f$it") },
    )
    androidx.compose.foundation.layout.BoxWithConstraints(Modifier.fillMaxWidth().background(p.surface)) {
        // Eight columns on a phone; on a wide bar as many 52 dp keys as fit, all the same width.
        val cols = if (maxWidth < 720.dp) 8 else ((maxWidth.value - 16) / 58f).toInt().coerceAtLeast(8)
        Column(
            Modifier.fillMaxWidth().heightIn(max = 264.dp).verticalScroll(rememberScrollState())
                .padding(horizontal = 8.dp).padding(top = 10.dp, bottom = 12.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            // The pane itself first: find in what it printed.
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(5.dp)) {
                BarKey("find", "Find in this pane", model.stream != null, Modifier.weight(2f).height(42.dp), onTap = find)
                repeat(cols - 2) { Spacer(Modifier.weight(1f)) }
            }
            for ((title, keys) in groups) {
                BasicText(title, style = Type.tiny.copy(color = p.muted, fontWeight = androidx.compose.ui.text.font.FontWeight(500)),
                    modifier = Modifier.padding(start = 4.dp, top = 4.dp))
                for (row in keys.chunked(cols)) Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(5.dp)) {
                    for (k in row) BarKey(k.label, k.describe, model.stream != null, Modifier.weight(1f).height(42.dp), repeat = k.repeat) { send(model, k, mods, modsUsed) }
                    // A short last row keeps the grid's columns.
                    repeat(cols - row.size) { Spacer(Modifier.weight(1f)) }
                }
            }
        }
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

/** Hands a fetched file to an app that opens its type (read-only, through hive's FileProvider). */
private fun openIn(ctx: android.content.Context, file: java.io.File, type: String) {
    val uri = androidx.core.content.FileProvider.getUriForFile(ctx, ctx.packageName + ".files", file)
    val view = android.content.Intent(android.content.Intent.ACTION_VIEW).setDataAndType(uri, type.ifEmpty { "application/octet-stream" })
        .addFlags(android.content.Intent.FLAG_GRANT_READ_URI_PERMISSION)
    val chooser = android.content.Intent.createChooser(view, "Open ${file.name} in")
    try { ctx.startActivity(chooser) } catch (_: android.content.ActivityNotFoundException) {
        android.widget.Toast.makeText(ctx, "No app here opens ${file.name}", android.widget.Toast.LENGTH_SHORT).show()
    }
}
