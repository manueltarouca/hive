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
        if case let .tcp(host, _, _) = endpoints[0] { return host }
        return label
    }

    /// How the connection stands.
    enum Link: Equatable {
        /// Saved, not connected: an on-demand host nobody picked yet.
        case idle
        case connecting, connected, reconnecting, refused(String), noServer, silent, failed(String)
    }
    private(set) var link = Link.idle

    /// Why it isn't connected, as a sentence naming the host; nil while
    /// connected. The details (the server's words) are in `detail`.
    var problem: String? {
        let n = displayName
        switch link {
        case .connected, .idle: return nil
        case .connecting: return "Connecting to \(n)…"
        case .reconnecting: return "Reconnecting to \(n)…"
        case .refused: return "\(n) refused this Mac"
        case .noServer: return isLocal ? "The hive server isn't running" : "No hive server on \(n)"
        case .silent: return "\(n) isn't answering"
        case .failed: return "Can't reach \(n)"
        }
    }

    /// The reason in full, for a tooltip.
    var detail: String? {
        switch link {
        case .refused(let m), .failed(let m): return m
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
