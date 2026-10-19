import XCTest
@testable import HiveKit

final class HostsCodeTests: XCTestCase {
    /// The phone decodes exactly this (apps/android HostsTest.decodesTheMacsOwnCode).
    func testEncodesWhatThePhoneReads() {
        let code = HostsCode.encode([("box", "https://box.example.ts.net"), ("studio", "http://100.101.2.3:4483")])
        XCTAssertEqual(code, "hive://hosts?d=eyJob3N0cyI6W3siYWRkcmVzcyI6Imh0dHBzOi8vYm94LmV4YW1wbGUudHMubmV0IiwibGFiZWwiOiJib3gifSx7ImFkZHJlc3MiOiJodHRwOi8vMTAwLjEwMS4yLjM6NDQ4MyIsImxhYmVsIjoibmV3dG9uIn1dLCJ2IjoxfQ")
    }

    /// This Mac, when its server listens on the tailnet: Tailscale's own URL first, else a tailnet listener's address.
    func testThisMacsAddress() {
        XCTAssertEqual(HostsCode.thisMac(listeners: [("local", "/tmp/s.sock")], tailscaleURLs: []), nil)
        XCTAssertEqual(HostsCode.thisMac(listeners: [("local", "/tmp/s.sock"), ("tailnet", "100.101.2.3:4483")], tailscaleURLs: []),
                       "http://100.101.2.3:4483")
        XCTAssertEqual(HostsCode.thisMac(listeners: [("tailnet", "[fd7a:115c:a1e0::5]:4483")], tailscaleURLs: []),
                       "http://[fd7a:115c:a1e0::5]:4483")
        XCTAssertEqual(HostsCode.thisMac(listeners: [("tailnet", "100.101.2.3:4483")], tailscaleURLs: ["https://studio.example.ts.net", "http://studio.example.ts.net:4483"]),
                       "https://studio.example.ts.net")
        // A plain listener is no address for a phone: anyone could pose as it.
        XCTAssertEqual(HostsCode.thisMac(listeners: [("plain", "0.0.0.0:9000")], tailscaleURLs: []), nil)
    }
}
