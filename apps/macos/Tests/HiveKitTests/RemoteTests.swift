import Foundation
import Testing
@testable import HiveKit

@Suite struct ServerEndpointTests {
    @Test func parsesServerURLs() {
        #expect(ServerEndpoint(url: "https://box.example.ts.net") == .tcp(host: "box.example.ts.net", port: 443, tls: true))
        #expect(ServerEndpoint(url: "http://100.64.0.1:4483") == .tcp(host: "100.64.0.1", port: 4483, tls: false))
        #expect(ServerEndpoint(url: "https://box.example.ts.net/") == .tcp(host: "box.example.ts.net", port: 443, tls: true))
        #expect(ServerEndpoint(url: "http://[fd7a:115c:a1e0::1]:4483") == .tcp(host: "fd7a:115c:a1e0::1", port: 4483, tls: false))
        // Not a server URL: accounts, paths, other schemes.
        #expect(ServerEndpoint(url: "https://root@box.example.ts.net") == nil)
        #expect(ServerEndpoint(url: "https://box/v1") == nil)
        #expect(ServerEndpoint(url: "ftp://box") == nil)
        #expect(ServerEndpoint(url: "box.example.ts.net") == nil)
    }

    /// A saved host is tried where the CLI tries it.
    @Test func candidates() {
        #expect(ServerEndpoint.candidates(for: "https://box.ts.net") == [.tcp(host: "box.ts.net", port: 443, tls: true)])
        #expect(ServerEndpoint.candidates(for: "box.ts.net") == [.tcp(host: "box.ts.net", port: 443, tls: true),
                                                                  .tcp(host: "box.ts.net", port: 4483, tls: true),
                                                                  .tcp(host: "box.ts.net", port: 4483, tls: false)])
        #expect(ServerEndpoint.candidates(for: "box:9000") == [.tcp(host: "box", port: 9000, tls: true), .tcp(host: "box", port: 9000, tls: false)])
        #expect(ServerEndpoint.candidates(for: "fd7a:115c:a1e0::1").count == 3)
        #expect(ServerEndpoint.candidates(for: "fd7a:115c:a1e0::1")[0] == .tcp(host: "fd7a:115c:a1e0::1", port: 443, tls: true))
        #expect(ServerEndpoint.candidates(for: "[fd7a::1]:4483") == [.tcp(host: "fd7a::1", port: 4483, tls: true), .tcp(host: "fd7a::1", port: 4483, tls: false)])
        #expect(ServerEndpoint.candidates(for: "root@box").isEmpty)
        #expect(ServerEndpoint.candidates(for: "https://root@box").isEmpty)
    }

    /// The Host header names the server as it's addressed: Tailscale Serve
    /// matches on it, and the server refuses names it isn't (rebinding).
    @Test func hostHeader() {
        #expect(ServerEndpoint(url: "https://box.example.ts.net")!.hostHeader == "box.example.ts.net")
        #expect(ServerEndpoint(url: "http://100.64.0.1:4483")!.hostHeader == "100.64.0.1:4483")
        #expect(ServerEndpoint(url: "http://[fd7a::1]:4483")!.hostHeader == "[fd7a::1]:4483")
        #expect(ServerEndpoint.unix(path: "/tmp/s.sock").hostHeader == "hive")
        let req = String(decoding: WebSocketHandshake.request(path: "/v1/attach", host: "box.example.ts.net", key: "k"), as: UTF8.self)
        #expect(req.contains("\r\nHost: box.example.ts.net\r\n"))
    }

    /// A refused upgrade says why: the server's message, not "HTTP/1.1 403".
    @Test func refusalSaysWhy() {
        let resp = Data(("HTTP/1.1 403 Forbidden\r\nContent-Type: application/json\r\n\r\n"
            + #"{"error":{"status":403,"code":"forbidden","message":"tailscale:bob@github may not use this server"}}"#).utf8)
        #expect(throws: WebSocketError.handshake("tailscale:bob@github may not use this server")) {
            _ = try WebSocketHandshake.parseResponse(resp, key: "k")
        }
    }
}

