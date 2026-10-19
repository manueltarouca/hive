package xyz.matm.hive.net

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class EndpointTest {
    @Test fun urlsAreTakenAsTheyAre() {
        assertEquals(listOf(Endpoint("box.example.ts.net", 443, true)), Endpoint.candidates("https://box.example.ts.net"))
        assertEquals(listOf(Endpoint("100.64.0.1", 4483, false)), Endpoint.candidates("http://100.64.0.1:4483"))
    }

    @Test fun aBareHostTriesTheMacsOrder() {
        assertEquals(
            listOf(Endpoint("box", 443, true), Endpoint("box", 4483, true), Endpoint("box", 4483, false)),
            Endpoint.candidates("box"),
        )
        assertEquals(listOf(Endpoint("box", 9000, true), Endpoint("box", 9000, false)), Endpoint.candidates("box:9000"))
        assertEquals(listOf(Endpoint("fd7a::1", 4483, true), Endpoint("fd7a::1", 4483, false)), Endpoint.candidates("[fd7a::1]:4483"))
    }

    @Test fun refusesWhatIsNotAServer() {
        for (a in listOf("", "ftp://x", "https://root@x.ts.net", "https://x/path", "https://x?q=1", "a/b", "root@box", "box:", "box:99999", "two words")) {
            assertEquals(a, emptyList<Endpoint>(), Endpoint.candidates(a))
        }
        assertNull(Endpoint.parse("https://"))
    }

    @Test fun hostHeaderDropsTheDefaultPort() {
        assertEquals("box.ts.net", Endpoint("box.ts.net", 443, true).hostHeader)
        assertEquals("box:4483", Endpoint("box", 4483, false).hostHeader)
        assertEquals("[fd7a::1]:4483", Endpoint("fd7a::1", 4483, true).hostHeader)
        assertEquals("http://10.0.2.2:4490", Endpoint("10.0.2.2", 4490, false).url)
    }

    @Test fun reconnectBacksOffAfterARound() {
        val p = ReconnectPlan(3)
        assertEquals(0L, p.failed()); assertEquals(0L, p.failed())
        assertEquals(500L, p.failed()); assertEquals(0, p.index)
        assertEquals(0L, p.failed()); assertEquals(0L, p.failed())
        assertEquals(1000L, p.failed())
        repeat(10) { p.lost() }
        assertEquals(10_000L, p.lost())
        p.connected()
        assertEquals(500L, p.lost())
    }
}
