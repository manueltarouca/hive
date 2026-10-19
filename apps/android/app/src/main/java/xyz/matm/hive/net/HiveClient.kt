package xyz.matm.hive.net

import org.json.JSONObject
import java.util.Base64
import java.util.concurrent.Executor
import java.util.concurrent.Executors

/** An error from the server: `{status, code, message}`, or a lost connection. */
class HiveError(val code: String, override val message: String, val status: Int = 0) : Exception(message) {
    companion object {
        fun from(j: JSONObject?) = HiveError(j?.optString("code", "error") ?: "error", j?.optString("message", "") ?: "", j?.optInt("status") ?: 0)
        val closed = HiveError("closed", "connection closed")
    }
}

/** What a subscribed block delivers, in order. */
sealed class BlockUpdate {
    class Snapshot(val seq: Long, val data: ByteArray) : BlockUpdate()
    class Output(val seq: Long, val data: ByteArray) : BlockUpdate()
    class Resize(val seq: Long, val cols: Int, val rows: Int, val cellWidth: Int, val cellHeight: Int) : BlockUpdate()
    /** The client fell behind: subscribe again for a fresh snapshot. */
    object Resync : BlockUpdate()
    object Exited : BlockUpdate()
}

/**
 * The attach-stream client, as HiveKit's HiveClient: hello, calls, events
 * and block streams over one WebSocket. Calls and events are delivered on
 * [main]; block updates on the connection's own thread, in order, for the
 * caller to hand to its vt thread.
 */
class HiveClient(val endpoint: Endpoint, private val main: Executor) {
    private val net = Executors.newSingleThreadExecutor { r -> Thread(r, "hive.net ${endpoint.hostHeader}").apply { isDaemon = true } }
    private val ws = WebSocketConnection(endpoint, net)
    private var nextID = 1L
    private val calls = HashMap<Long, (Result<JSONObject>) -> Unit>()
    private val blockHandlers = HashMap<String, (BlockUpdate) -> Unit>()
    private val awaitingSnapshot = HashSet<String>()
    private val lock = Any()

    var clientID: String? = null
        private set
    var serverVersion: String? = null
        private set
    var onEvent: ((JSONObject) -> Unit)? = null
    var onEventsResync: (() -> Unit)? = null
    var onClose: ((Throwable?) -> Unit)? = null
    var onError: ((String?, String?, HiveError) -> Unit)? = null

    /** Connects and says hello; [ready] runs on [main] once welcomed, or with the failure. */
    fun connect(name: String, version: String, ready: (Result<String>) -> Unit) {
        var greeted = false
        ws.onOpen = {
            send(JSONObject().put("type", "hello").put("name", name).put("kind", "app").put("platform", "android").put("version", version))
        }
        ws.onMessage = { m ->
            when (m) {
                is WsMessage.Text -> {
                    val j = runCatching { JSONObject(m.data) }.getOrNull()
                    if (j != null) {
                        if (!greeted && j.optString("type") == "welcome") {
                            greeted = true
                            clientID = j.optString("client_id")
                            serverVersion = j.optString("version")
                            main.execute { ready(Result.success(clientID ?: "")) }
                        } else handleText(j)
                    }
                }
                is WsMessage.Binary -> handleBinary(m.data)
                else -> {}
            }
        }
        ws.onClose = { err ->
            val pending: List<(Result<JSONObject>) -> Unit>
            synchronized(lock) {
                pending = calls.values.toList()
                calls.clear()
                blockHandlers.clear()
                awaitingSnapshot.clear()
            }
            main.execute {
                if (!greeted) ready(Result.failure(err ?: HiveError.closed))
                pending.forEach { it(Result.failure(HiveError.closed)) }
                onClose?.invoke(err)
            }
            net.shutdown()
        }
        ws.start()
    }

    fun close() = ws.close()

    private fun send(j: JSONObject) = ws.send(j.toString())

    /** Calls a server method; [done] runs on [main]. */
    fun call(method: String, params: JSONObject = JSONObject(), done: (Result<JSONObject>) -> Unit = {}) {
        val id: Long
        synchronized(lock) {
            id = nextID++
            calls[id] = done
        }
        send(JSONObject().put("type", "call").put("id", id).put("method", method).put("params", params))
    }

    /** Starts the event stream. `since` null: live only. */
    fun subscribeEvents(all: Boolean = true, since: Long? = null) {
        val m = JSONObject().put("type", "events").put("all", all)
        if (since != null) m.put("since", since)
        send(m)
    }

