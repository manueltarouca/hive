import Foundation
import HiveCore
import HiveKit

/// A server the app talks to: this Mac's, or a remote host. Each has its own
/// client and engine (on the app's one vt queue), its connection state, and
/// its sessions and agents. Reconnects on its own, with a fresh client and
/// engine, so nothing is stale afterwards.
@MainActor
final class HostConnection {
    nonisolated static let localID = "local"
    /// A saved host's ID: never the local one's, whatever its label.
    nonisolated static func remoteID(_ label: String) -> String { "host:" + label }

    /// "local", or the label the host was saved under.
    let id: String
    /// What the app shows: this Mac's name, or the label.
    let label: String
    /// Where it's tried, in order: one for a URL, three for a bare host
    /// (https:443, then https and http on 4483). The one that answered is
    /// kept for reconnects.
    private(set) var endpoints: [ServerEndpoint]
    private var plan: ReconnectPlan
    var endpoint: ServerEndpoint { endpoints[plan.index] }
    /// A bare host connected at one candidate: it stays there (never back
    /// down to plain HTTP), and the model saves it as the host's URL.
    var onPinned: ((HostConnection, ServerEndpoint) -> Void)?
    var isLocal: Bool { id == HostConnection.localID }
    /// Its server's name and the account's home (server.version), once
    /// connected. Home shortens paths to ~ as that machine's shell does.
    private(set) var hostname: String?
    private(set) var home: String?
    /// What the app calls it: This Mac, or the remote server's address
    /// (box.example.ts.net), as the session list and title bar show it.
    var displayName: String {
        if isLocal { return "This Mac" }
        guard case let .tcp(host, port, tls) = endpoints[0] else { return label }
        // The port only when it isn't the one you'd assume: two servers on
        // one machine stay apart.
        let usual = port == ServerEndpoint.defaultPort || (tls && port == 443) || (!tls && port == 80)
        let h = host.contains(":") ? "[\(host)]" : host
        return usual ? h : "\(h):\(port)"
    }

    /// How the connection stands.
    enum Link: Equatable {
        /// Saved, not connected: an on-demand host nobody picked yet.
        case idle
        case connecting, connected, reconnecting, refused(String), noServer, silent, failed(String)
    }
    private(set) var link = Link.idle
    /// Whether this run has reached the host yet: until it has, a failure
    /// is "can't reach", not "reconnecting".
    private(set) var everConnected = false
    var refused: Bool { if case .refused = link { return true }; return false }

    /// Why it isn't connected, as the title bar says it in place of the
    /// host's name: what you experience (it keeps trying), naming the host.
    /// nil while connected. The cause is in `detail`.
    var problem: String? {
        let n = displayName
        switch link {
        case .connected, .idle: return nil
        case .connecting: return "Connecting to \(n)…"
        case .refused: return "\(n) refused this Mac"
        case .reconnecting:
            return isLocal ? "Reconnecting to the hive server…" : "Reconnecting to \(n)…"
        case .noServer, .silent, .failed:
            if !everConnected { return isLocal ? "Can't reach the hive server" : "Can't reach \(n)" }
            return isLocal ? "Reconnecting to the hive server…" : "Reconnecting to \(n)…"
        }
    }

    /// The same, under the host's own header in the session list: no need
    /// to name it again.
    var shortProblem: String? {
        switch link {
        case .connected, .idle: return nil
        case .connecting: return "Connecting…"
        case .reconnecting: return "Reconnecting…"
        case .refused: return "Refused this Mac"
        case .noServer: return "No hive server running"
        case .silent: return "Not answering"
        case .failed: return "Can't connect"
        }
    }

    /// The cause in full, for a tooltip.
    var detail: String? {
        let at = endpoint.description
        switch link {
        case .refused(let m): return m
        case .failed(let m): return "\(m) (\(at)). hive keeps trying."
        case .noServer:
            // This Mac's server isn't restarted for you once it's stopped.
            if isLocal { return "No hive server answers at \(at). Start it with hive server start." }
            return "No hive server answers at \(at). hive keeps trying."
        case .silent: return "\(at) doesn't answer. hive keeps trying."
        case .reconnecting: return "The connection to \(at) dropped. hive keeps trying."
        default: return nil
        }
    }

    let vt: DispatchQueue
    private(set) var client: HiveClient
    private(set) var engine: Engine
    private(set) var clientID: String?
    private(set) var connected = false
    private(set) var status = "Connecting…"

    var sessions: [SessionInfo] = []
    var agents: [AgentInfo] = []
    /// Each agent's state at the last reload, for notifications.
    var lastAgentStates: [String: String] = [:]

    /// Carried into every engine, the ones reconnects make too.
    var theme: Theme = .ghostty {
        didSet { let e = engine, t = theme; vt.async { e.theme = t } }
    }
    var cursorBlink: Bool? {
        didSet { let e = engine, b = cursorBlink; vt.async { e.cursorBlink = b } }
    }

    /// The model's: connected (after hello), events (one batch, and whether
    /// it overflowed), a gap to re-read after, and a lost connection.
    var onConnected: ((HostConnection) -> Void)?
    var onEvents: ((HostConnection, [JSON], Bool) -> Void)?
    var onResync: ((HostConnection) -> Void)?
    var onDisconnected: ((HostConnection) -> Void)?

    private var closing = false
    /// A connection is wanted: always-on, or picked. False for an on-demand
    /// host until it's used, and again after Disconnect.
    private(set) var active = false
    /// hosts.json says always: connect at launch, and when switched to it.
    var savedAlways = false

