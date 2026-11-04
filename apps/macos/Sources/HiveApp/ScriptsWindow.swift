import AppKit
import HiveCore
import HiveKit
import SwiftUI

/// Hive → Scripts…: every script in a server's scripts folder, what it may
/// do and did lately, its switches, and a way to turn it off or let it only
/// watch.
@MainActor
final class ScriptsWindow {
    private var window: NSWindow?
    private var page: ScriptsPage?

    func show(model: AppModel, host: HostConnection?, script: String?) {
        let p = page ?? ScriptsPage(model: model)
        page = p
        p.open(host: host?.id, script: script)
        if let window {
            window.makeKeyAndOrderFront(nil)
            return
        }
        let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 780, height: 560),
                         styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
        w.title = "Scripts"
        w.isReleasedWhenClosed = false
        w.minSize = NSSize(width: 620, height: 400)
        w.contentView = NSHostingView(rootView: ScriptsView(model: model, page: p))
        w.center()
        w.setFrameAutosaveName("HiveScripts")
        w.makeKeyAndOrderFront(nil)
        window = w
    }
}

/// What the window shows, for one host at a time.
@MainActor
final class ScriptsPage: ObservableObject {
    let model: AppModel
    @Published var hostID = HostConnection.localID
    @Published var scripts: [ScriptInfo] = []
    @Published var selected: String?
    @Published var activity: [ActivityEntry] = []
    @Published var loaded = false
    @Published var problem: String?
    /// The last refusal per option key, shown under it.
    @Published var optionProblem: [String: String] = [:]
    /// A script asked for (from a chip) that isn't in the folder: a
    /// one-shot run from the command line.
    @Published var missing: String?

    init(model: AppModel) { self.model = model }

    /// Shows a host, and a script on it when one is named.
    func open(host: String?, script: String?) {
        if let host, host != hostID {
            hostID = host
            scripts = []
            activity = []
            selected = nil
        }
        if let script { selected = script }
        missing = nil
        reload(asked: script)
    }

    /// The Picker's binding: another server.
    var hostBinding: Binding<String> {
        Binding(get: { self.hostID }, set: { self.open(host: $0, script: nil) })
    }

    var host: HostConnection? { model.host(id: hostID) ?? model.host(id: HostConnection.localID) }
    var current: ScriptInfo? { scripts.first { $0.name == selected } }

    func reload(asked: String? = nil) {
        guard let h = host, h.connected else {
            problem = "Not connected to this server. hive keeps trying."
            loaded = true
            return
        }
        if h.id != hostID { hostID = h.id } // it was another host, gone now
        h.call("script.list") { [weak self] r in
            guard let self else { return }
            switch r {
            case .success(let j):
                self.scripts = ScriptInfo.list(j)
                self.problem = nil
                if let asked, !self.scripts.contains(where: { $0.name == asked }) { self.missing = asked }
                if self.selected == nil || !self.scripts.contains(where: { $0.name == self.selected }) {
                    // What needs you first, else the first.
                    self.selected = (self.scripts.first(where: \.needsYou) ?? self.scripts.first)?.name
                }
            case .failure(let e):
                self.problem = e.message
            }
            self.loaded = true
            self.loadActivity()
        }
    }

    func loadActivity() {
        guard let h = host, let name = selected else { activity = []; return }
        h.call("audit.list", ["name": .string(name), "tail": 20]) { [weak self] r in
            guard let self, self.selected == name, case .success(let j) = r else { return }
            self.activity = ActivityEntry.list(j).reversed()
        }
    }

    /// A change by you: the server answers with the script as it is now.
    private func change(_ method: String, _ params: [String: JSON], key: String? = nil) {
        guard let h = host else { return }
        h.call(method, .object(params)) { [weak self] r in
            guard let self else { return }
            switch r {
            case .success(let j):
                if let i = self.scripts.firstIndex(where: { $0.name == j["name"].string }) { self.scripts[i] = ScriptInfo(j) }
                if let key { self.optionProblem[key] = nil }
                self.problem = nil
            case .failure(let e):
                if let key { self.optionProblem[key] = e.message } else { self.problem = e.message }
            }
            self.loadActivity()
        }
    }