    /** [handler] gets a snapshot, then output and resizes in order, until resync or exit; on the connection's thread. */
    fun subscribe(block: String, handler: (BlockUpdate) -> Unit) {
        synchronized(lock) {
            blockHandlers[block] = handler
            awaitingSnapshot.add(block)
        }
        send(JSONObject().put("type", "subscribe").put("block_id", block))
    }

    fun unsubscribe(block: String) {
        synchronized(lock) { blockHandlers.remove(block); awaitingSnapshot.remove(block) }
        send(JSONObject().put("type", "unsubscribe").put("block_id", block))
    }

    fun input(block: String, bytes: ByteArray) {
        if (bytes.isEmpty()) return
        send(JSONObject().put("type", "input").put("block_id", block).put("data_base64", Base64.getEncoder().encodeToString(bytes)))
    }

    /** A named key ("escape", "ctrl+c", "up"), encoded by the server for the program's modes. */
    fun key(block: String, name: String) {
        send(JSONObject().put("type", "key").put("block_id", block).put("key", name))
    }

    fun resize(block: String, cols: Int, rows: Int, cellWidth: Int, cellHeight: Int, take: Boolean) {
        send(JSONObject().put("type", "resize").put("block_id", block).put("cols", cols).put("rows", rows)
            .put("cell_width_px", cellWidth).put("cell_height_px", cellHeight).put("take", take))
    }

    private fun handleText(j: JSONObject) {
        when (j.optString("type")) {
            "result" -> {
                val cb = synchronized(lock) { calls.remove(j.optLong("id")) } ?: return
                val err = j.optJSONObject("error")
                val r = if (err != null) Result.failure(HiveError.from(err))
                else Result.success(j.optJSONObject("result") ?: JSONObject())
                main.execute { cb(r) }
            }
            "event" -> j.optJSONObject("event")?.let { ev -> main.execute { onEvent?.invoke(ev) } }
            "events_resync" -> main.execute { onEventsResync?.invoke() }
            "resync" -> {
                val b = j.optString("block_id")
                synchronized(lock) { blockHandlers.remove(b) }?.invoke(BlockUpdate.Resync)
            }
            "exited" -> synchronized(lock) { blockHandlers[j.optString("block_id")] }?.invoke(BlockUpdate.Exited)
            "error" -> {
                val block = j.optString("block_id").ifEmpty { null }
                val forWhat = j.optString("for").ifEmpty { null }
                if (forWhat == "subscribe" && block != null) synchronized(lock) { blockHandlers.remove(block) }
                val e = HiveError.from(j.optJSONObject("error"))
                main.execute { onError?.invoke(forWhat, block, e) }
            }
        }
    }

    private fun handleBinary(d: ByteArray) {
        val f = Frame.parse(d) ?: return
        val handler: ((BlockUpdate) -> Unit)
        val awaiting: Boolean
        synchronized(lock) {
            handler = blockHandlers[f.blockID] ?: return
            awaiting = f.blockID in awaitingSnapshot
            if (f.kind == 2) awaitingSnapshot.remove(f.blockID)
        }
        when {
            f.kind == 2 -> handler(BlockUpdate.Snapshot(f.seq, f.payload()))
            f.kind == 1 && !awaiting -> handler(BlockUpdate.Output(f.seq, f.payload()))
            f.kind == 3 && !awaiting && f.payloadSize == 8 -> {
                fun u16(i: Int) = ((d[f.payloadAt + i].toInt() and 0xFF) shl 8) or (d[f.payloadAt + i + 1].toInt() and 0xFF)
                handler(BlockUpdate.Resize(f.seq, u16(0), u16(2), u16(4), u16(6)))
            }
        }
    }

    /** A binary attach frame: [kind][u16 id length][id][u64 seq][payload]. */
    class Frame(val kind: Int, val blockID: String, val seq: Long, private val data: ByteArray, val payloadAt: Int) {
        val payloadSize get() = data.size - payloadAt
        fun payload(): ByteArray = data.copyOfRange(payloadAt, data.size)

        companion object {
            fun parse(d: ByteArray): Frame? {
                if (d.size < 3) return null
                val n = ((d[1].toInt() and 0xFF) shl 8) or (d[2].toInt() and 0xFF)
                if (d.size < 3 + n + 8) return null
                var seq = 0L
                for (i in 0 until 8) seq = (seq shl 8) or (d[3 + n + i].toLong() and 0xFF)
                return Frame(d[0].toInt() and 0xFF, String(d, 3, n), seq, d, 3 + n + 8)
            }
        }
    }
}