    /// Connects (an on-demand host that was picked, or an always one).
    func start() {
        guard !active else { return }
        active = true
        closing = false
        everConnected = false
        link = .connecting
        status = problem ?? ""
        connect()
    }

    /// Disconnects and stays so until started again. Its sessions and
    /// agents go with the connection.
    func stop() {
        guard active else { return }
        active = false
        closing = true
        let client = client
        vt.async { client.close() }
        // A fresh, unconnected client and engine: whatever the stopped one
        // still delivers (its close, a late reply, events) is ignored.
        self.client = HiveClient(endpoint: endpoint, queue: vt)
        engine = Engine(client: self.client)
        connected = false
        link = .idle
        status = ""
        sessions = []
        agents = []
        lastAgentStates = [:]
    }
    private var nextDelay: TimeInterval = 0.5

    /// The saved address became this one endpoint (the URL it answered at).
    func pin(to e: ServerEndpoint) {
        guard endpoints.contains(e) else { return }
        endpoints = [e]
        plan = ReconnectPlan(candidates: 1)
    }

    init(id: String, label: String, endpoints: [ServerEndpoint], vt: DispatchQueue) {
        precondition(!endpoints.isEmpty)
        self.id = id
        self.label = label
        self.endpoints = endpoints
        plan = ReconnectPlan(candidates: endpoints.count)
        self.vt = vt
        client = HiveClient(endpoint: endpoints[0], queue: vt)
        engine = Engine(client: client)
    }

    convenience init(id: String, label: String, endpoint: ServerEndpoint, vt: DispatchQueue) {
        self.init(id: id, label: label, endpoints: [endpoint], vt: vt)
    }

    static func endpoints(for address: String) -> [ServerEndpoint] { ServerEndpoint.candidates(for: address) }

    /// Connects, or reconnects with a fresh client and engine.
    func connect() {
        guard !closing else { return }
        client = HiveClient(endpoint: endpoint, queue: vt)
        engine = Engine(client: client)
        let engine = engine, theme = theme, blink = cursorBlink
        vt.async { [weak self] in
            engine.theme = theme
            engine.cursorBlink = blink
            engine.log = { msg in NSLog("hive: %@", msg) }
            // Some events were missed: re-read everything.
            engine.client.onEventsResync = { Task { @MainActor in if let self { self.onResync?(self) } } }
            // One hop to the main thread per batch, not per event.
            let batch = EventBatch()
            engine.client.onEvent = { ev in
                if batch.add(ev) {
                    Task { @MainActor in
                        guard let self else { return }
                        let (events, overflowed) = batch.takeAll()
                        guard self.engine === engine else { return }
                        self.onEvents?(self, events, overflowed)
                    }
                }
            }
            engine.client.onClose = { _ in
                Task { @MainActor in
                    guard let self, self.engine === engine else { return } // a replaced engine's
                    self.disconnected()
                }
            }
            engine.client.onError = { kind, block, err in NSLog("hive error (%@ %@): %@", kind ?? "-", block ?? "-", err.description) }
            engine.client.connect(name: "Hive.app", version: AppModel.version) { result in
                Task { @MainActor in
                    guard let self, self.engine === engine else { return }
                    switch result {
                    case .success(let id):
                        self.plan.connected()
                        if self.endpoints.count > 1 {
                            let e = self.endpoint
                            self.pin(to: e)
                            self.onPinned?(self, e)
                        }
                        self.clientID = id
                        self.vt.async { engine.client.subscribeEvents(all: true) }
                        // Its home first: the first view shortens paths with it.
                        self.call("server.version") { [weak self] j in
                            guard let self, self.engine === engine else { return }
                            self.hostname = j["hostname"].string
                            self.home = j["home"].string
                            self.link = .connected
                            self.everConnected = true
                            self.connected = true
                            self.status = "Connected"
                            self.onConnected?(self)
                        }
                    case .failure(let e):
                        // The next candidate at once, or a pause after a round.
                        self.nextDelay = self.plan.failed()
                        self.link = HostConnection.link(for: e)
                        self.status = self.problem ?? ""
                    }
                }
            }
        }
    }

    /// What a failed connection means: refused (the server's words),
    /// nothing listening, silence, or something else.
    static func link(for e: Error) -> Link {
        if case WebSocketError.handshake(let m) = e { return m.contains("may not use") ? .refused(m) : .failed(m) }
        let d = "\(e)"
        if d.contains("refused") || d.contains("ECONNREFUSED") { return .noServer }
        if d.contains("timed out") || d.contains("no answer") { return .silent }
        return .failed(d)
    }

    private func disconnected() {
        // A connection that was up: reconnecting. One that never came up
        // keeps its reason (refused, nothing answers).
        let wasUp = connected
        if wasUp { link = .reconnecting; status = problem ?? "" }
        connected = false
        onDisconnected?(self)
        guard !closing else { return }
        // A lost connection backs off; a failed attempt already chose.
        let delay = wasUp ? plan.lost() : nextDelay
        DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in self?.connect() }
    }

    /// Stops for good (the host was removed).
    func close() {
        active = false
        closing = true
        let client = client
        vt.async { client.close() }
    }

    func call(_ method: String, _ params: JSON = [:], then: (@MainActor (JSON) -> Void)? = nil) {
        let client = client
        vt.async {
            client.call(method, params) { result in
                Task { @MainActor [weak self] in
                    // A reply for a connection since replaced or stopped.
                    guard let self, self.client === client else { return }
                    switch result {
                    case .success(let j): then?(j)
                    case .failure(let e): NSLog("hive %@: %@", method, e.description)
                    }
                }
            }
        }
    }
}