    func setOn(_ s: ScriptInfo, _ on: Bool) { change(on ? "script.enable" : "script.disable", ["name": .string(s.name)]) }
    func setWatch(_ s: ScriptInfo, _ watch: Bool) {
        change("script.set_mode", ["name": .string(s.name), "mode": .string(watch ? "watch" : "act")])
    }
    func allow(_ s: ScriptInfo) { change("script.grant", ["name": .string(s.name)]) }
    func set(_ s: ScriptInfo, _ o: ScriptOption, _ v: JSON) {
        change("script.set_option", ["name": .string(s.name), "key": .string(o.key), "value": v], key: o.key)
    }
}

struct ScriptsView: View {
    @ObservedObject var model: AppModel
    @ObservedObject var page: ScriptsPage

    var body: some View {
        let s = model.style
        let bare = page.loaded && page.scripts.isEmpty && page.problem == nil && model.hosts.filter(\.connected).count <= 1
        HStack(spacing: 0) {
            if !bare {
                list(s).frame(width: 240)
                Divider()
            }
            Group {
                if let script = page.current {
                    ScriptDetail(page: page, script: script, style: s).id(script.name)
                } else {
                    empty(s)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .onChange(of: model.scriptsRevision) { _ in page.reload() }
        .onChange(of: page.selected) { _ in page.loadActivity() }
    }

    private func list(_ s: Style) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            let hosts = model.hosts.filter(\.connected)
            if hosts.count > 1 {
                Picker("Server", selection: page.hostBinding) {
                    ForEach(hosts, id: \.id) { h in Text(h.isLocal ? "This Mac" : h.displayName).tag(h.id) }
                }
                .labelsHidden()
                .padding(10)
            }
            if let m = page.missing {
                Text("\(m) ran once from the command line, so it isn't listed here. hive script audit \(m) shows what it did.")
                    .font(.caption).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, 12).padding(.vertical, 6)
            }
            List(selection: $page.selected) {
                ForEach(page.scripts) { sc in
                    ScriptRow(script: sc, style: s).tag(sc.name)
                }
            }
            .listStyle(.sidebar)
            if let p = page.problem {
                Label(p, systemImage: "exclamationmark.triangle.fill").font(.caption).foregroundStyle(s.blockedText.swiftUI)
                    .padding(10)
            }
        }
    }

    private func empty(_ s: Style) -> some View {
        VStack(spacing: 10) {
            Image(systemName: "bolt").font(.system(size: 28, weight: .light)).foregroundStyle(s.secondary.swiftUI)
            if !page.loaded {
                Text("Loading…").foregroundStyle(.secondary)
            } else if page.scripts.isEmpty {
                Text("No scripts on this server").font(.headline)
                Text("Scripts run on the server and react to your agents and terminals. Add a starter with hive script add, then allow it here.")
                    .font(.callout).foregroundStyle(.secondary).multilineTextAlignment(.center).frame(maxWidth: 360)
                Text("hive script add").font(.system(.callout, design: .monospaced)).textSelection(.enabled)
                    .padding(.horizontal, 8).padding(.vertical, 3)
                    .background(RoundedRectangle(cornerRadius: 6).fill(s.secondary.swiftUI.opacity(0.12)))
            } else {
                Text("Choose a script").foregroundStyle(.secondary)
            }
        }
        .padding(30)
    }
}

/// A script in the list: a hexagon in its state's color, its name, and its
/// state in words.
struct ScriptRow: View {
    let script: ScriptInfo
    let style: Style

