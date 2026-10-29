package xyz.matm.hive.model

import android.content.Context
import android.graphics.ImageDecoder
import android.os.Handler
import android.os.Looper
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import org.json.JSONObject
import xyz.matm.hive.net.Endpoint
import java.io.File
import java.io.IOException
import java.net.URLEncoder
import xyz.matm.hive.net.Http
import xyz.matm.hive.net.HttpResponse
import java.security.MessageDigest
import java.util.concurrent.Executors
import java.util.concurrent.Future

/**
 * A file a pane printed, fetched from its host to look at: the server's
 * /v1/file resolves the path where the pane is (HEAD), then this reads it by
 * the resolved path into the cache, with progress. Images and text are shown;
 * anything else is handed to an app on the device (Open in…).
 */
class FileViewer(private val ctx: Context) {
    data class Meta(val path: String, val name: String, val folder: String, val size: Long, val type: String,
                    val text: Boolean, val dir: Boolean, val line: Int?, val modified: Long = 0)
    data class Entry(val name: String, val dir: Boolean, val size: Long)
    enum class Fail { NotFound, Denied, Offline, Other }

    sealed interface Body {
        /** progress: 0 to 1, or null while it isn't known yet. */
        data class Loading(val progress: Float?) : Body
        data class Image(val bitmap: ImageBitmap, val width: Int, val height: Int) : Body
        /** tail: the end of a long file, not its start. */
        data class Text(val lines: List<String>, val tail: Boolean) : Body
        data class Folder(val entries: List<Entry>, val truncated: Boolean) : Body
        /** A type hive doesn't show: Open in…; fetching shows its progress. */
        data class Other(val fetching: Float?) : Body
        object TooLarge : Body
        data class Failed(val kind: Fail, val message: String) : Body
    }

    data class Shown(val host: String, val hostName: String, val asked: String, val meta: Meta?, val body: Body)

    var shown by mutableStateOf<Shown?>(null)
        private set

    /** The file to hand to another app, when one asked for it: read by the UI once. */
    var handOff by mutableStateOf<Pair<File, String>?>(null)

    /** Taps on something that only looked like a path: the pane un-lights it (counted, so each one shows). */
    var misses by mutableStateOf(0)
        private set

