import AppKit
import Combine
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
    /// The host it's on: HostConnection.localID, or a saved host's label.
    var host: String = HostConnection.localID
    var label: String
    var blocks: Int
    var agents: Int
}

struct AgentInfo: Identifiable, Equatable {
    var id: String { blockID }
    let blockID: String
    let sessionID: String
    var host: String = HostConnection.localID
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
    /// The account's home on the block's machine, for ~ (nil: this Mac's).
    var home: String? = nil

    /// The program as people call it: "Node", "Neovim", "Claude Code".
    var program: String { programInfo.name }

    /// The agent if one reports from here, else the foreground program (see
    /// Programs.shown for when the running program wins).
    var programInfo: Program { Programs.shown(harness: harness, foreground: foreground) }

    /// An agent's task, from the title it sets ("✳ Arch review" → "Arch review").
    var task: String? { harness.flatMap { taskName(title: title, harness: $0) } }

    var shortDir: String {
        guard var d = cwd, !d.isEmpty else { return "" }
        let home = self.home ?? NSHomeDirectory()
        if d.hasPrefix(home + "/") || d == home { d = "~" + d.dropFirst(home.count) }
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
    /// The servers: this Mac's first, then remote hosts.
    private(set) var hosts: [HostConnection] = []
    /// hosts.json as last read (AppModel+Hosts).
    var hostsStamp: Date?
    var hostsTimer: Timer?
    /// The session to come back to at launch. Its host may connect later
    /// (a remote one): it wins when it appears, until the user picks one.
    var restoreSession = AppModel.defaults.string(forKey: "lastSession")
    var hostsStampPath: String?
    /// The hosts the switcher lists: label, and why it isn't connected.
    @Published private(set) var hostRows: [HostRow] = []
    struct HostRow: Equatable {
        let id: String
        let name: String       // This Mac, or the server's address
        let label: String      // as `hive hosts` knows it
        let problem: String?   // a sentence naming the host, while not connected
        let detail: String?    // the reason in full
        var local: Bool { id == HostConnection.localID }
    }
    /// Runs once a host has connected (and listed its sessions).
    var pendingConnected: [String: () -> Void] = [:]
    func whenConnected(_ h: HostConnection, then: @escaping () -> Void) {
        if h.connected { then() } else { pendingConnected[h.id] = then }
    }

    /// The current host has a problem worth showing (connecting at launch
    /// isn't one).
    @Published var trouble = false

    /// The current host's problem in full, for the title bar's tooltip.
    @Published var statusDetail: String?
    /// The host of the session on screen. Its client and engine are the
    /// ones panes, input and calls go to.
    private(set) var current: HostConnection
    var client: HiveClient { current.client }
    var engine: Engine { current.engine }
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
    /// The settings file (~/.config/hive/config), read through the bundled hive.
    let settingsStore = SettingsStore()
    /// The theme: Hive Day, Hive Night, or follow macOS. From the settings
    /// file; until it exists, the choice remembered before (UserDefaults).
    @Published var appearance = Appearance.system
    private var settingsBag: Set<AnyCancellable> = []
    /// The agents sidebar: vertical tabs, organized around agents.
    @Published var sidebar = AppModel.defaults.bool(forKey: "sidebar") {
        didSet { AppModel.defaults.set(sidebar, forKey: "sidebar") }
    }
    /// The current host's name, as the title bar shows it.
    @Published var host = "This Mac"
    /// The session on screen is on another machine.
    @Published var remote = false
    static let macName = Host.current().localizedName ?? "This Mac"
    var clientID: String? { current.clientID }

    /// Frames by block, drawn by the terminal view.
    var frames: [String: PaneFrame] = [:]
    var onFrame: ((String) -> Void)?
    /// Runs after the layout changed, so the view can size the panes.
    var onLayout: (() -> Void)?
    private var attached: Set<String> = []
    private var reloadPending = false

    init() {
        socket = AppModel.socketPath()
        current = HostConnection(id: HostConnection.localID, label: AppModel.macName, endpoint: .unix(path: socket), vt: vt)
        hosts = [current]
        appearance = settingsStore.fileExists ? settingsStore.settings.theme
            : Appearance(stored: AppModel.defaults.string(forKey: "appearance"))
        settingsStore.$settings.combineLatest(settingsStore.$fileExists).sink { [weak self] s, exists in
            guard let self, exists, self.appearance != s.theme else { return }
            self.appearance = s.theme
        }.store(in: &settingsBag)
    }

    /// The settings the app follows (bell, notifications, paste).
    var settings: Settings { settingsStore.settings }

    /// Chosen from the menu or palette: applied now, and written to the file.
    func setAppearance(_ a: Appearance) {
        appearance = a
        settingsStore.set("theme", a.rawValue)
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
    /// Reads only the bundle, the environment and the disk: any thread.
    nonisolated static func hiveBinary() -> String? {
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
        if settings.notify != "never" {
            UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { _, _ in }
        }
        // Only at launch: if the server stops later, it was asked to.
        // Reconnecting must not undo `hive server stop`.
        ensureServer()
        for h in hosts { wireHost(h); h.connect() }
        watchHosts()
    }

    func setHosts(_ next: [HostConnection]) {
        hosts = next
        publishHosts()
    }

    /// Moves to a host's first session (its own, or this Mac's).
    func select(onHost h: HostConnection) {
        switchHost(to: h)
        select(session: h.sessions.first?.id)
    }

    /// The model hears each host's connection and events.
    func wireHost(_ h: HostConnection) {
        h.onConnected = { [weak self] h in
            guard let self else { return }
            self.publishHosts()
            if let then = self.pendingConnected.removeValue(forKey: h.id) { DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) { then() } }
            if h === self.current { self.connected = true; self.status = h.status }
            self.reloadSessions(h, create: h.isLocal)
            if h === self.current { self.reloadView() }
        }
        h.onDisconnected = { [weak self] h in
            guard let self else { return }
            // Its sessions stay listed (under its problem) until it's back.
            self.publishHosts()
            if h === self.current {
                self.connected = false
                self.status = h.status
                self.attached.removeAll() // the new engine attaches afresh
            }
        }
        h.onResync = { [weak self] h in
            self?.reloadSessions(h)
            if h === self?.current { self?.reloadView() }
        }
        h.onEvents = { [weak self] h, events, overflowed in
            guard let self else { return }
            // Too many to keep: re-read everything instead.
            if overflowed {
                self.reloadSessions(h)
                if h === self.current { self.reloadView() }
                return
            }
            for e in events { self.handleEvent(e, from: h) }
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

    /// Calls the current host's server.
    func call(_ method: String, _ params: JSON = [:], then: (@MainActor (JSON) -> Void)? = nil) {
        current.call(method, params, then: then)
    }

    func host(id: String) -> HostConnection? { hosts.first { $0.id == id } }

    /// Every host's sessions and agents, as the sidebar and switcher show
    /// them: this Mac's first, agents by urgency.
    func publishHosts() {
        let rows = hosts.map { HostRow(id: $0.id, name: $0.displayName, label: $0.label, problem: $0.problem, detail: $0.detail) }
        if rows != hostRows { hostRows = rows }
        if connected != current.connected { connected = current.connected }
        if status != current.status { status = current.status }
        if statusDetail != current.detail { statusDetail = current.detail }
        let t = !current.connected && current.link != .connecting
        if trouble != t { trouble = t }
        let all = hosts.flatMap(\.sessions)
        if all != sessions { sessions = all }
        let order = ["blocked": 0, "done": 1, "working": 2, "idle": 3]
        let list = hosts.flatMap(\.agents).sorted { (order[$0.state] ?? 9) < (order[$1.state] ?? 9) }
        if list != agents { agents = list }
    }

    /// Applies a terminal theme to every host's panes.
    func setTerminalTheme(_ t: Theme) { for h in hosts { h.theme = t } }
    func setCursorBlink(_ b: Bool?) { for h in hosts { h.cursorBlink = b } }

    // MARK: state

    /// Re-reads every host's sessions and agents.
    func reloadSessions() { for h in hosts { reloadSessions(h) } }
    func reloadAgents() { for h in hosts { reloadAgents(h) } }

    func reloadSessions(_ h: HostConnection, create: Bool = false) {
        h.call("session.list") { [weak self, weak h] j in
            guard let self, let h else { return }
            h.sessions = j["sessions"].array.compactMap { s in
                guard let id = s["session_id"].string else { return nil }
                return SessionInfo(id: id, host: h.id, label: s["label"].string ?? "", blocks: s["blocks"].int ?? 0, agents: s["agents"].int ?? 0)
            }
            publishHosts()
            if let r = restoreSession, h.sessions.contains(where: { $0.id == r }) {
                restoreSession = nil
                if currentSession != r { select(session: r, restoring: true) }
                return
            }
            if h.sessions.isEmpty, create {
                // A first session on this Mac, in its home (servers before
                // 0.1 took an empty cwd as their own, /).
                h.call("session.create", ["cwd": .string(NSHomeDirectory())]) { [weak self, weak h] _ in if let h { self?.reloadSessions(h) } }
                return
            }
            // Gone from the list: pick another, unless the session's host is
            // just away (it comes back to it).
            if self.current.connected || self.currentSession == nil,
               self.currentSession == nil || !self.sessions.contains(where: { $0.id == self.currentSession }) {
                // Come back to the session you were in, wherever it is.
                let last = AppModel.defaults.string(forKey: "lastSession")
                if let s = sessions.first(where: { $0.id == last }) ?? sessions.first(where: { $0.host == self.current.id }) ?? sessions.first {
                    select(session: s.id, restoring: true)
                } else if h === self.current {
                    select(session: nil, restoring: true)
                }
            }
        }
        reloadAgents(h)
    }

    func reloadAgents(_ h: HostConnection) {
        h.call("agent.list") { [weak self, weak h] j in
            guard let self, let h else { return }
            let list = j["agents"].array.compactMap { a -> AgentInfo? in
                let harness = a["agent"]["harness"].string ?? ""
                // The agent's own status wins over the last hook (an interrupt
                // runs no hook), and a pane back at its shell has no agent.
                // Servers from 0.1 do both themselves; this covers older ones.
                let records = a["program_status"].array.map { (app: $0["app"].string ?? "", status: $0["state"].string ?? "") }
                let state = AgentStatus.effective(hook: a["agent"]["state"].string, harness: harness, records: records) ?? ""
                if Programs.atShell(a["foreground"]["program"].string ?? a["foreground"]["name"].string) { return nil }
                return AgentInfo(blockID: a["block_id"].string ?? "", sessionID: a["session_id"].string ?? "", host: h.id,
                          sessionLabel: a["session_label"].string ?? "", harness: harness,
                          state: state, message: a["agent"]["message"].string ?? "",
                          cwd: a["agent"]["cwd"].string ?? "", title: a["title"].string ?? "",
                          updatedAt: a["agent"]["updated_at"].string.flatMap { ISO8601DateFormatter.hive.date(from: $0) })
            }
            h.agents = list
            publishHosts()
            // A pane's agent can report after the layout loaded (a new Claude
            // says SessionStart once it's up): carry it into the pane, so its
            // icon and name follow without waiting for another reload.
            // Only this host's panes are on screen: another host's agents
            // say nothing about them.
            for a in list where h === self.current && blocks[a.blockID] != nil {
                let gone = a.state == "ended" || a.harness.isEmpty || Programs.atShell(blocks[a.blockID]?.foreground)
                let harness = gone ? nil : a.harness, state = gone ? "ended" : a.state
                if blocks[a.blockID]?.harness != harness { blocks[a.blockID]?.harness = harness }
                if blocks[a.blockID]?.agentState != state { blocks[a.blockID]?.agentState = state }
            }
            // A pane whose agent left the list ended (or its shell came back).
            let listed = Set(list.map(\.blockID))
            for (id, b) in blocks where h === self.current && b.harness != nil && !listed.contains(id) {
                blocks[id]?.harness = nil
                blocks[id]?.agentState = "ended"
            }
            let live = Set(agents.map(\.blockID))
            if seenDone.keys.contains(where: { !live.contains($0) }) { seenDone = seenDone.filter { live.contains($0.key) } }
            markSeen()
            notifyChanges(list, from: h)
        }
    }

    private func notifyChanges(_ list: [AgentInfo], from h: HostConnection) {
        defer { h.lastAgentStates = Dictionary(uniqueKeysWithValues: list.map { ($0.blockID, $0.state) }) }
        switch settings.notify {
        case "never": return
        case "always": break
        default: guard !NSApp.isActive else { return } // background
        }
        for a in list where (a.state == "blocked" || a.state == "done") && h.lastAgentStates[a.blockID] != a.state {
            let c = UNMutableNotificationContent()
            c.title = "\(Programs.lookup(a.harness).name) \(a.state == "blocked" ? "needs you" : "is done")"
            // Where it is goes in the subtitle (a remote host's agents name
            // the host), what it said in the body.
            let session = a.sessionLabel.isEmpty ? "a session" : a.sessionLabel
            c.subtitle = h.isLocal ? a.sessionLabel : "\(session) on \(h.displayName)"
            c.body = a.message
            UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: "\(h.id)-\(a.blockID)-\(a.state)", content: c, trigger: nil))
        }
    }

    /// restoring: the app's own choice, which the session to come back to
    /// can still replace; the user's choice ends that.
    func select(session id: String?, restoring: Bool = false) {
        if !restoring { restoreSession = nil }
        if let id, let s = sessions.first(where: { $0.id == id }), s.host != current.id, let h = host(id: s.host) {
            switchHost(to: h)
        }
        currentSession = id
        // A fallback doesn't replace the session to come back to.
        if let id, !restoring || restoreSession == nil { AppModel.defaults.set(id, forKey: "lastSession") }
        reloadView()
    }

    /// Panes, input and calls go to another host from now on.
    func switchHost(to h: HostConnection) {
        guard h !== current else { return }
        let old = current.engine
        for b in attached { vt.async { old.detach(block: b) } }
        attached.removeAll()
        frames.removeAll()
        windows = []
        blocks = [:]
        current = h
        host = h.displayName
        remote = !h.isLocal
        connected = h.connected
        status = h.status
    }

    func reloadView() {
        guard let sid = currentSession else { windows = []; return }
        let h = current
        h.call("session.view", ["session_id": .string(sid)]) { [weak self] j in
            guard let self, self.currentSession == sid, self.current === h else { return }
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
                let foreground = b["foreground"]["program"].string ?? b["foreground"]["name"].string
                // An agent is gone once it ended or its shell is back in front
                // (killed before it could report; servers before 0.1 don't
                // notice that themselves).
                let gone = b["agent"]["state"].string == "ended" || Programs.atShell(foreground)
                let records = b["program_status"].array.map { (app: $0["app"].string ?? "", status: $0["state"].string ?? "") }
                let ownState = AgentStatus.effective(hook: b["agent"]["state"].string, harness: b["agent"]["harness"].string, records: records)
                let agentState = gone && b["agent"]["state"].string != nil ? "ended" : ownState
                info[bid] = BlockInfo(title: b["title"].string ?? "", label: b["label"].string ?? "",
                                      cols: b["cols"].int ?? 0, rows: b["rows"].int ?? 0, sizeOwner: b["size_owner"].string,
                                      foreground: foreground, cwd: cwd,
                                      harness: gone ? nil : b["agent"]["harness"].string,
                                      agentState: agentState, exited: b["exited"].bool ?? false, exitCode: b["exit_code"].int ?? 0,
                                      home: h.isLocal ? nil : h.home)
            }
            blocks = info
            // Closed panes leave nothing behind in the bell's rate limit.
            let live = Set(info.keys)
            bellPolicy.prune(keeping: live)
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
                        // Not from an engine since replaced (reconnect, or
                        // another host), nor for a pane since detached.
                        guard let self, self.engine === engine, self.attached.contains(b) else { return }
                        self.frames[b] = frame
                        self.onFrame?(b)
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

    private func handleEvent(_ ev: JSON, from h: HostConnection) {
        let here = h === current
        switch ev["type"].string {
        case "session_created", "session_destroyed":
            reloadSessions(h)
        case "session_event":
            if ev["payload"]["event"]["payload"]["name"].string == "child_exited", here { scheduleReload() }
            // Session IDs are unique across hosts, but only this host's
            // session can be the one on screen.
            let sid = here ? ev["payload"]["session_id"].string : nil
            let inner = ev["payload"]["event"]
            let type = inner["type"].string
            if type == "block_event" {
                let name = inner["payload"]["name"].string
                if name == "agent_changed" { reloadAgents(h) }
                if name == "program_status_changed" {
                    scheduleAgentReload(h)
                    if sid == currentSession { scheduleReload() }
                }
                if name == "bell", sid == currentSession, let b = inner["payload"]["block_id"].string { bell(block: b) }
                if name == "clipboard_written", here {
                    programCopied(block: inner["payload"]["block_id"].string ?? "", text: inner["payload"]["payload"]["text"].string ?? "")
                }
                if name == "title_changed" || name == "size_changed" || name == "process_changed", sid == currentSession { scheduleReload() }
            } else if sid == currentSession {
                scheduleReload()
            }
            if type == "block_closed" || type == "block_created" { reloadSessions(h) }
        default: break
        }
    }

    /// Coalesces bursts of status reports (progress updates) into one reload.
    private var agentReloadPending: Set<String> = []
    private func scheduleAgentReload(_ h: HostConnection) {
        guard !agentReloadPending.contains(h.id) else { return }
        agentReloadPending.insert(h.id)
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.25) { [weak self, weak h] in
            guard let self, let h else { return }
            self.agentReloadPending.remove(h.id)
            self.reloadAgents(h)
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

    /// A new session on another host, which becomes the current one.
    func newSession(onHost id: String) {
        guard let h = host(id: id) else { return }
        switchHost(to: h)
        newSession()
    }

    /// A new session on the current host, starting in that account's home.
    func newSession(label: String = "") {
        let h = current
        var p: [String: JSON] = ["label": .string(label)]
        if h.isLocal { p["cwd"] = .string(NSHomeDirectory()) } // older servers: an empty cwd was their /
        h.call("session.create", .object(p)) { [weak self, weak h] j in
            guard let self, let h else { return }
            self.reloadSessions(h)
            self.select(session: j["session_id"].string)
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
    // MARK: bell

    private var bellPolicy = BellPolicy()
    /// Flashes a pane (set by the app).
    var onFlash: ((String) -> Void)?

    /// A program rang the terminal bell (BEL). What happens is the `bell`
    /// setting's, rate-limited by BellPolicy. Only the open session's panes
    /// can flash; bells elsewhere are dropped. Finished agents show on their
    /// tab already, by their status.
    func bell(block: String) {
        guard blocks[block] != nil else { return }
        for effect in bellPolicy.ring(block: block, setting: settings.bell, appActive: NSApp.isActive) {
            switch effect {
            case .flash: onFlash?(block)
            case .sound: NSSound.beep()
            case .bounce: NSApp.requestUserAttention(.informationalRequest)
            }
        }
    }

    func isIn(_ block: String, _ w: WindowInfo) -> Bool { w.layout?.rects().contains { $0.0 == block } ?? false }

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

    /// The programs of a tab's other panes, for the tiles stacked behind its
    /// icon: up to two, nearest first.
    func otherPrograms(of w: WindowInfo) -> [Program] {
        let main = program(of: w)
        var seen: [Program] = []
        for id in w.layout?.rects().map(\.0) ?? [] {
            guard let p = blocks[id]?.programInfo, p != main, !seen.contains(p) else { continue }
            seen.append(p)
        }
        // Panes of the same program still show as panes.
        let panes = (w.layout?.rects().count ?? 1) - 1
        while seen.count < min(panes, 2) { seen.append(main) }
        return Array(seen.prefix(2))
    }

    func summary(of w: WindowInfo) -> TabSummary {
        let ids = Set(w.layout?.rects().map(\.0) ?? [])
        let mine = agents.filter { ids.contains($0.blockID) && isLive($0) }
        let info = w.focused.flatMap { blocks[$0] }
        let label = w.label.isEmpty ? (info?.label ?? "") : w.label
        return TabSummary.make(label: label, program: info?.program ?? "shell", dir: info?.shortDir ?? "",
                               agents: mine.map { AgentSnapshot(state: $0.state, task: $0.task, message: $0.message) },
                               doneSeen: mine.filter { $0.state == "done" }.allSatisfy(doneSeen))
    }

    /// An agent counts while its pane hasn't gone back to the shell. Only
    /// the open session's panes are known here; the server ends the others.
    func isLive(_ a: AgentInfo) -> Bool { blocks[a.blockID].map { $0.harness != nil } ?? true }

    private var places: [AgentPlace] {
        agents.filter(isLive).map { AgentPlace(session: $0.sessionID, state: $0.state, doneSeen: doneSeen($0)) }
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

    func newTab() { userActed(); call("session.new_window", ["session_id": sid, "layout": AppModel.layout(cwd: currentCwd())]) }

    /// A one-block layout, with a cwd only when there is one.
    static func layout(cwd: JSON) -> JSON { cwd.isNull ? [:] : ["cwd": cwd] }

    func split(horizontal: Bool, cols: Int? = nil, rows: Int? = nil) {
        guard let b = focusedBlock else { return }
        var p: [String: JSON] = ["session_id": sid, "block_id": .string(b), "direction": horizontal ? "horizontal" : "vertical",
                                 "layout": AppModel.layout(cwd: currentCwd())]
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

    /// Opens the Settings window (set by the app).
    var openSettings: (() -> Void)?
    /// Hive's actions for a context menu, by the app: the pane's own come first.
    var contextActions: (() -> [NSMenuItem])?

    /// Whether the program in a block uses the mouse. A quick read on the vt
    /// queue, which never waits on the main thread.
    func tracksMouse(block: String) -> Bool {
        let engine = engine
        return vt.sync { engine.tracksMouse(block: block) }
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
        if currentSession != agent.sessionID {
            select(session: agent.sessionID)
        }
        call("session.focus_block", ["session_id": .string(agent.sessionID), "block_id": .string(agent.blockID)]) { [weak self] _ in
            self?.reloadView()
        }
    }

    /// The directory new panes start in: the focused pane's. Nothing (the
    /// server's home) when it isn't known; on a remote host, never a path
    /// from this Mac.
    private func currentCwd() -> JSON {
        let fallback: JSON = current.isLocal ? .string(NSHomeDirectory()) : .null
        guard let f = focusedBlock, let d = blocks[f]?.cwd, d.hasPrefix("/") else { return fallback }
        if current.isLocal, !FileManager.default.fileExists(atPath: d) { return fallback }
        return .string(d)
    }

    // MARK: input and size

    /// The user is working here: the session to come back to at launch
    /// no longer takes over.
    private func userActed() { restoreSession = nil }

    func key(_ k: KeyInput) {
        userActed()
        guard let b = focusedBlock else { return }
        let engine = engine
        vt.async { engine.key(block: b, k) }
    }

    func text(_ s: String) {
        userActed()
        guard let b = focusedBlock else { return }
        let engine = engine
        vt.async { engine.text(block: b, s) }
    }

    func paste(_ s: String) {
        userActed()
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

    /// The link at a pane's cell, read from its mirror on the vt queue (a
    /// quick read; it never waits on the main thread).
    func link(block: String, col: Int, row: Int) -> TerminalLink? {
        let engine = engine
        return vt.sync { engine.mirror(block: block)?.link(col: col, row: row) }
    }

    /// Opens a link as Ghostty does: a web link in its app, a file on this
    /// Mac in its app (Preview for an image), a folder in Finder; another
    /// scheme after asking; a file that would run, shown in Finder instead.
    /// A remote pane's paths are on that machine: only its web links open.
    func open(_ link: TerminalLink, block: String) {
        userActed()
        let host = ProcessInfo.processInfo.hostName
        let names = [host, String(host.split(separator: ".").first ?? ""), Host.current().localizedName ?? ""].filter { !$0.isEmpty }
        let action = LinkAction.resolve(link.target, cwd: blocks[block]?.cwd, home: NSHomeDirectory(),
                                        local: current.isLocal, hostnames: names)
        // Test runs write down what would open, instead of opening it.
        if let log = ProcessInfo.processInfo.environment["HIVE_LINK_LOG"] {
            let line = "\(link.target) -> \(action)\n"
            if let h = FileHandle(forWritingAtPath: log) { h.seekToEndOfFile(); h.write(Data(line.utf8)); h.closeFile() }
            else { FileManager.default.createFile(atPath: log, contents: Data(line.utf8)) }
            return
        }
        switch action {
        case .open(let u):
            NSWorkspace.shared.open(u)
        case .reveal(let u):
            NSWorkspace.shared.activateFileViewerSelecting([u])
        case .confirm(let u):
            let app = NSWorkspace.shared.urlForApplication(toOpen: u).map { FileManager.default.displayName(atPath: $0.path) }
            let alert = NSAlert()
            alert.messageText = app.map { "Open this link with \($0)?" } ?? "No app opens this link"
            alert.informativeText = u.absoluteString
            if app != nil {
                alert.addButton(withTitle: "Open")
                alert.addButton(withTitle: "Cancel")
                if alert.runModal() == .alertFirstButtonReturn { NSWorkspace.shared.open(u) }
            } else {
                alert.runModal()
            }
        case .none:
            NSSound.beep()
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
        guard settings.confirmPaste else { then(true); return } // confirm-paste = false
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
