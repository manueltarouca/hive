package xyz.matm.hive.ui

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.foundation.gestures.detectVerticalDragGestures
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicText
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import xyz.matm.hive.model.FileViewer
import xyz.matm.hive.model.FileViewer.Body

/**
 * A file a pane printed. On a phone, a sheet over the pane (half height; drag
 * it up for more, down to close); on a wide screen, a panel beside the panes
 * ([panel]). Images and text show here; anything else opens in another app.
 */
@Composable
fun FileView(v: FileViewer, panel: Boolean, close: () -> Unit, openEntry: (FileViewer.Entry) -> Unit, retry: () -> Unit) {
    val p = LocalPalette.current
    val shown = v.shown ?: return
    if (panel) {
        Column(
            Modifier.width(360.dp).fillMaxHeight().padding(top = 6.dp, bottom = 6.dp, end = 8.dp)
                .clip(RoundedCornerShape(12.dp)).background(p.surface).border(1.dp, p.line, RoundedCornerShape(12.dp)),
        ) { Contents(v, shown, close, openEntry, retry, grab = null) }
        return
    }
    var full by remember { mutableStateOf(false) }
    var drag by remember { mutableFloatStateOf(0f) }
    Box(Modifier.fillMaxSize()) {
        // Light: the path you tapped stays readable above the sheet.
        Box(Modifier.fillMaxSize().background(Color.Black.copy(alpha = if (p.night) 0.32f else 0.18f))
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = close))
        BoxWithConstraints(Modifier.fillMaxSize(), contentAlignment = Alignment.BottomCenter) {
            val h = if (full) maxHeight * 0.9f else maxHeight * 0.56f
            Column(
                Modifier.fillMaxWidth().widthIn(max = 640.dp).height(h).graphicsLayer { translationY = drag.coerceAtLeast(0f) }
                    .clip(RoundedCornerShape(topStart = 22.dp, topEnd = 22.dp)).background(p.surface)
                    .border(1.dp, p.line, RoundedCornerShape(topStart = 22.dp, topEnd = 22.dp))
                    .navigationBarsPadding().imePadding(),
            ) {
                val grab = Modifier.pointerInput(full) {
                    detectVerticalDragGestures(
                        onDragEnd = {
                            when {
                                drag > 120 && !full -> close()
                                drag > 120 -> full = false
                                drag < -80 -> full = true
                            }
                            drag = 0f
                        },
                        onDragCancel = { drag = 0f },
                    ) { _, d -> drag += d }
                }
                Contents(v, shown, close, openEntry, retry, grab = grab, onGrabTap = { full = !full })
            }
        }
    }
}