    /// The brand's states: honey is at work, coral needs you (an OK to give, or it failed), quiet is
    /// watching or off. Sage is an agent's "done", which a script never is.
    static func color(_ sc: ScriptInfo, _ s: Style) -> Color {
        switch sc.state {
        case "running": return sc.watching ? s.secondary.swiftUI : s.accent.swiftUI
        case "pending", "failed", "quarantined": return s.blocked.swiftUI
        default: return s.secondary.swiftUI.opacity(0.6)
        }
    }

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Image(systemName: "hexagon.fill").font(.system(size: 8))
                .foregroundStyle(Self.color(script, style))
                .frame(width: 12).padding(.top, 4)
            VStack(alignment: .leading, spacing: 1) {
                Text(script.name).font(.system(size: 12.5, weight: .medium)).lineLimit(1)
                Text(script.stateText).font(.system(size: 11)).foregroundStyle(style.secondary.swiftUI).lineLimit(1)
            }
        }
        .padding(.vertical, 2)
    }
}

struct ScriptDetail: View {
    @ObservedObject var page: ScriptsPage
    let script: ScriptInfo
    let style: Style

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                header
                if script.state == "pending" { allowBox } else { controls }
                if !script.options.isEmpty { switches }
                recent
                footer
            }
            .padding(22)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(script.name).font(.system(size: 20, weight: .semibold))
                Spacer()
                if script.state != "pending" {
                    Text(script.on ? "On" : "Off").foregroundStyle(.secondary)
                    Toggle("On", isOn: Binding(get: { script.on }, set: { page.setOn(script, $0) }))
                        .toggleStyle(.switch).labelsHidden()
                        .help(script.on ? "Turn Off: the script stops and its marks leave the panes" : "Turn On")
                }
            }
            Text(script.stateText).font(.callout)
                .foregroundStyle(script.needsYou && script.state != "pending" ? style.blockedText.swiftUI : style.secondary.swiftUI)
            if !script.description.isEmpty {
                Text(script.description).font(.body).fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    private var allowBox: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("It asks to:").fixedSize(horizontal: false, vertical: true)
            permissionList
            if let w = script.warning {
                Label(w, systemImage: "exclamationmark.triangle").font(.callout).foregroundStyle(style.blockedText.swiftUI)
            }
            HStack {
                // The decision this page is for: the one honey button.
                Button("Allow") { page.allow(script) }.keyboardShortcut(.defaultAction)
                    .buttonStyle(.borderedProminent).tint(style.accent.swiftUI)
                Text("You can turn the script off, or let it only watch, at any time.").font(.caption).foregroundStyle(.secondary)
            }
        }
        .padding(14)
        .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(style.accent.swiftUI.opacity(0.10)))
    }

    private var controls: some View {
        VStack(alignment: .leading, spacing: 10) {
            if script.state != "pending", !script.shownPermissions.isEmpty {
                Text("It may:").foregroundStyle(.secondary)
                permissionList
            }
            if script.state != "pending" {
                HStack(alignment: .top) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Watch only")
                        Text("The script can read terminals, mark panes and notify you. Anything else it tries is refused and listed below.")
                            .font(.caption).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                    }
                    Spacer(minLength: 16)
                    Toggle("Watch only", isOn: Binding(get: { script.watching }, set: { page.setWatch(script, $0) }))
                        .toggleStyle(.switch).labelsHidden()
                }
                .disabled(!script.on)
            }
        }
    }

    private var permissionList: some View {
        VStack(alignment: .leading, spacing: 5) {
            ForEach(script.shownPermissions, id: \.self) { p in
                let w = ScriptInfo.word[p]
                Label {
                    Text(w?.text ?? p)
                } icon: {
                    Image(systemName: w?.symbol ?? "questionmark").foregroundStyle(p == "input" || p == "screen" ? style.accent.swiftUI : .secondary)
                        .frame(width: 18)
                }
            }
        }
        .padding(.leading, 2)
    }

    private var switches: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Options").font(.headline)
            ForEach(script.options) { o in
                OptionRow(option: o, problem: page.optionProblem[o.key]) { page.set(script, o, $0) }
            }
        }
    }

    private var recent: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Recent activity").font(.headline)
            if page.activity.isEmpty {
                Text("Nothing yet. Every change the script makes shows here: typing, opening or closing panes, and asking the app to do something.")
                    .font(.callout).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            }
            ForEach(page.activity) { e in
                HStack(alignment: .firstTextBaseline, spacing: 10) {
                    Text(e.time.map(Self.when) ?? "").font(.system(size: 11).monospacedDigit()).foregroundStyle(.secondary)
                        .frame(width: 64, alignment: .leading)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(e.text(options: script.options)).font(.callout).fixedSize(horizontal: false, vertical: true).textSelection(.enabled)
                        if let p = e.problem { Text(p).font(.caption).foregroundStyle(style.blockedText.swiftUI) }
                    }
                }
            }
        }
    }

    private var footer: some View {
        HStack {
            Text(script.path.replacingOccurrences(of: NSHomeDirectory(), with: "~"))
                .font(.system(.caption, design: .monospaced)).foregroundStyle(.secondary)
                .lineLimit(1).truncationMode(.middle).textSelection(.enabled)
            Spacer()
            if page.host?.isLocal == true, !script.path.isEmpty {
                Button("Show in Finder") { NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: script.path)]) }
            }
        }
    }

    /// "18:04" today, "Mon 18:04" this week, else the date.
    static func when(_ d: Date) -> String {
        let f = DateFormatter()
        let cal = Calendar.current
        f.dateFormat = cal.isDateInToday(d) ? "HH:mm" : d > Date().addingTimeInterval(-6 * 86_400) ? "EEE HH:mm" : "d MMM"
        return f.string(from: d)
    }
}

