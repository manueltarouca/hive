package xyz.matm.hive.net

import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.net.InetSocketAddress
import java.net.Socket
import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Base64
import java.util.concurrent.Executor
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.TimeUnit
import javax.net.ssl.SSLParameters
import javax.net.ssl.SSLSocket
import javax.net.ssl.SSLSocketFactory

/** A complete WebSocket message, after reassembling fragments. */
sealed class WsMessage {
    class Text(val data: String) : WsMessage()
    class Binary(val data: ByteArray) : WsMessage()
    class Ping(val data: ByteArray) : WsMessage()
    class Pong(val data: ByteArray) : WsMessage()
    class Close(val code: Int?) : WsMessage()
}

class WsProtocolError(message: String) : IOException(message)

/**
 * Parses server-to-client frames (RFC 6455): fragments, control frames
 * interleaved with them, and 7/16/64-bit lengths, as HiveKit's parser does.
 */
/** maxMessage: hosts are untrusted (a scanned code can name any); the server's own messages stay far below. */
class WsParser(private val maxMessage: Int = 32 shl 20) {
    private var fragments: ByteArrayOutputStream? = null
    private var fragmentOpcode = -1

    /** Reads the next message, blocking. */
    fun read(input: InputStream): WsMessage {
        while (true) {
            val b0 = input.readByte()
            val b1 = input.readByte()
            val fin = b0 and 0x80 != 0
            val opcode = b0 and 0x0F
            val masked = b1 and 0x80 != 0
            var len = (b1 and 0x7F).toLong()
            if (len == 126L) len = (input.readByte().toLong() shl 8) or input.readByte().toLong()
            else if (len == 127L) { len = 0; repeat(8) { len = (len shl 8) or input.readByte().toLong() } }
            if (len > maxMessage) throw WsProtocolError("frame of $len bytes")
            val mask = if (masked) input.readExactly(4) else null
            val payload = input.readExactly(len.toInt())
            if (mask != null) for (i in payload.indices) payload[i] = (payload[i].toInt() xor mask[i % 4].toInt()).toByte()
            when (opcode) {
                0x8 -> return WsMessage.Close(if (payload.size >= 2) ((payload[0].toInt() and 0xFF) shl 8) or (payload[1].toInt() and 0xFF) else null)
                0x9 -> return WsMessage.Ping(payload)
                0xA -> return WsMessage.Pong(payload)
                0x1, 0x2 -> {
                    if (fragmentOpcode != -1) throw WsProtocolError("new message inside a fragmented one")
                    if (fin) return if (opcode == 1) WsMessage.Text(String(payload)) else WsMessage.Binary(payload)
                    fragmentOpcode = opcode
                    fragments = ByteArrayOutputStream().also { it.write(payload) }
                }
                0x0 -> {
                    val f = fragments ?: throw WsProtocolError("continuation without a start")
                    f.write(payload)
                    if (f.size() > maxMessage) throw WsProtocolError("message too large")
                    if (fin) {
                        val whole = f.toByteArray()
                        val op = fragmentOpcode
                        fragments = null
                        fragmentOpcode = -1
                        return if (op == 1) WsMessage.Text(String(whole)) else WsMessage.Binary(whole)
                    }
                }
                else -> throw WsProtocolError("opcode $opcode")
            }
        }
    }

    companion object {
        private val random = SecureRandom()

        /** A client frame. Client frames are always masked. */
        fun encode(opcode: Int, payload: ByteArray): ByteArray {
            val out = ByteArrayOutputStream(payload.size + 14)
            out.write(0x80 or opcode)
            val n = payload.size
            when {
                n < 126 -> out.write(0x80 or n)
                n < 65536 -> { out.write(0x80 or 126); out.write(n shr 8); out.write(n and 0xFF) }
                else -> { out.write(0x80 or 127); for (i in 7 downTo 0) out.write(((n.toLong() shr (8 * i)) and 0xFF).toInt()) }
            }
            val mask = ByteArray(4).also(random::nextBytes)
            out.write(mask)
            val body = ByteArray(n) { (payload[it].toInt() xor mask[it % 4].toInt()).toByte() }
            out.write(body)
            return out.toByteArray()
        }
    }
}

