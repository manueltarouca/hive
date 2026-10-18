import Foundation
import Network

/// Where a hive server is: its unix socket, or a URL on the network
/// (https://box.example.ts.net, http://100.64.0.1:4483).
public enum ServerEndpoint: Equatable, Sendable, CustomStringConvertible {
    case unix(path: String)
    case tcp(host: String, port: UInt16, tls: Bool)

    /// A server URL: https or http, a host, an optional port, no path.
    public init?(url s: String) {
        guard let u = URLComponents(string: s), let scheme = u.scheme?.lowercased(), scheme == "https" || scheme == "http",
              let host = u.host, !host.isEmpty, u.user == nil, u.password == nil,
              u.path.trimmingCharacters(in: CharacterSet(charactersIn: "/")).isEmpty, u.query == nil
        else { return nil }
        let tls = scheme == "https"
        let port = u.port ?? (tls ? 443 : 80)
        guard (1...65535).contains(port) else { return nil }
        self = .tcp(host: host.trimmingCharacters(in: CharacterSet(charactersIn: "[]")), port: UInt16(port), tls: tls)
    }

    /// hive's port for tailnet and plain listeners.
    public static let defaultPort: UInt16 = 4483

    /// Where a saved host is tried, in order, as the CLI does: a URL as it
    /// is; a bare host at https:443, then https and http on 4483; HOST:PORT
    /// with https, then http.
    public static func candidates(for address: String) -> [ServerEndpoint] {
        if address.contains("://") { return ServerEndpoint(url: address).map { [$0] } ?? [] }
        guard !address.isEmpty, !address.contains("/"), !address.contains("@") else { return [] }
        var host = address, port: UInt16?
        if address.hasPrefix("["), let close = address.firstIndex(of: "]") {
            host = String(address[address.index(after: address.startIndex)..<close])
            let rest = address[address.index(after: close)...]
            if rest.hasPrefix(":") { port = UInt16(rest.dropFirst()) }
        } else if address.filter({ $0 == ":" }).count == 1, let colon = address.firstIndex(of: ":") {
            host = String(address[..<colon])
            port = UInt16(address[address.index(after: colon)...])
            if port == nil { return [] }
        }
        if let port { return [.tcp(host: host, port: port, tls: true), .tcp(host: host, port: port, tls: false)] }
        return [.tcp(host: host, port: 443, tls: true), .tcp(host: host, port: defaultPort, tls: true), .tcp(host: host, port: defaultPort, tls: false)]
    }

    /// The Host header: the name the server answers to (Tailscale Serve
    /// matches on it, and the server checks it against DNS rebinding).
    var hostHeader: String {
        switch self {
        case .unix: return "hive"
        case let .tcp(host, port, tls):
            let h = host.contains(":") ? "[\(host)]" : host
            return (tls && port == 443) || (!tls && port == 80) ? h : "\(h):\(port)"
        }
    }

    public var description: String {
        switch self {
        case .unix(let path): return path
        case let .tcp(_, _, tls): return "\(tls ? "https" : "http")://\(hostHeader)"
        }
    }
}

/// A WebSocket connection to hive's attach endpoint. Everything happens on
/// `queue`, which the caller owns; callbacks run there.
///
/// Over the network a connection can die without closing (sleep, a Wi-Fi
/// change, a path through a relay): it pings every 20 s and gives up when
/// nothing has arrived for 45 s, and a connection that can't be made
/// (`waiting`) gets 15 s before it counts as failed.
final class WebSocketConnection {
    let queue: DispatchQueue
    let endpoint: ServerEndpoint
    private let conn: NWConnection
    private let path: String
    private var key = WebSocketHandshake.newKey()
    private var upgraded = false
    private var pending = Data()
    private var parser = WebSocketParser()
    private var closed = false
    private var lastHeard = Date()
    private var keepalive: DispatchSourceTimer?
    private var waitingSince: Date?

    var onOpen: (() -> Void)?
    var onMessage: ((WebSocketMessage) -> Void)?
    var onClose: ((Error?) -> Void)?

    static let pingEvery: TimeInterval = 20
    static let silentLimit: TimeInterval = 45
    static let waitingLimit: TimeInterval = 15

    init(endpoint: ServerEndpoint, path: String = "/v1/attach", queue: DispatchQueue) {
        self.queue = queue
        self.path = path
        self.endpoint = endpoint
        switch endpoint {
        case .unix(let socketPath):
            conn = NWConnection(to: .unix(path: socketPath), using: .tcp)
        case let .tcp(host, port, tls):
            let tcp = NWProtocolTCP.Options()
            tcp.noDelay = true // keystrokes go out at once
            tcp.enableKeepalive = true
            tcp.keepaliveIdle = 10
            let params: NWParameters
            if tls {
                let t = NWProtocolTLS.Options()
                // HTTP/1.1 only: the attach stream is an HTTP/1.1 upgrade.
                sec_protocol_options_add_tls_application_protocol(t.securityProtocolOptions, "http/1.1")
                params = NWParameters(tls: t, tcp: tcp)
            } else {
                params = NWParameters(tls: nil, tcp: tcp)
            }
            conn = NWConnection(host: NWEndpoint.Host(host), port: NWEndpoint.Port(rawValue: port)!, using: params)
        }
    }