/// One switch: on/off, a number, or text, saved when it changes: Enter,
/// Tab or clicking away commit what's typed, once.
struct OptionRow: View {
    let option: ScriptOption
    let problem: String?
    let save: (JSON) -> Void
    @State private var text = ""
    @FocusState private var focused: Bool

    private func commit() {
        guard text != Self.show(option.value) else { return }
        if option.type == "number" {
            if let n = Double(text) { save(.number(n)) }
        } else {
            save(.string(text))
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            switch option.type {
            case "bool":
                HStack {
                    Text(option.label)
                    Spacer()
                    Toggle(option.label, isOn: Binding(get: { option.value.bool ?? false }, set: { save(.bool($0)) }))
                        .toggleStyle(.switch).labelsHidden()
                }
            case "number":
                HStack {
                    Text(option.label)
                    Spacer()
                    TextField("", text: $text).frame(width: 60).multilineTextAlignment(.trailing)
                        .focused($focused).onSubmit(commit)
                    // Steps from what's in the field, typed or not.
                    Stepper("", onIncrement: { step(1) }, onDecrement: { step(-1) }).labelsHidden()
                }
            default:
                HStack {
                    Text(option.label)
                    Spacer()
                    TextField("", text: $text).frame(width: 200).focused($focused).onSubmit(commit)
                }
            }
            if let problem { Text(problem).font(.caption).foregroundStyle(.red) }
        }
        .onAppear { text = Self.show(option.value) }
        .onChange(of: option.value) { v in if !focused { text = Self.show(v) } }
        .onChange(of: focused) { f in if !f { commit() } }
    }

    private func step(_ by: Double) {
        let now = Double(text) ?? option.value.double ?? 0
        let next = min(option.max ?? .infinity, max(option.min ?? -.infinity, now + by))
        text = Self.show(.number(next))
        save(.number(next))
    }

    static func show(_ v: JSON) -> String {
        if let d = v.double { return d == d.rounded() ? String(Int(d)) : String(d) }
        return v.string ?? ""
    }
}
