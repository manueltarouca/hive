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

extension ISO8601DateFormatter {
    static let hive: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()
}

/// An agent's task from its terminal title: leading status glyphs and
/// invisible formatting removed. Titles come from programs, so text-direction
/// overrides are stripped too: they could make a title read as something else.
func taskName(title: String, harness: String) -> String? {
    let t = cleanTitle(title).drop { !$0.isLetter && !$0.isNumber }.trimmingCharacters(in: .whitespaces)
    let name = Programs.lookup(harness).name
    return t.isEmpty || t == name ? nil : t
}

/// Removes bidi overrides, isolates and marks from text a program supplied.
func cleanTitle(_ s: String) -> String {
    String(s.unicodeScalars.filter { !(0x202A...0x202E).contains($0.value) && !(0x2066...0x2069).contains($0.value)
        && $0.value != 0x200E && $0.value != 0x200F })
}

struct WindowInfo: Identifiable, Equatable {
    let id: String
    var label: String
    var layout: Layout?
    var focused: String?
    var zoomed: Bool

    /// A tab's title: its label, else its focused block's.
    func titleParts(_ blocks: [String: BlockInfo]) -> (primary: String, secondary: String) {
        if !label.isEmpty { return (label, "") }
        return focused.flatMap { blocks[$0] }?.titleParts ?? ("shell", "")
    }
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
    var title: String = ""
    var updatedAt: Date?

    /// The task the agent is on, from its title, else the harness name.
    var task: String { taskName(title: title, harness: harness) ?? Programs.lookup(harness).name }
}

struct BlockInfo: Equatable {
    var title: String
    var label: String
    var cols: Int, rows: Int
    var sizeOwner: String?
    var foreground: String?
    var cwd: String?
    var harness: String?
    var agentState: String?
    var exited = false
    var exitCode = 0

    /// The program as people call it: "Node", "Neovim", "Claude Code".
    var program: String { programInfo.name }

    /// The agent if one reports from here, else the foreground program.
    var programInfo: Program { Programs.lookup(harness ?? foreground ?? "") }

    /// An agent's task, from the title it sets ("✳ Arch review" → "Arch review").
    var task: String? { harness.flatMap { taskName(title: title, harness: $0) } }

    var shortDir: String {
        guard var d = cwd, !d.isEmpty else { return "" }
        let home = NSHomeDirectory()
        if d.hasPrefix(home) { d = "~" + d.dropFirst(home.count) }
        return d
    }

    /// Header and tab title: the label, an agent's task, or "Program ~/dir".
    var titleParts: (primary: String, secondary: String) {
        if !label.isEmpty { return (label, shortDir) }
        if let task { return (task, (shortDir as NSString).lastPathComponent) }
        return (program, shortDir)
    }

}

/// The app's state. Lives on the main thread; talks to the server through
/// the engine on the `hive.vt` queue.
@MainActor
final class AppModel: ObservableObject {
    /// The semver the bundle was built with (Info.plist HiveVersion).
    static let version = Bundle.main.object(forInfoDictionaryKey: "HiveVersion") as? String ?? "0.0.0-dev"

    let vt = DispatchQueue(label: "hive.vt", qos: .userInteractive)
    /// Replaced on every reconnect.
    private(set) var client: HiveClient
    private(set) var engine: Engine
    let socket: String
    /// Preferences: a separate suite for test runs, so they never touch yours.
    static let defaults = ProcessInfo.processInfo.environment["HIVE_DEFAULTS_SUITE"].flatMap { UserDefaults(suiteName: $0) } ?? .standard

    @Published var status = "Connecting…"
    @Published var connected = false
    @Published var sessions: [SessionInfo] = []
    @Published var agents: [AgentInfo] = []
    @Published var currentSession: String?
    @Published var sessionLabel = ""
    @Published var windows: [WindowInfo] = []
    @Published var activeWindow: String?
    @Published var blocks: [String: BlockInfo] = [:]
    @Published var style: Style = .light
    /// The theme: Hive Day, Hive Night, or follow macOS.
    @Published var appearance = Appearance(stored: AppModel.defaults.string(forKey: "appearance")) {
        didSet { AppModel.defaults.set(appearance.rawValue, forKey: "appearance") }
    }
    /// The agents sidebar: vertical tabs, organized around agents.
    @Published var sidebar = AppModel.defaults.bool(forKey: "sidebar") {
        didSet { AppModel.defaults.set(sidebar, forKey: "sidebar") }
    }
    @Published var host = Host.current().localizedName ?? "This Mac"
    var clientID: String?