private fun InputStream.readByte(): Int {
    val b = read()
    if (b < 0) throw IOException("connection closed")
    return b
}

private fun InputStream.readExactly(n: Int): ByteArray {
    val buf = ByteArray(n)
    var at = 0
    while (at < n) {
        val r = read(buf, at, n - at)
        if (r < 0) throw IOException("connection closed")
        at += r
    }
    return buf
}

object WsHandshake {
    private const val GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

    fun newKey(): String = Base64.getEncoder().encodeToString(ByteArray(16).also(SecureRandom()::nextBytes))

    fun accept(key: String): String =
        Base64.getEncoder().encodeToString(MessageDigest.getInstance("SHA-1").digest((key + GUID).toByteArray()))

    /** No Origin (the server refuses browsers) and no extensions (no compression). */
    fun request(path: String, host: String, key: String) =
        ("GET $path HTTP/1.1\r\nHost: $host\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
            "Sec-WebSocket-Key: $key\r\nSec-WebSocket-Version: 13\r\n\r\n").toByteArray()
}

/**
 * A WebSocket connection to a hive server's attach endpoint. Callbacks run
 * on [callbacks], in order. Over the network a connection can die without
 * closing (sleep, a network change): it pings every 20 s and gives up when
 * nothing has arrived for 45 s; connecting gets 15 s.
 */
