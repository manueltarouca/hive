package xyz.matm.hive.net

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.net.ServerSocket
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import kotlin.concurrent.thread

/** Against a loopback server that speaks just enough WebSocket. */
class WebSocketConnectionTest {
    private fun server(handshakeDelayMs: Long = 0, after: (java.net.Socket) -> Unit = {}): Pair<ServerSocket, CountDownLatch> {
        val ss = ServerSocket(0)
        val done = CountDownLatch(1)
        thread(isDaemon = true) {
            ss.accept().use { s ->
                val input = s.getInputStream()
                val head = StringBuilder()
                while (!head.endsWith("\r\n\r\n")) head.append(input.read().toChar())
                val key = head.lines().first { it.startsWith("Sec-WebSocket-Key:") }.substringAfter(':').trim()
                Thread.sleep(handshakeDelayMs)
                s.getOutputStream().write(("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
                    "Sec-WebSocket-Accept: ${WsHandshake.accept(key)}\r\n\r\n").toByteArray())
                runCatching { after(s) }
                done.countDown()
            }
        }
        return ss to done
    }

    @Test fun closingWhileConnectingStillReportsTheClose() {
        val (ss, _) = server(handshakeDelayMs = 300) { Thread.sleep(2000) }
        val closed = CountDownLatch(1)
        val c = WebSocketConnection(Endpoint("127.0.0.1", ss.localPort, false), { it.run() })
        c.onClose = { closed.countDown() }
        // Closed before the connection thread has a socket to close.
        c.start()
        c.close()
        assertTrue("onClose after close() while connecting", closed.await(2, TimeUnit.SECONDS))
    }

    @Test fun aServerCloseIsAnsweredAndReported() {
        var answered = -1
        val (ss, done) = server { s ->
            s.getOutputStream().write(byteArrayOf(0x88.toByte(), 2, 0x03, 0xE8.toByte())) // close 1000
            // The client's own Close comes back, masked.
            answered = s.getInputStream().read() and 0x0F
        }
        val closed = CountDownLatch(1)
        var err: Throwable? = Throwable("unset")
        val c = WebSocketConnection(Endpoint("127.0.0.1", ss.localPort, false), { it.run() })
        c.onClose = { err = it; closed.countDown() }
        c.start()
        assertTrue(closed.await(2, TimeUnit.SECONDS))
        assertEquals(null, err)
        done.await(2, TimeUnit.SECONDS)
        assertEquals("the client answers Close with Close", 0x8, answered)
    }
}