    /// Frames by block, drawn by the terminal view.
    var frames: [String: PaneFrame] = [:]
    var onFrame: ((String) -> Void)?
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

    private var reconnectDelay: TimeInterval = 0.5
    private var closing = false

    func start() {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { _, _ in }
        // Only at launch: if the server stops later, it was asked to.
        // Reconnecting must not undo `hive server stop`.
        ensureServer()
        connect()
    }

    /// Connects, or reconnects with a fresh client and engine. Panes are
    /// attached again and state is re-read, so nothing is stale afterwards.
    private func connect() {
        let theme = engine.theme
        client = HiveClient(socketPath: socket, queue: vt)
        engine = Engine(client: client)
        attached.removeAll()
        let engine = engine
        vt.async { [weak self] in
            engine.theme = theme
            // Some events were missed: re-read everything.
            engine.client.onEventsResync = { Task { @MainActor in self?.reloadSessions(); self?.reloadView() } }
            engine.log = { msg in NSLog("hive: %@", msg) }
            engine.client.onEvent = { ev in Task { @MainActor in self?.handleEvent(ev) } }
            engine.client.onClose = { err in Task { @MainActor in self?.disconnected(err) } }
            engine.client.onError = { kind, block, err in NSLog("hive error (%@ %@): %@", kind ?? "-", block ?? "-", err.description) }
            engine.client.connect(name: "Hive.app", version: AppModel.version) { result in
                Task { @MainActor in
                    switch result {
                    case .success(let id):
                        self?.reconnectDelay = 0.5
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
    }

    /// Starts the server if nothing answers on the socket. A socket file can
    /// outlive its server, so this connects rather than checking the file.
    private func ensureServer() {
        guard !AppModel.canConnect(socket), let bin = AppModel.hiveBinary() else { return }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: bin)
        p.arguments = ["server", "start"]
        try? p.run()
        p.waitUntilExit()
    }

    private func disconnected(_ err: Error?) {
        connected = false
        status = "Reconnecting…"
        guard !closing else { return }
        // Back off: 0.5s, 1s, 2s… up to 10s.
        let delay = reconnectDelay
        reconnectDelay = min(reconnectDelay * 2, 10)
        DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in self?.connect() }
    }

    static func canConnect(_ path: String) -> Bool {
        let fd = Darwin.socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { return false }
        defer { Darwin.close(fd) }
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        let bytes = Array(path.utf8)
        guard bytes.count < MemoryLayout.size(ofValue: addr.sun_path) else { return false }
        withUnsafeMutableBytes(of: &addr.sun_path) { buf in
            for (i, b) in bytes.enumerated() { buf[i] = b }
        }
        let len = socklen_t(MemoryLayout<sockaddr_un>.size)
        return withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { Darwin.connect(fd, $0, len) == 0 }
        }
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
                // Come back to the session you were in.
                let last = AppModel.defaults.string(forKey: "lastSession")
                select(session: sessions.first { $0.id == last }?.id ?? sessions.first?.id)
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
                          cwd: a["agent"]["cwd"].string ?? "", title: a["title"].string ?? "",
                          updatedAt: a["agent"]["updated_at"].string.flatMap { ISO8601DateFormatter.hive.date(from: $0) })
            }
            let order = ["blocked": 0, "done": 1, "working": 2, "idle": 3]
            agents = list.sorted { (order[$0.state] ?? 9) < (order[$1.state] ?? 9) }
            let live = Set(list.map(\.blockID))
            if seenDone.keys.contains(where: { !live.contains($0) }) { seenDone = seenDone.filter { live.contains($0.key) } }
            markSeen()
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
        if let id { AppModel.defaults.set(id, forKey: "lastSession") }
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
                var cwd = b["foreground"]["cwd"].string
                if let pwd = b["pwd"].string, let u = URL(string: pwd), u.isFileURL { cwd = u.path }
                if cwd == nil || cwd!.isEmpty { cwd = b["agent"]["cwd"].string ?? b["cwd"].string }
                let agentState = b["agent"]["state"].string
                info[bid] = BlockInfo(title: b["title"].string ?? "", label: b["label"].string ?? "",
                                      cols: b["cols"].int ?? 0, rows: b["rows"].int ?? 0, sizeOwner: b["size_owner"].string,
                                      foreground: b["foreground"]["program"].string ?? b["foreground"]["name"].string, cwd: cwd,
                                      harness: agentState == "ended" ? nil : b["agent"]["harness"].string,
                                      agentState: agentState, exited: b["exited"].bool ?? false, exitCode: b["exit_code"].int ?? 0)
            }
            blocks = info
            attachVisible()
            markSeen()
            onLayout?()
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
                    #if DEBUG
                    // Terminal content in the log: debug builds only.
                    if ProcessInfo.processInfo.environment["HIVE_DEBUG"] != nil {
                        let rows = (0..<min(frame.rows, 4)).map { frame.rowText($0) }
                        NSLog("hive frame %@ %dx%d cursor=%d,%d rows=%@", b, frame.cols, frame.rows, frame.cursor.x, frame.cursor.y, rows.description)
                    }
                    #endif
                    Task { @MainActor [weak self] in
                        self?.frames[b] = frame
                        self?.onFrame?(b)
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

    /// The block whose program just copied to the clipboard, for a moment.
    @Published var copiedBlock: String?
    private var copiedReset: DispatchWorkItem?

    /// A program copied text (OSC 52): put it on the Mac clipboard if it came
    /// from the pane the user is working in, and say so in the pane header.
    private func programCopied(block: String, text: String) {
        guard ClipboardPolicy.accepts(text: text, from: block, focusedBlock: focusedBlock,
                                      appActive: NSApp.isActive) else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
        copiedBlock = block
        copiedReset?.cancel()
        let reset = DispatchWorkItem { [weak self] in self?.copiedBlock = nil }
        copiedReset = reset
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.5, execute: reset)
    }

    private func handleEvent(_ ev: JSON) {
        switch ev["type"].string {
        case "session_created", "session_destroyed":
            reloadSessions()
        case "session_event":
            if ev["payload"]["event"]["payload"]["name"].string == "child_exited" { scheduleReload() }
            let sid = ev["payload"]["session_id"].string
            let inner = ev["payload"]["event"]
            let type = inner["type"].string
            if type == "block_event" {
                let name = inner["payload"]["name"].string
                if name == "agent_changed" { reloadAgents() }
                if name == "clipboard_written" {
                    programCopied(block: inner["payload"]["block_id"].string ?? "", text: inner["payload"]["payload"]["text"].string ?? "")
                }
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

    func newSession(label: String = "") {
        call("session.create", ["cwd": .string(NSHomeDirectory()), "label": .string(label)]) { [weak self] j in
            self?.select(session: j["session_id"].string)
            self?.reloadSessions()
        }
    }

    func renameTab(_ label: String) {
        guard let w = active else { return }
        call("session.set_window_label", ["session_id": sid, "window_id": .string(w.id), "label": .string(label)])
    }

    func closeTab() {
        guard let w = active else { return }
        call("session.close_window", ["session_id": sid, "window_id": .string(w.id)])
    }

    /// The state a tab shows: working, blocked, or done not yet looked at.
    func status(of w: WindowInfo) -> String? { summary(of: w).state }

    // MARK: what needs the user

    /// For each block whose agent finished, the agent's update time when the
    /// user last looked at it. "Done" goes quiet once seen.
    @Published private(set) var seenDone: [String: Date] = [:]

    func doneSeen(_ a: AgentInfo) -> Bool {
        guard let d = a.updatedAt, let seen = seenDone[a.blockID] else { return false }
        return seen >= d
    }

    /// Marks finished agents in the window the user is looking at as seen.
    func markSeen() {
        guard NSApp.isActive, let w = active else { return }
        let ids = Set(w.layout?.rects().map(\.0) ?? [])
        for a in agents where a.state == "done" && ids.contains(a.blockID) {
            if let d = a.updatedAt, seenDone[a.blockID] != d { seenDone[a.blockID] = d }
        }
    }

    /// What a tab's sidebar row says (see TabSummary).
    /// A tab's program: its agent's if it has one, else the focused pane's.
    func program(of w: WindowInfo) -> Program {
        let ids = w.layout?.rects().map(\.0) ?? []
        if let agent = ids.compactMap({ blocks[$0] }).first(where: { $0.harness != nil }) { return agent.programInfo }
        return w.focused.flatMap { blocks[$0] }?.programInfo ?? Programs.lookup("")
    }

    func summary(of w: WindowInfo) -> TabSummary {
        let ids = Set(w.layout?.rects().map(\.0) ?? [])
        let mine = agents.filter { ids.contains($0.blockID) }
        let info = w.focused.flatMap { blocks[$0] }
        let label = w.label.isEmpty ? (info?.label ?? "") : w.label
        return TabSummary.make(label: label, program: info?.program ?? "shell", dir: info?.shortDir ?? "",
                               agents: mine.map { AgentSnapshot(state: $0.state, task: $0.task, message: $0.message) },
                               doneSeen: mine.filter { $0.state == "done" }.allSatisfy(doneSeen))
    }

    private var places: [AgentPlace] {
        agents.map { AgentPlace(session: $0.sessionID, state: $0.state, doneSeen: doneSeen($0)) }
    }

    /// The agent in another session that needs the user most: blocked first,
    /// then finished and not yet seen.
    var mostUrgentElsewhere: AgentInfo? {
        let others = agents.filter { $0.sessionID != currentSession }
        return others.first { $0.state == "blocked" } ?? others.first { $0.state == "done" && !doneSeen($0) }
    }

    /// Agents in other sessions that need the user.
    var attentionElsewhere: Attention { Attention.elsewhere(places, current: currentSession) }

    func attention(session: String) -> Attention { Attention.of(places, session: session) }

    /// Moves a tab to a new position.
    func moveWindow(_ id: String, to index: Int) {
        // Show it at once; the server's view follows.
        if let from = windows.firstIndex(where: { $0.id == id }) {
            let w = windows.remove(at: from)
            windows.insert(w, at: min(index, windows.count))
        }
        call("session.move_window", ["session_id": sid, "window_id": .string(id), "index": .number(Double(index))])
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
        if let f = focusedBlock, let d = blocks[f]?.cwd, FileManager.default.fileExists(atPath: d) { return d }
        return NSHomeDirectory()
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

    func select(block: String, _ type: Mirror.SelectEvent, col: Int, row: Int, x: Double, y: Double,
                cellWidth: Double, cellHeight: Double, repeatInterval: TimeInterval, repeatDistance: Double) {
        let engine = engine
        vt.async {
            engine.select(block: block, type, col: col, row: row, x: x, y: y, cellWidth: cellWidth, cellHeight: cellHeight,
                          repeatInterval: repeatInterval, repeatDistance: repeatDistance)
        }
    }

    func copySelection(block: String) {
        let engine = engine
        vt.async {
            guard let text = engine.selectedText(block: block), !text.isEmpty else { return }
            DispatchQueue.main.async {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(text, forType: .string)
            }
        }
    }

    /// Checks on the vt queue whether pasting s is safe, then answers on main.
    func pasteChecked(block: String, _ s: String, then: @escaping @MainActor (Bool) -> Void) {
        let engine = engine
        vt.async {
            let safe = engine.mirror(block: block)?.isPasteSafe(s) ?? true
            Task { @MainActor in then(safe) }
        }
    }

    func pointer(block: String, _ action: Mirror.PointerAction, button: Mirror.PointerButton?, mods: KeyMods,
                 x: Double, y: Double, geometry: PointerGeometry, col: Int, row: Int,
                 repeatInterval: TimeInterval, repeatDistance: Double) {
        let engine = engine
        vt.async {
            engine.pointer(block: block, action, button: button, mods: mods, x: x, y: y, geometry: geometry,
                           col: col, row: row, repeatInterval: repeatInterval, repeatDistance: repeatDistance)
        }
    }

    func wheel(block: String, rows: Int, shift: Bool, x: Double, y: Double, geometry: PointerGeometry) {
        let engine = engine
        vt.async { engine.wheel(block: block, rows: rows, shift: shift, x: x, y: y, geometry: geometry) }
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
