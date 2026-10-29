package xyz.matm.hive.net

import java.io.EOFException
import java.io.FilterInputStream
import java.io.IOException
import java.io.InputStream
import java.net.InetSocketAddress
import java.net.Socket
import javax.net.ssl.SSLParameters
import javax.net.ssl.SSLSocket
import javax.net.ssl.SSLSocketFactory

/**
 * One HTTP/1.1 request to a host's server, on a socket of its own as the
 * attach stream opens one: plain over the tailnet (WireGuard encrypts it),
 * TLS for a Tailscale Serve name. Android's HttpURLConnection would refuse
 * the plain one; this way the app keeps its no-cleartext policy for
 * everything else.
 */
class HttpResponse(val code: Int, val headers: Map<String, String>, val body: InputStream, private val socket: Socket) : AutoCloseable {
    fun header(name: String): String? = headers[name.lowercase()]
    val length: Long get() = header("content-length")?.toLongOrNull() ?: -1
    override fun close() { runCatching { socket.close() } }
}

object Http {
    private const val CONNECT_TIMEOUT_MS = 8000
    private const val READ_TIMEOUT_MS = 20000

    fun request(endpoint: Endpoint, method: String, pathAndQuery: String, headers: Map<String, String> = emptyMap()): HttpResponse {
        val raw = Socket()
        try {
            raw.connect(InetSocketAddress(endpoint.host, endpoint.port), CONNECT_TIMEOUT_MS)
            val s: Socket = if (endpoint.tls) {
                val ssl = (SSLSocketFactory.getDefault() as SSLSocketFactory).createSocket(raw, endpoint.host, endpoint.port, true) as SSLSocket
                val p: SSLParameters = ssl.sslParameters
                p.applicationProtocols = arrayOf("http/1.1")
                p.endpointIdentificationAlgorithm = "HTTPS"
                ssl.sslParameters = p
                ssl.soTimeout = CONNECT_TIMEOUT_MS
                ssl.startHandshake()
                ssl
            } else raw
            s.soTimeout = READ_TIMEOUT_MS
            val req = StringBuilder()
                .append(method).append(' ').append(pathAndQuery).append(" HTTP/1.1\r\n")
                .append("Host: ").append(endpoint.hostHeader).append("\r\n")
                .append("Connection: close\r\n")
                .append("User-Agent: hive-android\r\n")
            headers.forEach { (k, v) -> req.append(k).append(": ").append(v).append("\r\n") }
            req.append("\r\n")
            val o = s.getOutputStream()
            o.write(req.toString().toByteArray(Charsets.ISO_8859_1))
            o.flush()
            val input = s.getInputStream().buffered(64 shl 10)
            val status = line(input) ?: throw EOFException("no response")
            val code = status.split(' ').getOrNull(1)?.toIntOrNull() ?: throw IOException("bad status line: $status")
            val h = HashMap<String, String>()
            while (true) {
                val l = line(input) ?: break
                if (l.isEmpty()) break
                val i = l.indexOf(':')
                if (i > 0) h[l.substring(0, i).trim().lowercase()] = l.substring(i + 1).trim()
            }
            val body: InputStream = when {
                method == "HEAD" || code == 204 || code == 304 -> java.io.ByteArrayInputStream(ByteArray(0)) // nullInputStream is API 33
                h["transfer-encoding"]?.contains("chunked", ignoreCase = true) == true -> Chunked(input)
                h["content-length"] != null -> Limited(input, h["content-length"]!!.toLong())
                else -> input
            }
            return HttpResponse(code, h, body, s)
        } catch (e: Exception) {
            runCatching { raw.close() }
            throw e
        }
    }

    /** A CRLF-ended line, as Latin-1 (header names and values are ASCII). */
    internal fun line(i: InputStream): String? {
        val sb = StringBuilder()
        while (true) {
            val c = i.read()
            if (c < 0) return if (sb.isEmpty()) null else sb.toString()
            if (c == '\n'.code) return sb.toString().trimEnd('\r')
            sb.append(c.toChar())
            if (sb.length > 16 * 1024) throw IOException("header line too long")
        }
    }

    private class Limited(i: InputStream, private var left: Long) : FilterInputStream(i) {
        override fun read(): Int = if (left <= 0) -1 else super.read().also { if (it >= 0) left-- }
        override fun read(b: ByteArray, off: Int, len: Int): Int {
            if (left <= 0) return -1
            val n = super.read(b, off, minOf(len.toLong(), left).toInt())
            if (n > 0) left -= n
            return n
        }
    }

    /** Transfer-Encoding: chunked, as Go sends a body it hasn't measured. */
    internal class Chunked(private val i: InputStream) : InputStream() {
        private var left = 0L
        private var done = false
        override fun read(): Int {
            val b = ByteArray(1)
            return if (read(b, 0, 1) < 0) -1 else b[0].toInt() and 0xFF
        }
        override fun read(b: ByteArray, off: Int, len: Int): Int {
            if (done) return -1
            if (left == 0L) {
                val size = line(i)?.substringBefore(';')?.trim()?.toLongOrNull(16) ?: throw EOFException("bad chunk")
                if (size == 0L) { done = true; while (line(i)?.isNotEmpty() == true) {}; return -1 }
                left = size
            }
            val n = i.read(b, off, minOf(len.toLong(), left).toInt())
            if (n < 0) throw EOFException("chunk cut short")
            left -= n
            if (left == 0L) line(i)
            return n
        }
    }
}
