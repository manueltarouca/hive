package xyz.matm.hive.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class HostsTest {
    @Test fun labelsFollowTheServersRules() {
        for (ok in listOf("box", "lab-vm", "hive_2", "BOX")) assertNull(ok, Hosts.checkLabel(ok))
        for (bad in listOf("", "a/b", "box.ts", "root@box", "--always", "-x", "~me", ".x", "a b", "a:b")) {
            assertNotNull(bad, Hosts.checkLabel(bad))
        }
    }

    @Test fun addingAgainReplacesAndKeepsTheChoice() {
        var hs = Hosts.add(emptyList(), Host("box", "https://box.ts.net", Host.ALWAYS))
        hs = Hosts.add(hs, Host("BOX", "https://box.example.ts.net"))
        assertEquals(1, hs.size)
        assertTrue(hs[0].always)
        assertEquals("https://box.example.ts.net", hs[0].address)
        hs = Hosts.add(hs, Host("box", "https://box.example.ts.net", Host.ON_DEMAND))
        assertFalse(hs[0].always)
    }

    @Test fun hostsJsonIsTheMacsSchema() {
        val hs = listOf(Host("box", "https://box.ts.net", Host.ALWAYS), Host("lab", "lab"))
        val json = Hosts.toJson(hs)
        assertTrue(json, json.contains("\"version\":1"))
        assertFalse("on demand is saved as no field", json.contains("on-demand"))
        assertEquals(hs, Hosts.fromJson(json))
        assertEquals(emptyList<Host>(), Hosts.fromJson("not json"))
    }

    @Test fun theMacsCodeRoundTrips() {
        val hs = listOf(Host("box", "https://box.example.ts.net", Host.ALWAYS), Host("lab", "http://100.101.2.3:4483"))
        val code = HostsCode.encode(hs)
        assertTrue(code, code.startsWith("hive://hosts?d="))
        val offer = HostsCode.decode(code)!!
        assertEquals(listOf("box", "lab"), offer.map { it.host.label })
        assertTrue("a code never makes a host always", offer.none { it.host.always })
        assertTrue(offer.all { it.inTailnet })
    }

    @Test fun aCodeIsUntrusted() {
        assertNull(HostsCode.decode("https://example.com/?d=xx"))
        assertNull(HostsCode.decode("hive://hosts?d=%%%"))
        assertNull(HostsCode.decode("hive://hosts?d=" + "A".repeat(20_000)))
        val code = HostsCode.encode(listOf(Host("ok", "box.example.ts.net"), Host("../x", "https://evil"), Host("evil", "ftp://evil"), Host("far", "http://203.0.113.9:4483")))
        val offer = HostsCode.decode(code)!!
        assertEquals(listOf("ok", "far"), offer.map { it.host.label })
        assertTrue(offer[0].inTailnet)
        assertFalse("an address outside the tailnet is flagged", offer[1].inTailnet)
    }

    @Test fun tailnetAddresses() {
        for (a in listOf("box.example.ts.net", "https://box.example.ts.net", "100.64.0.1", "http://100.127.255.254:4483", "[fd7a:115c:a1e0::1]:4483", "box")) {
            assertTrue(a, HostsCode.inTailnet(a))
        }
        for (a in listOf("203.0.113.9", "http://100.128.0.1", "https://example.com", "10.0.2.2:4490", "100.63.255.255")) {
            assertFalse(a, HostsCode.inTailnet(a))
        }
    }

    @Test fun aNewHostIsNamedAsOnTheMac() {
        assertEquals("box", Hosts.newLabel("box.example.ts.net", emptyList()))
        assertEquals("box", Hosts.newLabel("https://box.example.ts.net", emptyList()))
        assertEquals("10-0-2-2", Hosts.newLabel("http://10.0.2.2:4490", emptyList()))
        assertEquals("fd7a--1", Hosts.newLabel("[fd7a::1]:4483", emptyList()))
        assertEquals("box-3", Hosts.newLabel("box:9000", listOf("box", "BOX-2")))
        assertNull(Hosts.checkLabel(Hosts.newLabel("https://-x.example", emptyList())))
    }

    @Test fun whatWasTriedReadsAsSentence() {
        assertEquals("Tried HTTPS on 443 and 4483, and HTTP on 4483.", Hosts.tried(listOf("https://box", "https://box:4483", "http://box:4483")))
        assertEquals("Tried HTTP on 4490.", Hosts.tried(listOf("http://10.0.2.2:4490")))
    }

    /** The Mac makes the code (HiveKit/HostsCode.swift); this is one it made, byte for byte. */
    @Test fun decodesTheMacsOwnCode() {
        val mac = "hive://hosts?d=eyJob3N0cyI6W3siYWRkcmVzcyI6Imh0dHBzOi8vYm94LmV4YW1wbGUudHMubmV0IiwibGFiZWwiOiJib3gifSx7ImFkZHJlc3MiOiJodHRwOi8vMTAwLjEwMS4yLjM6NDQ4MyIsImxhYmVsIjoic3R1ZGlvIn1dLCJ2IjoxfQ"
        val offer = HostsCode.decode(mac)!!
        assertEquals(listOf(Host("box", "https://box.example.ts.net"), Host("studio", "http://100.101.2.3:4483")), offer.map { it.host })
    }

    @Test fun serversAreKnownByTheirIDFirst() {
        fun v(s: String) = org.json.JSONObject(s)
        // Two machines both called ubuntu, same user: their IDs tell them apart.
        val a = Hosts.identity(v("""{"server_id":"aaaaaaaaaaaaaaaa","hostname":"ubuntu","state_dir":"/home/ubuntu/.local/state/hive"}"""))
        val b = Hosts.identity(v("""{"server_id":"bbbbbbbbbbbbbbbb","hostname":"ubuntu","state_dir":"/home/ubuntu/.local/state/hive"}"""))
        assertTrue(a != b)
        assertEquals("id:aaaaaaaaaaaaaaaa", Hosts.identity(v("""{"server_id":"aaaaaaaaaaaaaaaa","tailscale":{"dns_name":"box.ts.net"}}""")))
        // Older servers: the tailnet name, else hostname and state.
        assertEquals("ts:box.ts.net", Hosts.identity(v("""{"tailscale":{"dns_name":"box.ts.net"},"hostname":"box"}""")))
        assertEquals("box|/s", Hosts.identity(v("""{"hostname":"box","state_dir":"/s"}""")))
        assertNull(Hosts.identity(v("{}")))
    }
}