@Composable
private fun androidx.compose.foundation.layout.ColumnScope.Contents(v: FileViewer, shown: FileViewer.Shown, close: () -> Unit, openEntry: (FileViewer.Entry) -> Unit, retry: () -> Unit,
                     grab: Modifier?, onGrabTap: () -> Unit = {}) {
    val p = LocalPalette.current
    val meta = shown.meta
    val body = shown.body
    val head = Modifier.then(grab ?: Modifier)
    Column(head) {
        if (grab != null) {
            Box(Modifier.fillMaxWidth().clickable(role = Role.Button, onClick = onGrabTap).semantics { contentDescription = "Resize" }.padding(top = 8.dp, bottom = 2.dp),
                contentAlignment = Alignment.Center) {
                Box(Modifier.size(36.dp, 4.dp).clip(RoundedCornerShape(2.dp)).background(p.muted.copy(alpha = 0.4f)))
            }
        }
        Row(Modifier.fillMaxWidth().padding(start = if (v.canGoBack) 4.dp else 16.dp, end = 6.dp, top = 6.dp, bottom = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            if (v.canGoBack) {
                IconButton(Icon.Back, "Back to the folder") { v.goBack() }
            } else {
                Box(Modifier.size(30.dp).clip(RoundedCornerShape(8.dp)).background(p.bg), contentAlignment = Alignment.Center) {
                    LineIcon(kindIcon(meta, body), p.muted, 16.dp)
                }
                Spacer(Modifier.width(10.dp))
            }
            Column(Modifier.weight(1f)) {
                FadeText(meta?.name ?: shown.asked.substringAfterLast('/').ifEmpty { shown.asked }, Type.label, p.text)
                FadeText(listOfNotNull(meta?.folder ?: shown.asked.substringBeforeLast('/', "").ifEmpty { null }, "on ${shown.hostName}").joinToString(" "), Type.tiny, p.muted)
            }
            if (meta != null && !meta.dir && body !is Body.Failed && body !is Body.Loading) IconButton(Icon.Share, "Open in another app") { v.openIn() }
            IconButton(Icon.Close, "Close") { close() }
        }
    }
    Box(Modifier.weight(1f).fillMaxWidth()) {
        when (body) {
            is Body.Loading -> Centered {
                Progress(body.progress)
                Note("Getting it from ${shown.hostName}")
            }
            is Body.Image -> ImageBody(body)
            is Body.Text -> TextBody(body, meta?.line)
            is Body.Folder -> FolderBody(body, openEntry)
            is Body.Other -> Centered {
                Glyph(Icon.Doc, p.muted)
                BasicText(meta?.name ?: "", style = Type.label.copy(color = p.text))
                Note("${describe(meta)}, ${size(meta?.size ?: -1)}. hive shows images and text; this one opens in another app.")
                if (body.fetching != null) {
                    Progress(body.fetching)
                    Note("${size((body.fetching * (meta?.size ?: 0)).toLong())} of ${size(meta?.size ?: -1)} from ${shown.hostName}")
                    Buttons { Pill("Cancel") { v.cancelFetch() } }
                } else {
                    Buttons { Pill("Open in…", primary = true) { v.openIn() } }
                }
            }
            Body.TooLarge -> Centered {
                Glyph(Icon.Doc, p.muted)
                BasicText(meta?.name ?: "", style = Type.label.copy(color = p.text))
                Note("${size(meta?.size ?: -1)} of text is more than a phone shows well. Read its end here, or open the whole file in another app.")
                Buttons {
                    Pill("Show the last 2 MB", primary = true) { v.showTail() }
                    Pill("Open in…") { v.openIn() }
                }
            }
            is Body.Failed -> Centered {
                when (body.kind) {
                    FileViewer.Fail.Offline -> {
                        Glyph(Icon.Wifi, p.coral)
                        BasicText("${shown.hostName} isn’t answering", style = Type.label.copy(color = p.text))
                        Note("The file opens when the connection is back.")
                        Progress(null)
                        Buttons { Pill("Try again", primary = true, onClick = retry); Pill("Close", onClick = close) }
                    }
                    FileViewer.Fail.NotFound -> {
                        Glyph(Icon.Alert, p.coral)
                        BasicText("Not on ${shown.hostName}", style = Type.label.copy(color = p.text))
                        Note("No file at this path now. It may have moved, or the path was relative to another folder.")
                        Buttons { Pill("Close", onClick = close) }
                    }
                    FileViewer.Fail.Denied -> {
                        Glyph(Icon.Alert, p.coral)
                        BasicText("Can’t read this file", style = Type.label.copy(color = p.text))
                        Note("${shown.hostName} doesn’t let hive read it.")
                        Buttons { Pill("Close", onClick = close) }
                    }
                    FileViewer.Fail.Other -> {
                        Glyph(Icon.Alert, p.coral)
                        BasicText("Can’t show this file", style = Type.label.copy(color = p.text))
                        Note(body.message.ifEmpty { "Something went wrong reading it." })
                        Buttons { Pill("Close", onClick = close) }
                    }
                }
            }
        }
    }
    // What it is, how big: under the content, as a quiet line.
    val foot = footer(meta, body)
    if (foot != null) {
        Box(Modifier.fillMaxWidth().height(1.dp).background(p.line))
        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            foot.first.forEach { BasicText(it, style = Type.tiny.copy(color = p.muted)) }
            Spacer(Modifier.weight(1f))
            BasicText(foot.second, style = Type.tiny.copy(color = p.muted))
        }
    }
}

@Composable
private fun IconButton(icon: Icon, label: String, onClick: () -> Unit) {
    val p = LocalPalette.current
    Box(Modifier.size(40.dp).clip(CircleShape).clickable(role = Role.Button, onClick = onClick).semantics { contentDescription = label },
        contentAlignment = Alignment.Center) { LineIcon(icon, p.muted, 19.dp) }
}