class WebSocketConnection(
    val endpoint: Endpoint,
    private val callbacks: Executor,
    private val path: String = "/v1/attach",
) {
    var onOpen: (() -> Unit)? = null
    var onMessage: ((WsMessage) -> Unit)? = null
    var onClose: ((Throwable?) -> Unit)? = null

    @Volatile private var socket: Socket? = null
    @Volatile private var out: OutputStream? = null
    @Volatile private var closed = false
    @Volatile private var lastHeard = System.nanoTime()
    private var keepalive: ScheduledFuture<*>? = null
    private val writeLock = Any()
    /** Writes leave from here, in order: Android forbids the network on the main thread. */
    private val writer = Executors.newSingleThreadExecutor { r -> Thread(r, "hive.ws.write").apply { isDaemon = true } }

    companion object {
        const val PING_EVERY_S = 20L
        const val SILENT_LIMIT_S = 45L
        const val CONNECT_TIMEOUT_MS = 15_000
        private val timers: ScheduledExecutorService = Executors.newSingleThreadScheduledExecutor { r ->
            Thread(r, "hive.ws.timer").apply { isDaemon = true }
        }
    }

    fun start() {
        Thread({ run() }, "hive.ws ${endpoint.hostHeader}").apply { isDaemon = true }.start()
    }

    private fun run() {
        var error: Throwable? = null
        try {
            val raw = Socket()
            socket = raw
            raw.tcpNoDelay = true // keystrokes go out at once
            raw.keepAlive = true
            raw.connect(InetSocketAddress(endpoint.host, endpoint.port), CONNECT_TIMEOUT_MS)
            val s: Socket = if (endpoint.tls) {
                val ssl = (SSLSocketFactory.getDefault() as SSLSocketFactory)
                    .createSocket(raw, endpoint.host, endpoint.port, true) as SSLSocket
                // HTTP/1.1 only: the attach stream is an HTTP/1.1 upgrade.
                val p: SSLParameters = ssl.sslParameters
                p.applicationProtocols = arrayOf("http/1.1")
                p.endpointIdentificationAlgorithm = "HTTPS"
                ssl.sslParameters = p
                ssl.soTimeout = CONNECT_TIMEOUT_MS
                ssl.startHandshake()
                ssl
            } else raw
            socket = s
            s.soTimeout = CONNECT_TIMEOUT_MS
            val input = s.getInputStream().buffered(64 shl 10)
            val o = s.getOutputStream()
            val key = WsHandshake.newKey()
            o.write(WsHandshake.request(path, endpoint.hostHeader, key))
            o.flush()
            readHandshake(input, key)
            s.soTimeout = 0
            out = o
            if (closed) throw IOException("closed while connecting")
            lastHeard = System.nanoTime()
            keepalive = timers.scheduleWithFixedDelay({ tick() }, PING_EVERY_S, PING_EVERY_S, TimeUnit.SECONDS)
            callbacks.execute { if (!closed) onOpen?.invoke() }
            val parser = WsParser()
            while (!closed) {
                val m = parser.read(input)
                lastHeard = System.nanoTime()
                when (m) {
                    is WsMessage.Ping -> runCatching { writer.execute { runCatching { write(0xA, m.data) } } }
                    is WsMessage.Close -> {
                        // Answered, so the server needn't wait out its close timeout.
                        runCatching { synchronized(writeLock) { o.write(WsParser.encode(0x8, m.code?.let { byteArrayOf((it shr 8).toByte(), it.toByte()) } ?: ByteArray(0))); o.flush() } }
                        error = null
                        break
                    }
                    else -> callbacks.execute { if (!closed) onMessage?.invoke(m) }
                }
            }
        } catch (t: Throwable) {
            error = if (closed) null else t
        }
        finish(error)
    }

    private fun readHandshake(input: InputStream, key: String) {
        val head = StringBuilder()
        while (!head.endsWith("\r\n\r\n")) {
            val b = input.read()
            if (b < 0) throw IOException("closed during the handshake")
            head.append(b.toChar())
            if (head.length > 16 shl 10) throw WsProtocolError("handshake too long")
        }
        val lines = head.split("\r\n")
        val status = lines.first()
        if (!status.startsWith("HTTP/1.1 101")) throw HandshakeRefused(status.substringAfter(' '))
        val accept = lines.firstOrNull { it.lowercase().startsWith("sec-websocket-accept:") }?.substringAfter(':')?.trim()
        if (accept != WsHandshake.accept(key)) throw WsProtocolError("bad Sec-WebSocket-Accept")
    }

    private fun tick() {
        if (closed) return
        if (System.nanoTime() - lastHeard > TimeUnit.SECONDS.toNanos(SILENT_LIMIT_S)) {
            runCatching { socket?.close() } // the reader fails and finishes
            return
        }
        runCatching { writer.execute { runCatching { write(0x9, ByteArray(0)) } } }
    }

    private fun write(opcode: Int, payload: ByteArray) {
        val o = out ?: return
        val frame = WsParser.encode(opcode, payload)
        synchronized(writeLock) {
            o.write(frame)
            o.flush()
        }
    }

    /** Sends a text message; from any thread. Lost silently if the connection is down (onClose follows). */
    fun send(text: String) {
        val bytes = text.toByteArray()
        runCatching {
            writer.execute { try { write(0x1, bytes) } catch (_: IOException) { runCatching { socket?.close() } } }
        }
    }

    fun close() {
        if (closed) return
        closed = true
        runCatching {
            writer.execute {
                runCatching { write(0x8, byteArrayOf(0x03, 0xE8.toByte())) }
                runCatching { socket?.close() }
            }
        }
        writer.shutdown()
    }

    private fun finish(error: Throwable?) {
        keepalive?.cancel(false)
        writer.shutdown()
        runCatching { socket?.close() }
        val wasClosed = closed
        closed = true
        callbacks.execute { onClose?.invoke(if (wasClosed) null else error) }
    }
}

/** The server answered the upgrade with an HTTP error, such as 403 for a caller it doesn't let in. */
class HandshakeRefused(val status: String) : IOException("refused: $status") {
    val code: Int get() = status.substringBefore(' ').toIntOrNull() ?: 0
}