    func start() {
        conn.stateUpdateHandler = { [weak self] state in
            guard let self else { return }
            switch state {
            case .ready:
                self.waitingSince = nil
                self.lastHeard = Date()
                self.conn.send(content: WebSocketHandshake.request(path: self.path, host: self.endpoint.hostHeader, key: self.key),
                               completion: .contentProcessed { _ in })
                self.receive()
                if case .tcp = self.endpoint { self.startKeepalive() }
            case .waiting(let e):
                // The unix socket has nobody listening; a network path may
                // come back (Network retries), but not forever.
                if case .unix = self.endpoint { self.finish(e); return }
                // Refused: nothing listens there; the caller's backoff retries.
                if case .posix(.ECONNREFUSED) = e { self.finish(e); return }
                if self.waitingSince == nil {
                    self.waitingSince = Date()
                    self.queue.asyncAfter(deadline: .now() + WebSocketConnection.waitingLimit) { [weak self] in
                        guard let self, let since = self.waitingSince,
                              Date().timeIntervalSince(since) >= WebSocketConnection.waitingLimit - 0.01 else { return }
                        self.finish(e)
                    }
                }
            case .failed(let e):
                self.finish(e)
            case .cancelled:
                self.finish(nil)
            default: break
            }
        }
        conn.start(queue: queue)
    }

    private func startKeepalive() {
        let t = DispatchSource.makeTimerSource(queue: queue)
        t.schedule(deadline: .now() + WebSocketConnection.pingEvery, repeating: WebSocketConnection.pingEvery)
        t.setEventHandler { [weak self] in
            guard let self, !self.closed else { return }
            if Date().timeIntervalSince(self.lastHeard) > WebSocketConnection.silentLimit {
                self.finish(WebSocketError.protocolViolation("no answer from the server for \(Int(WebSocketConnection.silentLimit)) s"))
                return
            }
            if self.upgraded { self.write(opcode: 0x9, Data()) }
        }
        t.resume()
        keepalive = t
    }

    private func receive() {
        conn.receive(minimumIncompleteLength: 1, maximumLength: 1 << 20) { [weak self] data, _, isComplete, error in
            guard let self, !self.closed else { return }
            if let data, !data.isEmpty {
                self.lastHeard = Date()
                self.ingest(data)
            }
            if let error { self.finish(error); return }
            if isComplete { self.finish(nil); return }
            self.receive()
        }
    }

    private func ingest(_ data: Data) {
        do {
            var bytes = data
            if !upgraded {
                pending.append(data)
                let (done, rest) = try WebSocketHandshake.parseResponse(pending, key: key)
                guard done else { return }
                upgraded = true
                pending = Data()
                onOpen?()
                bytes = rest
            }
            for msg in try parser.feed(bytes) {
                switch msg {
                case .ping(let p): write(opcode: 0xA, p)
                case .pong: break // lastHeard is already fresh
                case .close(let code):
                    write(opcode: 0x8, code.map { Data([UInt8($0 >> 8), UInt8($0 & 0xff)]) } ?? Data())
                    finish(nil)
                    return
                default: onMessage?(msg)
                }
            }
        } catch {
            finish(error)
        }
    }

    private func write(opcode: UInt8, _ payload: Data) {
        guard !closed else { return }
        conn.send(content: WebSocketParser.encode(opcode: opcode, payload: payload), completion: .contentProcessed { _ in })
    }

    func send(text: Data) { write(opcode: 0x1, text) }
    func send(binary: Data) { write(opcode: 0x2, binary) }

    func close() {
        guard !closed else { return }
        write(opcode: 0x8, Data([0x03, 0xe8]))
        finish(nil)
    }

    private func finish(_ error: Error?) {
        guard !closed else { return }
        closed = true
        keepalive?.cancel()
        keepalive = nil
        conn.cancel()
        onClose?(error)
    }
}

/// When to try a server again. Candidates of one round (a bare host's
/// https:443, https:4483, http:4483) follow each other at once; after a
/// round fails, or a connection is lost, it backs off: 0.5 s, 1 s, 2 s…
/// up to 10 s. A connection that came up starts the backoff over.
public struct ReconnectPlan: Equatable, Sendable {
    public let candidates: Int
    public private(set) var index = 0
    public private(set) var backoff: TimeInterval = 0.5

    public init(candidates: Int) { self.candidates = max(1, candidates) }

    /// A connection attempt failed: the next candidate, and how long to wait.
    public mutating func failed() -> TimeInterval {
        if index + 1 < candidates {
            index += 1
            return 0
        }
        index = 0
        return bump()
    }

    /// An established connection was lost: the same candidate, after the backoff.
    public mutating func lost() -> TimeInterval { bump() }

    public mutating func connected() { backoff = 0.5 }

    private mutating func bump() -> TimeInterval {
        let d = backoff
        backoff = min(backoff * 2, 10)
        return d
    }
}