@Composable
private fun Centered(content: @Composable () -> Unit) {
    Column(Modifier.fillMaxSize().padding(horizontal = 26.dp, vertical = 16.dp), horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(6.dp, Alignment.CenterVertically)) { content() }
}

@Composable
private fun Glyph(icon: Icon, color: Color) {
    val p = LocalPalette.current
    Box(Modifier.padding(bottom = 6.dp).size(64.dp).clip(RoundedCornerShape(16.dp)).background(p.bg), contentAlignment = Alignment.Center) {
        LineIcon(icon, color, 30.dp)
    }
}

@Composable
private fun Note(text: String) {
    val p = LocalPalette.current
    BasicText(text, style = Type.small.copy(color = p.muted, textAlign = TextAlign.Center), modifier = Modifier.widthIn(max = 280.dp))
}

@Composable
private fun Buttons(content: @Composable () -> Unit) {
    Row(Modifier.padding(top = 10.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) { content() }
}

/** A thin honey bar: how much has come, or a sliding piece while that isn't known. */
@Composable
private fun Progress(fraction: Float?) {
    val p = LocalPalette.current
    BoxWithConstraints(Modifier.padding(top = 8.dp).width(220.dp).height(4.dp).clip(RoundedCornerShape(2.dp)).background(p.line)) {
        if (fraction != null) {
            Box(Modifier.fillMaxHeight().width(maxWidth * fraction.coerceIn(0f, 1f)).background(p.honey))
        } else {
            val t by rememberInfiniteTransition(label = "indeterminate").animateFloat(-0.3f, 1f,
                infiniteRepeatable(tween(1400, easing = LinearEasing), RepeatMode.Restart), label = "slide")
            Box(Modifier.fillMaxHeight().width(maxWidth * 0.3f).graphicsLayer { translationX = t * size.width / 0.3f }.background(p.honey))
        }
    }
}

@Composable
private fun ImageBody(b: Body.Image) {
    var scale by remember(b) { mutableFloatStateOf(1f) }
    var pan by remember(b) { mutableStateOf(Offset.Zero) }
    Box(Modifier.fillMaxSize().padding(10.dp).pointerInput(b) {
        detectTransformGestures { _, move, zoom, _ ->
            scale = (scale * zoom).coerceIn(1f, 8f)
            pan = if (scale == 1f) Offset.Zero else pan + move
        }
    }, contentAlignment = Alignment.Center) {
        Image(b.bitmap, contentDescription = "The image", contentScale = ContentScale.Fit,
            modifier = Modifier.fillMaxSize().graphicsLayer { scaleX = scale; scaleY = scale; translationX = pan.x; translationY = pan.y })
    }
}

/** Plain text in the terminal's font, with line numbers; the line a path named, lit and in view. */
@Composable
private fun TextBody(b: Body.Text, line: Int?) {
    val p = LocalPalette.current
    val mono = LocalMono.current
    val hscroll = remember(b) { androidx.compose.foundation.ScrollState(0) }
    val target = if (b.tail) null else line
    // The end of a log opens at its last line; a named line opens a few lines above it.
    val first = if (b.tail) (b.lines.size - 1).coerceAtLeast(0) else ((target ?: 1) - 4).coerceAtLeast(0)
    val state = remember(b) { androidx.compose.foundation.lazy.LazyListState(firstVisibleItemIndex = first) }
    val width = if (b.tail) 12.dp else (b.lines.size.toString().length.coerceAtLeast(2) * 8 + 12).dp
    val style = Type.mono.copy(fontFamily = mono, fontSize = 12.sp, lineHeight = 18.sp, color = p.text,
        fontFeatureSettings = "liga 0, calt 0")
    LazyColumn(Modifier.fillMaxSize().background(p.bg).padding(vertical = 6.dp), state = state) {
        items(b.lines.size) { i ->
            val n = i + 1
            Row(Modifier.fillMaxWidth().background(if (n == target) p.honey.copy(alpha = 0.14f) else Color.Transparent)) {
                BasicText(if (b.tail) "" else n.toString(), style = style.copy(color = p.muted, textAlign = TextAlign.End),
                    modifier = Modifier.width(width).padding(end = 10.dp))
                BasicText(b.lines[i].replace("\t", "    ").ifEmpty { " " }, style = style, softWrap = false,
                    modifier = Modifier.horizontalScroll(hscroll))
            }
        }
    }
}

@Composable
private fun FolderBody(b: Body.Folder, openEntry: (FileViewer.Entry) -> Unit) {
    val p = LocalPalette.current
    LazyColumn(Modifier.fillMaxSize().padding(horizontal = 8.dp)) {
        items(b.entries) { e ->
            Row(Modifier.fillMaxWidth().height(44.dp).clip(RoundedCornerShape(10.dp)).clickable(role = Role.Button) { openEntry(e) }.padding(horizontal = 8.dp),
                verticalAlignment = Alignment.CenterVertically) {
                LineIcon(if (e.dir) Icon.Folder else if (isImage(e.name)) Icon.Image else Icon.Doc, p.muted, 18.dp)
                Spacer(Modifier.width(12.dp))
                Box(Modifier.weight(1f)) { FadeText(e.name, Type.body.copy(fontSize = 14.sp), p.text) }
                if (!e.dir) BasicText(size(e.size), style = Type.tiny.copy(color = p.muted))
            }
        }
        if (b.truncated) item { BasicText("The first 500, newest first", style = Type.tiny.copy(color = p.muted), modifier = Modifier.padding(12.dp)) }
    }
}

private fun kindIcon(m: FileViewer.Meta?, b: Body) = when {
    m?.dir == true || b is Body.Folder -> Icon.Folder
    b is Body.Image || m?.type?.startsWith("image/") == true -> Icon.Image
    else -> Icon.Doc
}

private fun isImage(name: String) = name.substringAfterLast('.', "").lowercase() in setOf("png", "jpg", "jpeg", "gif", "webp", "heic", "bmp")

private val languages = mapOf("kt" to "Kotlin", "kts" to "Kotlin", "swift" to "Swift", "go" to "Go", "py" to "Python", "ts" to "TypeScript",
    "tsx" to "TypeScript", "js" to "JavaScript", "rs" to "Rust", "java" to "Java", "c" to "C", "h" to "C", "md" to "Markdown",
    "json" to "JSON", "yaml" to "YAML", "yml" to "YAML", "toml" to "TOML", "sh" to "Shell", "log" to "Log", "txt" to "Text", "html" to "HTML", "css" to "CSS")

/** "PNG image", "Kotlin", "PDF document": what the file is, in a word or two. */
private fun describe(m: FileViewer.Meta?): String {
    if (m == null) return "File"
    val ext = m.name.substringAfterLast('.', "").lowercase()
    return when {
        m.dir -> "Folder"
        m.type.startsWith("image/") -> "${ext.uppercase().ifEmpty { "An" }} image"
        m.text -> languages[ext] ?: "Text"
        m.type == "application/pdf" -> "PDF document"
        m.type.startsWith("video/") -> "Video"
        m.type.startsWith("audio/") -> "Audio"
        ext.isNotEmpty() -> "${ext.uppercase()} file"
        else -> "File"
    }
}

private fun footer(m: FileViewer.Meta?, b: Body): Pair<List<String>, String>? = when (b) {
    is Body.Image -> listOf(describe(m), "${b.width} × ${b.height}") to size(m?.size ?: -1)
    is Body.Text -> listOf(describe(m), if (b.tail) "the last 2 MB" else "${b.lines.size} ${if (b.lines.size == 1) "line" else "lines"}") to size(m?.size ?: -1)
    is Body.Folder -> listOf("Folder") to "${b.entries.size} ${if (b.entries.size == 1) "item" else "items"}, newest first"
    else -> null
}

/** 412 KB, 2.4 MB: as Finder writes sizes. */
fun size(n: Long): String = when {
    n < 0 -> "unknown size"
    n < 1000 -> "$n bytes"
    n < 1_000_000 -> "${(n + 500) / 1000} KB"
    n < 1_000_000_000 -> String.format(java.util.Locale.US, "%.1f MB", n / 1e6)
    else -> String.format(java.util.Locale.US, "%.1f GB", n / 1e9)
}
