package xyz.matm.hive.ui

import com.google.zxing.BarcodeFormat
import com.google.zxing.qrcode.QRCodeWriter
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import xyz.matm.hive.model.Host
import xyz.matm.hive.model.HostsCode

class QrTest {
    /** A camera frame's luminance plane: the code, dark on light, with a margin, rows padded past the width. */
    private fun frame(text: String, scale: Int = 4, rowPad: Int = 16): Triple<ByteArray, Int, Int> {
        val m = QRCodeWriter().encode(text, BarcodeFormat.QR_CODE, 0, 0)
        val w = (m.width + 8) * scale
        val stride = w + rowPad
        val y = ByteArray(stride * w) { 0xE0.toByte() }
        for (r in 0 until m.height) for (c in 0 until m.width) if (m[c, r]) {
            for (dy in 0 until scale) for (dx in 0 until scale) y[((r + 4) * scale + dy) * stride + (c + 4) * scale + dx] = 0x10
        }
        return Triple(y, w, stride)
    }

    @Test fun readsTheMacsCodeFromAFrame() {
        val code = HostsCode.encode(listOf(Host("box", "https://box.example.ts.net"), Host("lab", "http://100.101.2.3:4483")))
        val (y, w, stride) = frame(code)
        assertEquals(code, Qr.decode(y, w, w, stride))
    }

    @Test fun nothingInAnEmptyFrame() {
        assertNull(Qr.decode(ByteArray(64 * 64) { 0x80.toByte() }, 64, 64, 64))
    }
}
