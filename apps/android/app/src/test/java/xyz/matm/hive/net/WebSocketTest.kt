package xyz.matm.hive.net

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream

class WebSocketTest {
    /** A server frame: unmasked. */
    private fun frame(op: Int, payload: ByteArray, fin: Boolean = true): ByteArray {
        val o = ByteArrayOutputStream()
        o.write((if (fin) 0x80 else 0) or op)
        val n = payload.size
        when {
            n < 126 -> o.write(n)
            n < 65536 -> { o.write(126); o.write(n shr 8); o.write(n and 0xFF) }
            else -> { o.write(127); for (i in 7 downTo 0) o.write(((n.toLong() shr (8 * i)) and 0xFF).toInt()) }
        }
        o.write(payload)
        return o.toByteArray()
    }

    @Test fun readsFragmentsWithControlFramesBetween() {
        val big = ByteArray(70_000) { (it % 251).toByte() }
        val stream = frame(1, "hel".toByteArray(), fin = false) + frame(9, byteArrayOf(7)) + frame(0, "lo".toByteArray()) +
            frame(2, big) + frame(2, ByteArray(300) { 1 }) + frame(8, byteArrayOf(0x03, 0xE8.toByte()))
        val input = ByteArrayInputStream(stream)
        val p = WsParser()
        assertArrayEquals(byteArrayOf(7), (p.read(input) as WsMessage.Ping).data)
        assertEquals("hello", (p.read(input) as WsMessage.Text).data)
        assertArrayEquals(big, (p.read(input) as WsMessage.Binary).data)
        assertEquals(300, (p.read(input) as WsMessage.Binary).data.size)
        assertEquals(1000, (p.read(input) as WsMessage.Close).code)
    }

    @Test fun clientFramesAreMaskedAndDecodeBack() {
        for (n in listOf(0, 5, 125, 126, 65535, 65536)) {
            val payload = ByteArray(n) { (it * 7).toByte() }
            val f = WsParser.encode(2, payload)
            assertTrue("mask bit", f[1].toInt() and 0x80 != 0)
            assertArrayEquals("n=$n", payload, (WsParser().read(ByteArrayInputStream(f)) as WsMessage.Binary).data)
        }
    }

    @Test fun acceptKeyIsRfc6455s() {
        assertEquals("s3pPLMBiTxaQ9kYGzzhZRbK+xOo=", WsHandshake.accept("dGhlIHNhbXBsZSBub25jZQ=="))
        val req = String(WsHandshake.request("/v1/attach", "box:4483", "k"))
        assertTrue(req.contains("Host: box:4483\r\n"))
        assertTrue("never an Origin: the server refuses browsers", !req.contains("Origin"))
    }

    @Test fun parsesAttachFrames() {
        val id = "block:1".toByteArray()
        val d = byteArrayOf(3, 0, id.size.toByte()) + id + byteArrayOf(0, 0, 0, 0, 0, 0, 1, 2) + byteArrayOf(0, 80, 0, 24, 0, 9, 0, 18)
        val f = HiveClient.Frame.parse(d)!!
        assertEquals(3, f.kind); assertEquals("block:1", f.blockID); assertEquals(258L, f.seq)
        assertArrayEquals(byteArrayOf(0, 80, 0, 24, 0, 9, 0, 18), f.payload())
        assertEquals(null, HiveClient.Frame.parse(byteArrayOf(1, 0, 9, 1)))
    }
}