    private val back = ArrayDeque<Shown>()
    private val io = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())
    private var job: Future<*>? = null
    private var target: Triple<Endpoint, String?, String>? = null
    val canGoBack get() = back.isNotEmpty()

    init { sweep() }

    /**
     * Each load, tail, Open in…, Back and close starts a new generation; work
     * from an older one (a read a cancel can't interrupt) never lands.
     */
    private var gen = 0

    /** Opens [path] as the pane [block] on host [h] sees it. */
    fun open(h: HostConnection, block: String?, path: String) {
        // Shown once the host says it's there: a tap to focus or aim that lands on a word
        // like a/b mustn't pop a viewer. Only an explicit path says it's gone.
        val explicit = path.startsWith("/") || path.startsWith("~") || path.startsWith("file://")
        load(h, block, path, quiet = !explicit, fresh = true)
    }

    /** A file or folder of the folder shown: Back returns to it. */
    fun openEntry(h: HostConnection, e: Entry) {
        val cur = shown ?: return
        val dir = cur.meta?.path ?: return
        back.addLast(cur)
        load(h, null, "$dir/${e.name}")
    }

    fun goBack(): Boolean {
        val prev = back.removeLastOrNull() ?: return false
        stop()
        shown = prev
        return true
    }

    fun close() {
        stop()
        back.clear()
        shown = null
        target = null
    }

    fun retry(h: HostConnection) { target?.let { (_, b, p) -> load(h, b, p) } }

    private fun stop(): Int { job?.cancel(true); return ++gen }

    /** [fresh]: a new file from the pane (the folder history goes, once it's a file). */
    private fun load(h: HostConnection, block: String?, path: String, quiet: Boolean = false, fresh: Boolean = false) {
        val g = stop()
        val ep = h.client?.endpoint
        val asked = Shown(h.id, h.name, path, null, Body.Loading(null))
        target = ep?.let { Triple(it, block, path) }
        if (ep == null) {
            target = null
            pendingRetry = Triple(h.id, block, path)
            if (fresh) back.clear()
            shown = asked.copy(body = Body.Failed(Fail.Offline, "${h.name} isn’t answering"))
            return
        }
        if (!quiet) { if (fresh) back.clear(); shown = asked }
        job = io.submit {
            val meta = try { head(ep, block, path) } catch (e: Failed) {
                main.post {
                    if (g != gen) return@post
                    if (quiet && e.kind != Fail.Offline) { misses++; return@post }
                    if (fresh) back.clear()
                    shown = asked.copy(body = Body.Failed(e.kind, e.message ?: ""))
                }
                return@submit
            } catch (e: Exception) { main.post { if (g == gen && !quiet) shown = asked.copy(body = Body.Failed(Fail.Other, "")) }; return@submit }
            main.post {
                if (g != gen) return@post
                if (fresh) back.clear()
                shown = asked.copy(meta = meta)
            }
            work(g) {
                when {
                    meta.dir -> {
                        val j = JSONObject(String(get(ep, meta.path, null, null)))
                        val a = j.optJSONArray("entries")
                        val entries = (0 until (a?.length() ?: 0)).map { i -> a!!.getJSONObject(i).let { Entry(it.getString("name"), it.optBoolean("dir"), it.optLong("size")) } }
                        post(g) { it.copy(body = Body.Folder(entries, j.optBoolean("truncated"))) }
                    }
                    // Text first: an SVG is an image to the server, and shown as its source here.
                    meta.text && meta.size <= MAX_TEXT -> {
                        val bytes = get(ep, meta.path, null) { p -> post(g) { it.copy(body = Body.Loading(p)) } }
                        post(g) { it.copy(body = Body.Text(lines(bytes), tail = false)) }
                    }
                    meta.text -> post(g) { it.copy(body = Body.TooLarge) }
                    meta.type.startsWith("image/") && meta.size <= MAX_IMAGE -> {
                        val f = fetch(ep, meta) { p -> post(g) { it.copy(body = Body.Loading(p)) } }
                        val bmp = decode(f)
                        post(g) { it.copy(body = Body.Image(bmp.first, bmp.second, bmp.third)) }
                    }
                    else -> post(g) { it.copy(body = Body.Other(null)) }
                }
            }
        }
    }

    /** A host that was away when a file was asked for: tried again when it's back. */
    var pendingRetry: Triple<String, String?, String>? = null
        private set

    /** The host is back: a file that couldn't come because of it tries again. */
    fun reconnected(h: HostConnection) {
        val p = pendingRetry
        val failed = (shown?.body as? Body.Failed)?.kind == Fail.Offline
        if (!failed) return
        if (p != null && p.first == h.id) { pendingRetry = null; load(h, p.second, p.third); return }
        retry(h)
    }

    /** Runs [body] for generation [g], turning failures into the viewer's states. */
    private inline fun work(g: Int, body: () -> Unit) {
        try {
            body()
        } catch (e: Failed) {
            post(g) { it.copy(body = Body.Failed(e.kind, e.message ?: "")) }
        } catch (_: InterruptedException) {
        } catch (e: Exception) {
            post(g) { it.copy(body = Body.Failed(Fail.Other, "")) }
        }
    }

    /** The end of a long text file: its last 2 MB, from a whole line. */
    fun showTail() {
        val (ep, _, _) = target ?: return
        val meta = shown?.meta ?: return
        val g = stop()
        shown = shown?.copy(body = Body.Loading(null))
        job = io.submit {
            work(g) {
                val bytes = get(ep, meta.path, "bytes=-$MAX_TEXT", null)
                val start = bytes.indexOf('\n'.code.toByte()).let { if (it < 0) 0 else it + 1 }
                post(g) { it.copy(body = Body.Text(lines(bytes.copyOfRange(start, bytes.size)), tail = true)) }
            }
        }
    }

    /** Fetches the file shown (if it isn't in the cache yet) and hands it to another app. */
    fun openIn() {
        val (ep, _, _) = target ?: return
        val cur = shown ?: return
        val meta = cur.meta ?: return
        if (meta.dir || cur.body is Body.Loading) return
        val wasBody = cur.body
        val g = stop()
        // A card shows the fetch's progress; an image or text stays as it is meanwhile.
        val card = wasBody is Body.Other || wasBody is Body.TooLarge
        job = io.submit {
            work(g) {
                val f = fetch(ep, meta) { p -> if (card) post(g) { it.copy(body = Body.Other(p)) } }
                main.post {
                    if (g != gen) return@post
                    if (card) shown = shown?.copy(body = if (wasBody is Body.TooLarge) wasBody else Body.Other(null))
                    handOff = f to meta.type.substringBefore(';')
                }
            }
        }
    }

    /** Stops fetching for Open in…, back to the file's card. */
    fun cancelFetch() {
        stop()
        shown?.let { if (it.body is Body.Other) shown = it.copy(body = Body.Other(null)) }
    }

    // MARK: network (on io)

    private class Failed(val kind: Fail, message: String) : Exception(message)

    private fun query(block: String?, path: String) =
        "/v1/file?path=" + enc(path) + (block?.let { "&block_id=" + enc(it) } ?: "")

    private fun enc(s: String) = URLEncoder.encode(s, "UTF-8").replace("+", "%20")

    private fun connect(ep: Endpoint, method: String, block: String?, path: String, range: String? = null): HttpResponse {
        val r = try {
            Http.request(ep, method, query(block, path), range?.let { mapOf("Range" to it) } ?: emptyMap())
        } catch (e: IOException) { throw Failed(Fail.Offline, "The host isn’t answering") }
        if (r.code >= 400) {
            val msg = runCatching { JSONObject(r.body.bufferedReader().readText()).getJSONObject("error").getString("message") }.getOrDefault("")
            r.close()
            throw Failed(when (r.code) { 404 -> Fail.NotFound; 403 -> Fail.Denied; else -> Fail.Other }, msg)
        }
        return r
    }

    private fun head(ep: Endpoint, block: String?, path: String): Meta {
        val c = connect(ep, "HEAD", block, path)
        c.close()
        val full = c.header("X-Hive-Path") ?: path
        val slash = full.trimEnd('/').lastIndexOf('/')
        return Meta(
            path = full, name = full.trimEnd('/').substring(slash + 1).ifEmpty { "/" },
            folder = if (slash > 0) full.substring(0, slash) else "/",
            size = c.length,
            type = c.header("Content-Type") ?: "application/octet-stream",
            text = c.header("X-Hive-Text") == "1",
            dir = c.header("X-Hive-Kind") == "dir",
            line = c.header("X-Hive-Line")?.toIntOrNull(),
            modified = c.header("Last-Modified")?.let { runCatching { java.time.ZonedDateTime.parse(it, java.time.format.DateTimeFormatter.RFC_1123_DATE_TIME).toInstant().toEpochMilli() }.getOrNull() } ?: 0,
        )
    }

    /** The whole response, reporting progress when its length is known. */
    private fun get(ep: Endpoint, path: String, range: String?, progress: ((Float) -> Unit)?): ByteArray {
        val c = connect(ep, "GET", null, path, range)
        try {
            val total = c.length
            val out = java.io.ByteArrayOutputStream(if (total in 1..MAX_TEXT) total.toInt() else 64 * 1024)
            copy(c, total, progress) { b, n -> out.write(b, 0, n) }
            return out.toByteArray()
        } finally { c.close() }
    }

    /** The file in the cache (open/<hash of host and path>/<name>), fetched unless it's there and current. */
    private fun fetch(ep: Endpoint, meta: Meta, progress: (Float) -> Unit): File {
        val dir = File(ctx.cacheDir, "open/" + hash(ep.url + meta.path))
        val f = File(dir, meta.name)
        // Kept from before only if it's the same: size and time (a log rewritten in place keeps its size).
        if (f.exists() && f.length() == meta.size && meta.modified != 0L && f.lastModified() == meta.modified) return f
        dir.mkdirs()
        val tmp = File(dir, ".part")
        val c = connect(ep, "GET", null, meta.path)
        try {
            tmp.outputStream().use { o -> copy(c, c.length, progress) { b, n -> o.write(b, 0, n) } }
        } catch (e: IOException) {
            tmp.delete(); throw Failed(Fail.Offline, "The connection dropped")
        } finally { c.close() }
        if (meta.size >= 0 && tmp.length() != meta.size) { tmp.delete(); throw Failed(Fail.Other, "${meta.name} changed while it was read") }
        tmp.renameTo(f)
        if (meta.modified != 0L) f.setLastModified(meta.modified)
        return f
    }

    private fun copy(c: HttpResponse, total: Long, progress: ((Float) -> Unit)?, write: (ByteArray, Int) -> Unit) {
        val buf = ByteArray(64 * 1024)
        var done = 0L
        var last = 0L
        c.body.let { i ->
            while (true) {
                if (Thread.currentThread().isInterrupted) throw InterruptedException()
                val n = try { i.read(buf) } catch (e: IOException) { throw Failed(Fail.Offline, "The connection dropped") }
                if (n < 0) break
                write(buf, n)
                done += n
                val now = System.currentTimeMillis()
                if (progress != null && total > 0 && now - last > 100) { last = now; progress(done.toFloat() / total) }
            }
        }
    }

    /** Decoded at most screen size: a 12000 px photo would take hundreds of MB. */
    private fun decode(f: File): Triple<ImageBitmap, Int, Int> {
        var w = 0
        var hgt = 0
        val max = ctx.resources.displayMetrics.let { maxOf(it.widthPixels, it.heightPixels) }
        val bmp = try {
            ImageDecoder.decodeBitmap(ImageDecoder.createSource(f)) { d, info, _ ->
                w = info.size.width; hgt = info.size.height
                val scale = maxOf(w, hgt).toFloat() / max
                if (scale > 1) d.setTargetSize((w / scale).toInt().coerceAtLeast(1), (hgt / scale).toInt().coerceAtLeast(1))
                d.allocator = ImageDecoder.ALLOCATOR_SOFTWARE
            }
        } catch (e: Exception) { throw Failed(Fail.Other, "This image can’t be shown here") }
        return Triple(bmp.asImageBitmap(), w, hgt)
    }

    private fun lines(b: ByteArray): List<String> = String(b, Charsets.UTF_8).replace("\r\n", "\n").split('\n').let {
        if (it.size > 1 && it.last().isEmpty()) it.dropLast(1) else it
    }

    private fun post(g: Int, f: (Shown) -> Shown) = main.post { if (g == gen) shown?.let { shown = f(it) } }

    private fun hash(s: String) = MessageDigest.getInstance("SHA-256").digest(s.toByteArray()).take(8).joinToString("") { "%02x".format(it) }

    /** Copies handed to other apps stay a day: the app may read them later (an upload in the background). */
    private fun sweep() {
        io.execute {
            val old = System.currentTimeMillis() - 24 * 3600 * 1000L
            File(ctx.cacheDir, "open").listFiles()?.forEach { d -> if (d.lastModified() < old) d.deleteRecursively() }
        }
    }

    companion object {
        const val MAX_TEXT = 2L * 1024 * 1024
        const val MAX_IMAGE = 40L * 1024 * 1024
    }
}
