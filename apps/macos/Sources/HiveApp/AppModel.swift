import AppKit
import Foundation
import HiveCore
import HiveKit
import UserNotifications

/// A window's layout tree, as the server sends it.
indirect enum Layout: Equatable {
    case block(String)
    case split(horizontal: Bool, ratio: Double, a: Layout, b: Layout)

    init?(_ j: JSON) {
        switch j["type"].string {
        case "block":
            guard let id = j["block_id"].string else { return nil }
            self = .block(id)
        case "split":
            guard let a = Layout(j["a"]), let b = Layout(j["b"]) else { return nil }
            self = .split(horizontal: j["direction"].string != "vertical", ratio: j["ratio"].double ?? 0.5, a: a, b: b)
        default: return nil
        }
    }

    /// Each block's place, as fractions of the window.
    func rects(in r: CGRect = CGRect(x: 0, y: 0, width: 1, height: 1)) -> [(String, CGRect)] {
        switch self {
        case .block(let id): return [(id, r)]
        case let .split(horizontal, ratio, a, b):
            if horizontal {
                let w = r.width * ratio
                return a.rects(in: CGRect(x: r.minX, y: r.minY, width: w, height: r.height))
                    + b.rects(in: CGRect(x: r.minX + w, y: r.minY, width: r.width - w, height: r.height))
            }
            let h = r.height * ratio
            return a.rects(in: CGRect(x: r.minX, y: r.minY, width: r.width, height: h))
                + b.rects(in: CGRect(x: r.minX, y: r.minY + h, width: r.width, height: r.height - h))
        }
    }
}

struct WindowInfo: Identifiable, Equatable {
    let id: String
    var label: String
    var layout: Layout?
    var focused: String?
    var zoomed: Bool
}

struct SessionInfo: Identifiable, Equatable {
    let id: String
    var label: String
    var blocks: Int
    var agents: Int
}

struct AgentInfo: Identifiable, Equatable {
    var id: String { blockID }
    let blockID: String
    let sessionID: String
    var sessionLabel: String
    var harness: String
    var state: String
    var message: String
    var cwd: String
}

struct BlockInfo: Equatable {
    var title: String
    var label: String
    var cols: Int, rows: Int
    var sizeOwner: String?
    var foreground: String?
}

/// The app's state. Lives on the main thread; talks to the server through
/// the engine on the `hive.vt` queue.
@MainActor
final class AppModel: ObservableObject {
    let vt = DispatchQueue(label: "hive.vt", qos: .userInteractive)
    let client: HiveClient
    let engine: Engine
    let socket: String

    @Published var status = "Connecting…"
    @Published var connected = false
    @Published var sessions: [SessionInfo] = []
    @Published var agents: [AgentInfo] = []
    @Published var currentSession: String?
    @Published var sessionLabel = ""
    @Published var windows: [WindowInfo] = []
    @Published var activeWindow: String?
    @Published var blocks: [String: BlockInfo] = [:]
    var clientID: String?

    /// Frames by block, drawn by the terminal view.
    var frames: [String: PaneFrame] = [:]
    var onFrame: (() -> Void)?
    /// Runs after the layout changed, so the view can size the panes.
    var onLayout: (() -> Void)?
    private var attached: Set<String> = []
    private var reloadPending = false
    private var lastAgentStates: [String: String] = [:]

    init() {
        socket = AppModel.socketPath()
        client = HiveClient(socketPath: socket, queue: vt)
        engine = Engine(client: client)
    }

    static func socketPath() -> String {
        let env = ProcessInfo.processInfo.environment
        if let s = env["HIVE_SERVER"], !s.isEmpty { return s }
        if let d = env["XDG_RUNTIME_DIR"], !d.isEmpty { return d + "/hive/server.sock" }
        // Same as Go's os.TempDir() on macOS: $TMPDIR, the per-user temp dir.
        let tmp = (env["TMPDIR"] ?? "/tmp/").replacingOccurrences(of: "//", with: "/")
        return (tmp as NSString).appendingPathComponent("hive-\(getuid())/server.sock")
    }