/// A real server listening on TCP as well as its socket.
final class TCPTestServer {
    let server: TestServer
    let port: UInt16

    init() throws {
        // A free port: bind, read it, close.
        let fd = socket(AF_INET, SOCK_STREAM, 0)
        var addr = sockaddr_in(sin_len: UInt8(MemoryLayout<sockaddr_in>.size), sin_family: sa_family_t(AF_INET), sin_port: 0,
                               sin_addr: in_addr(s_addr: inet_addr("127.0.0.1")), sin_zero: (0, 0, 0, 0, 0, 0, 0, 0))
        var len = socklen_t(MemoryLayout<sockaddr_in>.size)
        _ = withUnsafeMutablePointer(to: &addr) { $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { bind(fd, $0, len) } }
        _ = withUnsafeMutablePointer(to: &addr) { $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { getsockname(fd, $0, &len) } }
        close(fd)
        port = UInt16(bigEndian: addr.sin_port)
        server = try TestServer(extra: ["--extra-listen", "127.0.0.1:\(port)", "--allow-anonymous"])
        for _ in 0..<200 {
            let c = socket(AF_INET, SOCK_STREAM, 0)
            var a = sockaddr_in(sin_len: UInt8(MemoryLayout<sockaddr_in>.size), sin_family: sa_family_t(AF_INET), sin_port: port.bigEndian,
                                sin_addr: in_addr(s_addr: inet_addr("127.0.0.1")), sin_zero: (0, 0, 0, 0, 0, 0, 0, 0))
            let ok = withUnsafeMutablePointer(to: &a) { $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { Darwin.connect(c, $0, len) == 0 } }
            close(c)
            if ok { break }
            usleep(20_000)
        }
    }

    func stop() { server.stop() }
}

@Suite(.serialized) struct RemoteClientTests {
    /// The app's client over TCP: hello, calls, and a block's terminal
    /// state, as over the socket. The server knows it came in over the
    /// network.
    @Test func clientOverTCP() async throws {
        let s = try TCPTestServer(); defer { s.stop() }
        let h = Harness(endpoint: .tcp(host: "127.0.0.1", port: s.port, tls: false))
        _ = try await h.connect()
        let me = try await h.call("whoami")
        #expect(me["principal"].string == "anonymous")
        #expect(me["via"].string == "plain")
        let created = try await h.call("session.create", ["command": ["/bin/sh", "-c", "echo over-tcp; exec cat"]])
        let block = created["block_ids"][0].string ?? ""
        let u = Updates()
        h.queue.async { h.client.subscribe(block: block) { u.add($0) } }
        let gotSnapshot = await u.wait { $0.contains { if case .snapshot = $0 { true } else { false } } }
        #expect(gotSnapshot)
    }

    /// Nothing listening: the connection fails at once, it doesn't wait
    /// for the network to come back.
    @Test func refusedFailsAtOnce() async throws {
        let h = Harness(endpoint: .tcp(host: "127.0.0.1", port: 1, tls: false))
        let start = Date()
        await #expect(throws: (any Error).self) { _ = try await h.connect() }
        #expect(Date().timeIntervalSince(start) < 3)
    }
}

@Suite struct ReconnectPlanTests {
    /// A bare host that's down: its three candidates at once, then a pause
    /// that grows. Never a loop without a pause.
    @Test func roundsBackOff() {
        var p = ReconnectPlan(candidates: 3)
        var delays: [TimeInterval] = []
        for _ in 0..<9 { delays.append(p.failed()) }
        #expect(delays == [0, 0, 0.5, 0, 0, 1, 0, 0, 2])
        for _ in 0..<30 { _ = p.failed() }
        #expect(p.backoff == 10)
    }

    @Test func singleCandidateBacksOffEachTime() {
        var p = ReconnectPlan(candidates: 1)
        #expect([p.failed(), p.failed(), p.failed()] == [0.5, 1, 2])
    }

    @Test func connectingStartsOver() {
        var p = ReconnectPlan(candidates: 1)
        _ = p.failed(); _ = p.failed()
        p.connected()
        #expect(p.lost() == 0.5)
    }
}