    /// The hive binary: bundled in the app, or HIVE_BIN, or on PATH.
    static func hiveBinary() -> String? {
        if let p = Bundle.main.url(forResource: "hive", withExtension: nil)?.path, FileManager.default.isExecutableFile(atPath: p) { return p }
        if let p = ProcessInfo.processInfo.environment["HIVE_BIN"] { return p }
        for dir in (ProcessInfo.processInfo.environment["PATH"] ?? "").split(separator: ":") {
            let p = "\(dir)/hive"
            if FileManager.default.isExecutableFile(atPath: p) { return p }
        }
        return nil
    }

    // MARK: connection

    func start() {
        ensureServer()
        let engine = engine
        vt.async { [weak self] in
            engine.log = { msg in NSLog("hive: %@", msg) }
            engine.client.onEvent = { ev in Task { @MainActor in self?.handleEvent(ev) } }
            engine.client.onClose = { err in Task { @MainActor in self?.disconnected(err) } }
            engine.client.onError = { kind, block, err in NSLog("hive error (%@ %@): %@", kind ?? "-", block ?? "-", err.description) }
            engine.client.connect(name: "Hive.app", version: "0.1") { result in
                Task { @MainActor in
                    switch result {
                    case .success(let id):
                        self?.clientID = id
                        self?.connected = true
                        self?.status = "Connected"
                        self?.vt.async { engine.client.subscribeEvents(all: true) }
                        self?.reloadSessions(create: true)
                    case .failure(let e):
                        self?.status = "Can't reach the hive server: \(e)"
                    }
                }
            }
        }
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { _, _ in }
    }

    /// Starts the server if nothing listens on the socket.
    private func ensureServer() {
        guard !FileManager.default.fileExists(atPath: socket), let bin = AppModel.hiveBinary() else { return }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: bin)
        p.arguments = ["server", "start"]
        try? p.run()
        p.waitUntilExit()
    }

    private func disconnected(_ err: Error?) {
        connected = false
        status = "Disconnected\(err.map { ": \($0)" } ?? "")"
    }

    func call(_ method: String, _ params: JSON = [:], then: (@MainActor (JSON) -> Void)? = nil) {
        let client = client
        vt.async {
            client.call(method, params) { result in
                Task { @MainActor in
                    switch result {
                    case .success(let j): then?(j)
                    case .failure(let e): NSLog("hive %@: %@", method, e.description)
                    }
                }
            }
        }
    }

    // MARK: state

    func reloadSessions(create: Bool = false) {
        call("session.list") { [weak self] j in
            guard let self else { return }
            sessions = j["sessions"].array.compactMap { s in
                guard let id = s["session_id"].string else { return nil }
                return SessionInfo(id: id, label: s["label"].string ?? "", blocks: s["blocks"].int ?? 0, agents: s["agents"].int ?? 0)
            }
            if sessions.isEmpty, create {
                call("session.create", ["cwd": .string(NSHomeDirectory())]) { [weak self] _ in self?.reloadSessions() }
                return
            }
            if self.currentSession == nil || !self.sessions.contains(where: { $0.id == self.currentSession }) {
                select(session: sessions.first?.id)
            }
        }
        reloadAgents()
    }

    func reloadAgents() {
        call("agent.list") { [weak self] j in
            guard let self else { return }
            let list = j["agents"].array.map { a in
                AgentInfo(blockID: a["block_id"].string ?? "", sessionID: a["session_id"].string ?? "",
                          sessionLabel: a["session_label"].string ?? "", harness: a["agent"]["harness"].string ?? "",
                          state: a["agent"]["state"].string ?? "", message: a["agent"]["message"].string ?? "",
                          cwd: a["agent"]["cwd"].string ?? "")
            }
            let order = ["blocked": 0, "done": 1, "working": 2, "idle": 3]
            agents = list.sorted { (order[$0.state] ?? 9) < (order[$1.state] ?? 9) }
            notifyChanges(list)
        }
    }

    private func notifyChanges(_ list: [AgentInfo]) {
        defer { lastAgentStates = Dictionary(uniqueKeysWithValues: list.map { ($0.blockID, $0.state) }) }
        guard !NSApp.isActive else { return }
        for a in list where (a.state == "blocked" || a.state == "done") && lastAgentStates[a.blockID] != a.state {
            let c = UNMutableNotificationContent()
            c.title = "\(a.harness) \(a.state == "blocked" ? "needs you" : "is done")"
            c.body = [a.sessionLabel.isEmpty ? nil : a.sessionLabel, a.message.isEmpty ? nil : a.message].compactMap { $0 }.joined(separator: " — ")
            UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: "\(a.blockID)-\(a.state)", content: c, trigger: nil))
        }
    }

    func select(session id: String?) {
        currentSession = id
        reloadView()
    }

    func reloadView() {
        guard let sid = currentSession else { windows = []; return }
        call("session.view", ["session_id": .string(sid)]) { [weak self] j in
            guard let self, self.currentSession == sid else { return }
            sessionLabel = j["label"].string ?? ""
            activeWindow = j["active_window_id"].string
            windows = j["windows"].array.map { w in
                WindowInfo(id: w["window_id"].string ?? "", label: w["label"].string ?? "", layout: Layout(w["layout"]),
                           focused: w["focused_block_id"].string, zoomed: w["zoomed"].bool ?? false)
            }
            var info: [String: BlockInfo] = [:]
            for (bid, b) in j["blocks"].object {
                info[bid] = BlockInfo(title: b["title"].string ?? "", label: b["label"].string ?? "",
                                      cols: b["cols"].int ?? 0, rows: b["rows"].int ?? 0, sizeOwner: b["size_owner"].string,
                                      foreground: b["foreground"]["name"].string)
            }
            blocks = info
            attachVisible()
            onLayout?()
            onFrame?()
        }
    }

    var active: WindowInfo? { windows.first { $0.id == activeWindow } ?? windows.first }
    var focusedBlock: String? { active?.focused }

    /// Visible blocks: the active window's, or only the zoomed one.
    var visibleBlocks: [(String, CGRect)] {
        guard let w = active, let layout = w.layout else { return [] }
        if w.zoomed, let f = w.focused { return [(f, CGRect(x: 0, y: 0, width: 1, height: 1))] }
        return layout.rects()
    }

    private func attachVisible() {
        let want = Set(visibleBlocks.map(\.0))
        let engine = engine
        for b in want.subtracting(attached) {
            vt.async {
                engine.attach(block: b) { frame in
                    if ProcessInfo.processInfo.environment["HIVE_DEBUG"] != nil {
                        let rows = (0..<min(frame.rows, 4)).map { frame.rowText($0) }
                        NSLog("hive frame %@ %dx%d cursor=%d,%d rows=%@", b, frame.cols, frame.rows, frame.cursor.x, frame.cursor.y, rows.description)
                    }
                    Task { @MainActor [weak self] in
                        self?.frames[b] = frame
                        self?.onFrame?()
                    }
                }
            }
        }
        for b in attached.subtracting(want) {
            vt.async { engine.detach(block: b) }
            frames[b] = nil
        }
        attached = want
    }

    private func handleEvent(_ ev: JSON) {
        switch ev["type"].string {
        case "session_created", "session_destroyed":
            reloadSessions()
        case "session_event":
            let sid = ev["payload"]["session_id"].string
            let inner = ev["payload"]["event"]
            let type = inner["type"].string
            if type == "block_event" {
                let name = inner["payload"]["name"].string
                if name == "agent_changed" { reloadAgents() }
                if name == "title_changed" || name == "size_changed" || name == "process_changed", sid == currentSession { scheduleReload() }
            } else if sid == currentSession {
                scheduleReload()
            }
            if type == "block_closed" || type == "block_created" { reloadSessions() }
        default: break
        }
    }

    /// Coalesces bursts of layout events into one reload.
    private func scheduleReload() {
        guard !reloadPending else { return }
        reloadPending = true
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.03) { [weak self] in
            self?.reloadPending = false
            self?.reloadView()
        }
    }

    // MARK: actions

    private var sid: JSON { .string(currentSession ?? "") }

    func newSession() {
        call("session.create", ["cwd": .string(NSHomeDirectory())]) { [weak self] j in
            self?.currentSession = j["session_id"].string
            self?.reloadSessions()
        }
    }

    func newTab() { call("session.new_window", ["session_id": sid, "layout": ["cwd": .string(currentCwd())]]) }

    func split(horizontal: Bool, cols: Int? = nil, rows: Int? = nil) {
        guard let b = focusedBlock else { return }
        var p: [String: JSON] = ["session_id": sid, "block_id": .string(b), "direction": horizontal ? "horizontal" : "vertical",
                                 "layout": ["cwd": .string(currentCwd())]]
        if let cols, let rows { p["cols"] = .number(Double(cols)); p["rows"] = .number(Double(rows)) }
        call("session.new_split", .object(p))
    }

    func closePane() {
        guard let b = focusedBlock else { return }
        call("block.close", ["session_id": sid, "block_id": .string(b)])
    }

    func focus(_ direction: String) {
        guard let b = focusedBlock else { return }
        call("session.focus_direction", ["session_id": sid, "block_id": .string(b), "direction": .string(direction)])
    }

    func focus(block: String) {
        guard block != focusedBlock else { return }
        call("session.focus_block", ["session_id": sid, "block_id": .string(block)])
    }

    func toggleZoom() {
        guard let b = focusedBlock else { return }
        call("session.zoom_block", ["session_id": sid, "block_id": .string(b)])
    }

    func resizePane(_ direction: String) {
        guard let b = focusedBlock else { return }
        call("session.resize_pane", ["session_id": sid, "block_id": .string(b), "direction": .string(direction), "amount": 0.04])
    }

    func selectWindow(index: Int) {
        guard windows.indices.contains(index) else { return }
        call("session.focus_window", ["session_id": sid, "window_id": .string(windows[index].id)])
    }

    func selectWindow(offset: Int) {
        call(offset > 0 ? "session.focus_next_window" : "session.focus_previous_window", ["session_id": sid])
    }

    func renameSession(_ label: String) { call("session.set_label", ["session_id": sid, "label": .string(label)]) }

    func jump(to agent: AgentInfo) {
        if currentSession != agent.sessionID { currentSession = agent.sessionID }
        call("session.focus_block", ["session_id": .string(agent.sessionID), "block_id": .string(agent.blockID)]) { [weak self] _ in
            self?.reloadView()
        }
    }

    /// The directory new panes start in: the focused pane's.
    private func currentCwd() -> String {
        NSHomeDirectory()
    }

    // MARK: input and size

    func key(_ k: KeyInput) {
        guard let b = focusedBlock else { return }
        let engine = engine
        vt.async { engine.key(block: b, k) }
    }

    func text(_ s: String) {
        guard let b = focusedBlock else { return }
        let engine = engine
        vt.async { engine.text(block: b, s) }
    }

    func paste(_ s: String) {
        guard let b = focusedBlock else { return }
        let engine = engine
        vt.async { engine.paste(block: b, s) }
    }

    func scroll(block: String, rows: Int) {
        let engine = engine
        vt.async { engine.scroll(block: block, rows: rows) }
    }

    /// Takes size authority for the visible panes at their pixel sizes. Done
    /// on key focus and interaction, not mere visibility, so two clients
    /// showing one session don't fight.
    func claimSizes(_ sizes: [String: (cols: Int, rows: Int)], cellWidth: Int, cellHeight: Int) {
        let client = client
        for (b, s) in sizes where s.cols > 1 && s.rows > 1 {
            if let info = blocks[b], info.cols == s.cols, info.rows == s.rows, info.sizeOwner == clientID { continue }
            vt.async {
                client.resize(block: b, cols: UInt16(s.cols), rows: UInt16(s.rows),
                              cellWidth: UInt16(cellWidth), cellHeight: UInt16(cellHeight), take: true)
            }
            blocks[b]?.cols = s.cols
            blocks[b]?.rows = s.rows
            blocks[b]?.sizeOwner = clientID
        }
    }
}
