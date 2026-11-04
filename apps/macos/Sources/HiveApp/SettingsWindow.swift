import AppKit
import Combine
import HiveCore
import HiveRender
import SwiftUI

/// Hive → Settings… (⌘,): a sidebar of pages and a search field. Every row
/// writes the settings file, which stays the source of truth; Advanced opens
/// it. Labels and sentences come from SettingsIndex, so search finds a row by
/// what it does.
@MainActor
final class SettingsWindow {
    private var window: NSWindow?
    /// What the window can open elsewhere in the app (set by the app).
    var actions = SettingsActions()

    func show(model: AppModel, page: SettingsIndex.Page? = nil) {
        if let window {
            nav.query = ""
            if let page { nav.go(page) }
            window.makeKeyAndOrderFront(nil)
            DispatchQueue.main.async { window.makeFirstResponder(nil) }
            return
        }
        if let page { nav.start(at: page) }
        let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 760, height: 540),
                         styleMask: [.titled, .closable, .miniaturizable, .fullSizeContentView], backing: .buffered, defer: false)
        w.title = "Settings"
        w.titleVisibility = .hidden
        w.titlebarAppearsTransparent = true
        w.isReleasedWhenClosed = false
        w.isMovableByWindowBackground = true
        w.contentView = NSHostingView(rootView: SettingsView(model: model, store: model.settingsStore, nav: nav, actions: actions))
        // Popups, switches and steppers follow hive's theme, not the Mac's.
        w.appearance = NSAppearance(named: model.style.dark ? .darkAqua : .aqua)
        styleWatch = model.$style.sink { [weak w] s in w?.appearance = NSAppearance(named: s.dark ? .darkAqua : .aqua) }
        w.center()
        w.setFrameAutosaveName("HiveSettings2")
        w.makeKeyAndOrderFront(nil)
        // Opens on the page, not in the search field.
        DispatchQueue.main.async { w.makeFirstResponder(nil) }
        window = w
    }

    let nav = SettingsNav()
    private var styleWatch: AnyCancellable?
}

/// What the settings window can open elsewhere.
struct SettingsActions {
    var shareHosts: () -> Void = {}
    var addHost: () -> Void = {}
    var scripts: () -> Void = {}
    var shortcuts: () -> Void = {}
}

/// The page on screen, with back and forward as in a browser, and the search.
@MainActor
final class SettingsNav: ObservableObject {
    @Published private(set) var page: SettingsIndex.Page = .server
    @Published var query = ""
    /// A row to point at once its page shows (picked from search).
    @Published var spotlight: String?
    private var back: [SettingsIndex.Page] = []
    private var forward: [SettingsIndex.Page] = []

    var canGoBack: Bool { !back.isEmpty }
    var canGoForward: Bool { !forward.isEmpty }

    /// The first page, without history.
    func start(at p: SettingsIndex.Page) { page = p }

    func go(_ p: SettingsIndex.Page, spotlight id: String? = nil) {
        query = ""
        spotlight = id
        guard p != page else { return }
        back.append(page)
        forward.removeAll()
        page = p
    }

    func goBack() {
        guard let p = back.popLast() else { return }
        forward.append(page)
        page = p
    }

    func goForward() {
        guard let p = forward.popLast() else { return }
        back.append(page)
        page = p
    }
}

struct SettingsView: View {
    @ObservedObject var model: AppModel
    @ObservedObject var store: SettingsStore
    @ObservedObject var nav: SettingsNav
    let actions: SettingsActions

    var body: some View {
        let s = model.style
        HStack(spacing: 0) {
            SettingsSidebar(model: model, nav: nav)
                .frame(width: 214)
            VStack(alignment: .leading, spacing: 0) {
                SettingsTitle(nav: nav, style: s)
                ScrollViewReader { scroll in
                    ScrollView {
                        VStack(alignment: .leading, spacing: 0) {
                            if nav.query.trimmingCharacters(in: .whitespaces).isEmpty {
                                page(nav.page)
                            } else {
                                SettingsSearchResults(nav: nav, style: s)
                            }
                        }
                        .padding(.horizontal, 22).padding(.bottom, 22)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .onChange(of: nav.spotlight) { _, id in
                        guard let id else { return }
                        DispatchQueue.main.asyncAfter(deadline: .now() + 0.05) {
                            withAnimation(.easeOut(duration: 0.25)) { scroll.scrollTo(id, anchor: .center) }
                        }
                    }
                }
            }
            .background(s.windowTop.swiftUI.opacity(0.0))
        }
        .background(LinearGradient(colors: [s.windowTop.swiftUI, s.windowBottom.swiftUI], startPoint: .top, endPoint: .bottom))
        .tint(s.accent.swiftUI)
        .environment(\.colorScheme, s.dark ? .dark : .light)
        .frame(minWidth: 760, minHeight: 540)
    }

    @ViewBuilder func page(_ p: SettingsIndex.Page) -> some View {
        let ctx = SettingsContext(model: model, store: store, nav: nav, actions: actions)
        switch p {
        case .server: ServerPage(ctx: ctx)
        case .appearance: AppearancePage(ctx: ctx)
        case .terminal: TerminalPage(ctx: ctx)
        case .panes: PanesPage(ctx: ctx)
        case .notifications: NotificationsPage(ctx: ctx)
        case .agents: AgentsPage(ctx: ctx)
        case .hosts: HostsPage(ctx: ctx)
        case .shortcuts: ShortcutsPage(ctx: ctx)
        case .advanced: AdvancedPage(ctx: ctx)
        }
    }
}

/// What every page needs.
@MainActor
struct SettingsContext {
    let model: AppModel
    let store: SettingsStore
    let nav: SettingsNav
    let actions: SettingsActions
    var s: HiveCore.Settings { store.settings }
    var style: Style { model.style }

    /// Writes an app setting.
    func set(_ key: String, _ value: String) { store.set(key, value) }

    /// Writes a server setting and asks this Mac's server to read it.
    func setServer(_ key: String, _ value: String) {
        let model = model
        store.set(key, value) { model.host(id: HostConnection.localID)?.call("config.reload") }
    }
}

// MARK: sidebar and title

struct SettingsSidebar: View {
    @ObservedObject var model: AppModel
    @ObservedObject var nav: SettingsNav
    @FocusState private var searching: Bool

    var body: some View {
        let s = model.style
        VStack(alignment: .leading, spacing: 2) {
            Color.clear.frame(height: 34) // the traffic lights
            HStack(spacing: 6) {
                Image(systemName: "magnifyingglass").font(.system(size: 11)).foregroundStyle(s.secondary.swiftUI)
                TextField("Search settings", text: $nav.query)
                    .textFieldStyle(.plain).font(.system(size: 12.5))
                    .focused($searching)
                if !nav.query.isEmpty {
                    Button { nav.query = "" } label: { Image(systemName: "xmark.circle.fill").font(.system(size: 11)) }
                        .buttonStyle(.plain).foregroundStyle(s.secondary.swiftUI)
                        .accessibilityLabel("Clear search")
                }
            }
            .padding(.horizontal, 8).frame(height: 28)
            .background(RoundedRectangle(cornerRadius: 7, style: .continuous).fill(s.text.swiftUI.opacity(0.07)))
            .background(Button("") { searching = true }.keyboardShortcut("f").opacity(0).accessibilityHidden(true))
            .overlay(RoundedRectangle(cornerRadius: 7, style: .continuous)
                .strokeBorder(searching ? s.accent.swiftUI.opacity(0.7) : .clear, lineWidth: 1))
            .padding(.bottom, 10)

            ForEach(SettingsIndex.Page.allCases, id: \.self) { p in
                if p == .appearance || p == .agents || p == .advanced { Color.clear.frame(height: 8) }
                SidebarItem(page: p, model: model, selected: nav.query.isEmpty && nav.page == p) { nav.go(p) }
            }
            Spacer()
        }
        .padding(.horizontal, 12)
    }
}

struct SidebarItem: View {
    let page: SettingsIndex.Page
    @ObservedObject var model: AppModel
    let selected: Bool
    let action: () -> Void
    @State private var hover = false

    var body: some View {
        let s = model.style
        Button(action: action) {
            HStack(spacing: 9) {
                ZStack {
                    RoundedRectangle(cornerRadius: 6, style: .continuous)
                        .fill(page == .server ? .clear : s.text.swiftUI.opacity(0.06))
                    RoundedRectangle(cornerRadius: 6, style: .continuous)
                        .strokeBorder(selected && page != .server ? s.accent.swiftUI.opacity(0.8) : .clear, lineWidth: 1)
                    if page == .server {
                        Image(systemName: "hexagon.fill").font(.system(size: 17))
                            .foregroundStyle(serverUp ? s.done.swiftUI : s.blocked.swiftUI)
                    } else {
                        Image(systemName: page.symbol).font(.system(size: 11.5))
                            .foregroundStyle(selected ? s.accent.swiftUI : s.secondary.swiftUI)
                    }
                }
                .frame(width: 24, height: 24)
                VStack(alignment: .leading, spacing: 0) {
                    Text(page.title).font(.system(size: 12.5, weight: selected ? .medium : .regular))
                        .foregroundStyle(s.text.swiftUI)
                    if page == .server {
                        Text(serverUp ? "Running" : "Not running").font(.system(size: 11)).foregroundStyle(s.secondary.swiftUI)
                    }
                }
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 6).padding(.vertical, page == .server ? 5 : 3)
            .background(RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(selected ? s.accent.swiftUI.opacity(0.14) : s.text.swiftUI.opacity(hover ? 0.05 : 0)))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hover = $0 }
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private var serverUp: Bool { model.host(id: HostConnection.localID)?.connected ?? false }
}

struct SettingsTitle: View {
    @ObservedObject var nav: SettingsNav
    let style: Style

    var body: some View {
        HStack(spacing: 12) {
            HStack(spacing: 0) {
                Button { nav.goBack() } label: { Image(systemName: "chevron.left").frame(width: 26, height: 24) }
                    .disabled(!nav.canGoBack).help("Back").accessibilityLabel("Back")
                Rectangle().fill(style.text.swiftUI.opacity(0.12)).frame(width: 1, height: 14)
                Button { nav.goForward() } label: { Image(systemName: "chevron.right").frame(width: 26, height: 24) }
                    .disabled(!nav.canGoForward).help("Forward").accessibilityLabel("Forward")
            }
            .buttonStyle(.plain)
            .font(.system(size: 11, weight: .semibold))
            .foregroundStyle(style.secondary.swiftUI)
            .background(Capsule().fill(style.text.swiftUI.opacity(0.06)))
            Text(nav.query.isEmpty ? nav.page.title : "Search").font(.system(size: 14, weight: .semibold))
                .foregroundStyle(style.text.swiftUI)
            Spacer()
        }
        .padding(.horizontal, 22).frame(height: 52)
    }
}

// MARK: building blocks

/// A bold section title over a card.
struct SettingsSection<Content: View>: View {
    let title: String
    var note: String? = nil
    let style: Style
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            VStack(alignment: .leading, spacing: 1) {
                Text(title).font(.system(size: 12.5, weight: .semibold)).foregroundStyle(style.text.swiftUI)
                if let note { Text(note).font(.system(size: 11.5)).foregroundStyle(style.secondary.swiftUI) }
            }
            .padding(.leading, 2)
            VStack(spacing: 0) { content }
                .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(style.card.swiftUI.opacity(style.dark ? 0.7 : 1)))
                .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(style.border.swiftUI.opacity(0.8), lineWidth: 1))
        }
        .padding(.top, 16)
    }
}

/// One row: its label (and, when the index says so, its sentence) and its control.
struct SettingsRow<Control: View>: View {
    let id: String
    let style: Style
    @ObservedObject var nav: SettingsNav
    var divider = true
    @ViewBuilder let control: Control
    @State private var lit = false

    var body: some View {
        let e = SettingsIndex.entry(id)
        VStack(spacing: 0) {
            if divider { Rectangle().fill(style.border.swiftUI.opacity(0.6)).frame(height: 1).padding(.leading, 12) }
            HStack(alignment: .center, spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(e.label).font(.system(size: 12.5)).foregroundStyle(style.text.swiftUI)
                    if e.always {
                        Text(e.help).font(.system(size: 11.5)).foregroundStyle(style.secondary.swiftUI)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                Spacer(minLength: 8)
                control.accessibilityLabel(e.label)
            }
            .padding(.horizontal, 12).padding(.vertical, 8).frame(minHeight: 38)
            .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(style.accent.swiftUI.opacity(lit ? 0.16 : 0)).padding(2))
        }
        .id(id)
        .help(e.always ? "" : e.help)
        .accessibilityElement(children: .contain)
        .accessibilityHint(e.help)
        .onAppear { light(nav.spotlight) }
        .onChange(of: nav.spotlight) { _, v in light(v) }
    }

    /// Picked from search: the row glows once, then settles.
    private func light(_ spot: String?) {
        guard spot == id else { return }
        lit = true
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.2) {
            withAnimation(.easeOut(duration: 0.6)) { lit = false }
            if nav.spotlight == id { nav.spotlight = nil }
        }
    }
}

/// A small terminal showing the font, as panes draw it.
struct FontPreview: View {
    let family: String
    let size: Double
    let style: Style
    var cursorOnly = false

    var body: some View {
        let t = style.terminal
        let fg = Color(red: Double(t.foreground.r) / 255, green: Double(t.foreground.g) / 255, blue: Double(t.foreground.b) / 255)
        let bg = Color(red: Double(t.background.r) / 255, green: Double(t.background.g) / 255, blue: Double(t.background.b) / 255)
        let green = Color(red: Double(t.palette[2].r) / 255, green: Double(t.palette[2].g) / 255, blue: Double(t.palette[2].b) / 255)
        // The face panes draw with (the bundled one isn't registered app-wide).
        let font = Font(FontSet(family: family, pointSize: min(size, 15), scale: 1).face(bold: false, italic: false))
        VStack(alignment: .leading, spacing: 2) {
            if !cursorOnly {
                (Text("~/hive ").foregroundColor(green) + Text("main ").foregroundColor(style.secondary.swiftUI) + Text("$ go test ./...").foregroundColor(fg))
                (Text("ok   hive/internal/server  ").foregroundColor(fg) + Text("1.24s").foregroundColor(style.secondary.swiftUI))
            }
            HStack(spacing: 0) {
                Text(cursorOnly ? "~/hive $ " : "❯ ").foregroundColor(cursorOnly ? green : style.accent.swiftUI)
                RoundedRectangle(cornerRadius: 1).fill(style.accent.swiftUI).frame(width: 7, height: 14)
            }
        }
        .font(font)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 12).padding(.vertical, 9)
        .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(bg))
        .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).strokeBorder(style.border.swiftUI.opacity(0.8), lineWidth: 1))
        .padding(10)
        .accessibilityHidden(true)
    }
}

/// A theme as a tile: System split down the middle, Day, Night.
struct ThemeTile: View {
    let appearance: Appearance
    let selected: Bool
    let style: Style
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(spacing: 6) {
                ZStack {
                    switch appearance {
                    case .system:
                        HStack(spacing: 0) { mini(Style.light); mini(Style.dark) }
                    case .day: mini(Style.light)
                    case .night: mini(Style.dark)
                    }
                }
                .frame(width: 96, height: 60)
                .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).strokeBorder(style.border.swiftUI, lineWidth: 1))
                .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .strokeBorder(selected ? style.accent.swiftUI : .clear, lineWidth: 2).padding(-3))
                Text(appearance.title).font(.system(size: 11.5, weight: selected ? .medium : .regular))
                    .foregroundStyle(selected ? style.text.swiftUI : style.secondary.swiftUI)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(appearance.title)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private func mini(_ s: Style) -> some View {
        ZStack(alignment: .topLeading) {
            s.windowTop.swiftUI
            VStack(spacing: 3) {
                HStack(spacing: 3) {
                    RoundedRectangle(cornerRadius: 2).fill(s.accent.swiftUI.opacity(0.8)).frame(width: 10, height: 4)
                    RoundedRectangle(cornerRadius: 2).fill(s.text.swiftUI.opacity(0.25)).frame(height: 4)
                }
                HStack(spacing: 3) {
                    RoundedRectangle(cornerRadius: 3).fill(s.card.swiftUI)
                    RoundedRectangle(cornerRadius: 3).fill(s.card.swiftUI)
                }
            }
            .padding(6)
        }
    }
}

// MARK: pages

struct ServerPage: View {
    let ctx: SettingsContext
    @State private var now = Date()
    /// Bumped when server.version comes back, so the Tailscale line redraws.
    @State private var version = 0

    var body: some View {
        let st = ctx.style, local = ctx.model.host(id: HostConnection.localID)
        let up = local?.connected ?? false
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 12) {
                Image(systemName: "hexagon.fill").font(.system(size: 26)).foregroundStyle(up ? st.done.swiftUI : st.blocked.swiftUI)
                VStack(alignment: .leading, spacing: 2) {
                    Text("hive server").font(.system(size: 13, weight: .semibold)).foregroundStyle(st.text.swiftUI)
                    Text(up ? (local?.serverStarted.map { runningFor(since: $0, now: now) } ?? "Running") : (local?.status ?? "Not running"))
                        .font(.system(size: 11.5)).foregroundStyle(st.secondary.swiftUI)
                }
                Spacer()
                if let v = local?.serverVersion {
                    Text(v).font(.system(size: 11, design: .monospaced)).foregroundStyle(st.secondary.swiftUI).textSelection(.enabled)
                }
            }
            .padding(14)
            .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(st.card.swiftUI.opacity(st.dark ? 0.7 : 1)))
            .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(st.border.swiftUI.opacity(0.8), lineWidth: 1))
            .padding(.top, 4)
            Text("The server keeps your sessions running while the app is closed. It runs in the background as part of Hive.")
                .font(.system(size: 11.5)).foregroundStyle(st.secondary.swiftUI).padding(.top, 6).padding(.leading, 2)

            SettingsSection(title: "Reach", style: st) {
                SettingsRow(id: "tailscale", style: st, nav: ctx.nav, divider: false) {
                    Toggle("", isOn: Binding(get: { ctx.s.tailscale }, set: { ctx.setServer("tailscale", $0 ? "true" : "false") }))
                        .toggleStyle(.switch).labelsHidden().controlSize(.small)
                }
                TailscaleState(ctx: ctx, version: version)
                SettingsRow(id: "tailnet", style: st, nav: ctx.nav) {
                    Toggle("", isOn: Binding(get: { ctx.s.listen.contains { $0.hasPrefix("tailnet://") } }, set: { on in
                        var l = ctx.s.listen.filter { !$0.hasPrefix("tailnet://") }
                        if on { l.append("tailnet://:4483") }
                        ctx.setServer("listen", l.joined(separator: " "))
                    })).toggleStyle(.switch).labelsHidden().controlSize(.small)
                }
            }
            SettingsSection(title: "Sessions", style: st) {
                SettingsRow(id: "resume-agents", style: st, nav: ctx.nav, divider: false) {
                    Toggle("", isOn: Binding(get: { ctx.s.resumeAgents }, set: { ctx.setServer("resume-agents", $0 ? "true" : "false") }))
                        .toggleStyle(.switch).labelsHidden().controlSize(.small)
                }
                SettingsRow(id: "scrollback-limit", style: st, nav: ctx.nav) {
                    Picker("", selection: Binding(get: { ctx.s.scrollbackLimit }, set: { ctx.setServer("scrollback-limit", SettingsFormat.size($0)) })) {
                        ForEach(SettingsFormat.scrollbackChoices(current: ctx.s.scrollbackLimit), id: \.self) { Text(SettingsFormat.megabytes($0)).tag($0) }
                    }
                    .labelsHidden().fixedSize()
                }
            }
        }
        .onReceive(Timer.publish(every: 30, on: .main, in: .common).autoconnect()) { now = $0 }
        .onAppear { ctx.model.host(id: HostConnection.localID)?.refreshVersion { version += 1 } }
        // While Built-in Tailscale waits for a sign-in in the browser, the line follows.
        .onReceive(Timer.publish(every: 3, on: .main, in: .common).autoconnect()) { _ in
            if ctx.s.tailscale || ctx.model.host(id: HostConnection.localID)?.tailscale != nil {
                ctx.model.host(id: HostConnection.localID)?.refreshVersion { version += 1 }
            }
        }
    }
}

/// Built-in Tailscale as the running server has it: waiting for a sign-in,
/// on the tailnet, or set to start with the server.
struct TailscaleState: View {
    let ctx: SettingsContext
    let version: Int

    var body: some View {
        let st = ctx.style
        let t = ctx.model.host(id: HostConnection.localID)?.tailscale
        // Only a web page: the link comes from the server's control server.
        let url = t?["auth_url"].string.flatMap(URL.init(string:)).flatMap { $0.scheme == "https" ? $0 : nil }
        let name = t?["dns_name"].string.map { $0.hasSuffix(".") ? String($0.dropLast()) : $0 }
        let line: String? = {
            if let e = t?["error"].string, !e.isEmpty { return "Tailscale: \(e)" }
            if url != nil { return "Sign in to add this server to your tailnet." }
            if let name, !name.isEmpty { return ctx.s.tailscale ? "On your tailnet as \(name)." : "On your tailnet as \(name) until the server starts again." }
            if t != nil { return "Starting." }
            return ctx.s.tailscale ? "Starts when the server starts again." : nil
        }()
        if let line {
            HStack(spacing: 10) {
                Text(line).font(.system(size: 11.5)).foregroundStyle(url != nil ? st.accentText.swiftUI : st.secondary.swiftUI)
                    .textSelection(.enabled)
                Spacer()
                if let url { Button("Sign In…") { NSWorkspace.shared.open(url) } }
            }
            .padding(.horizontal, 12).padding(.bottom, 8).id(version)
        }
    }
}

struct AppearancePage: View {
    let ctx: SettingsContext

    var body: some View {
        let st = ctx.style
        VStack(alignment: .leading, spacing: 0) {
            SettingsSection(title: "Theme", style: st) {
                HStack(spacing: 18) {
                    ForEach(Appearance.allCases, id: \.self) { a in
                        ThemeTile(appearance: a, selected: ctx.model.appearance == a, style: st) { ctx.model.setAppearance(a) }
                    }
                    Spacer()
                }
                .padding(14)
            }
            SettingsSection(title: "Terminal text", style: st) {
                FontPreview(family: ctx.s.fontFamily, size: ctx.s.fontSize, style: st)
                SettingsRow(id: "font-family", style: st, nav: ctx.nav) {
                    Picker("", selection: Binding(get: { ctx.s.fontFamily }, set: { ctx.set("font-family", $0) })) {
                        Text("JetBrains Mono").tag("")
                        ForEach(MonospacedFonts.families, id: \.self) { Text($0).tag($0) }
                        if !ctx.s.fontFamily.isEmpty, !MonospacedFonts.families.contains(ctx.s.fontFamily) {
                            Text("\(ctx.s.fontFamily) (not installed)").tag(ctx.s.fontFamily)
                        }
                    }
                    .labelsHidden().fixedSize()
                }
                SettingsRow(id: "font-size", style: st, nav: ctx.nav) {
                    Stepper(value: Binding(get: { ctx.s.fontSize }, set: { ctx.set("font-size", SettingsFormat.number($0)) }), in: 8...32, step: 0.5) {
                        Text("\(SettingsFormat.number(ctx.s.fontSize)) pt").font(.system(size: 12, design: .monospaced)).foregroundStyle(st.text.swiftUI)
                    }
                    .fixedSize()
                }
                if let p = ctx.store.fontProblem {
                    Text(p).font(.system(size: 11.5)).foregroundStyle(st.blockedText.swiftUI).padding(.horizontal, 12).padding(.bottom, 8)
                }
            }
            SettingsSection(title: "Motion", style: st) {
                SettingsRow(id: "motion", style: st, nav: ctx.nav, divider: false) {
                    Picker("", selection: Binding(get: { ctx.s.motion }, set: { ctx.set("motion", $0.rawValue) })) {
                        Text("Snappy").tag(Motion.snappy)
                        Text("Natural").tag(Motion.natural)
                        Text("Off").tag(Motion.off)
                    }
                    .pickerStyle(.segmented).labelsHidden().fixedSize()
                }
            }
        }
    }
}

struct TerminalPage: View {
    let ctx: SettingsContext

    var body: some View {
        let st = ctx.style
        VStack(alignment: .leading, spacing: 0) {
            SettingsSection(title: "Cursor", style: st) {
                FontPreview(family: ctx.s.fontFamily, size: ctx.s.fontSize, style: st, cursorOnly: true)
                SettingsRow(id: "cursor-style-blink", style: st, nav: ctx.nav) {
                    Toggle("", isOn: Binding(get: { ctx.s.cursorStyleBlink != false }, set: { ctx.set("cursor-style-blink", $0 ? "" : "false") }))
                        .toggleStyle(.switch).labelsHidden().controlSize(.small)
                }
            }
            SettingsSection(title: "Input and output", style: st) {
                SettingsRow(id: "bell", style: st, nav: ctx.nav, divider: false) {
                    Picker("", selection: Binding(get: { ctx.s.bell }, set: { ctx.set("bell", $0) })) {
                        Text("Flash the pane").tag("flash")
                        Text("Play a sound").tag("sound")
                        Text("Flash and play a sound").tag("flash+sound")
                        Text("Nothing").tag("none")
                    }
                    .labelsHidden().fixedSize()
                }
                SettingsRow(id: "confirm-paste", style: st, nav: ctx.nav) {
                    Toggle("", isOn: Binding(get: { ctx.s.confirmPaste }, set: { ctx.set("confirm-paste", $0 ? "true" : "false") }))
                        .toggleStyle(.switch).labelsHidden().controlSize(.small)
                }
                SettingsRow(id: "copy-on-select", style: st, nav: ctx.nav) {
                    Toggle("", isOn: Binding(get: { ctx.s.copyOnSelect }, set: { ctx.set("copy-on-select", $0 ? "true" : "false") }))
                        .toggleStyle(.switch).labelsHidden().controlSize(.small)
                }
            }
        }
    }
}

struct PanesPage: View {
    let ctx: SettingsContext

    var body: some View {
        let st = ctx.style
        VStack(alignment: .leading, spacing: 0) {
            SettingsSection(title: "Focus", style: st) {
                SettingsRow(id: "dim-panes", style: st, nav: ctx.nav, divider: false) {
                    Picker("", selection: Binding(get: { ctx.s.dimPanes }, set: { ctx.set("dim-panes", $0) })) {
                        Text("Dim a little").tag("light")
                        Text("Dim more").tag("more")
                        Text("Don’t dim").tag("off")
                    }
                    .labelsHidden().fixedSize()
                }
                SettingsRow(id: "focus-follows-pointer", style: st, nav: ctx.nav) {
                    Toggle("", isOn: Binding(get: { ctx.s.focusFollowsPointer }, set: { ctx.set("focus-follows-pointer", $0 ? "true" : "false") }))
                        .toggleStyle(.switch).labelsHidden().controlSize(.small)
                }
            }
            SettingsSection(title: "Splits", style: st) {
                SettingsRow(id: "balance-splits", style: st, nav: ctx.nav, divider: false) {
                    Toggle("", isOn: Binding(get: { ctx.s.balanceSplits }, set: { ctx.set("balance-splits", $0 ? "true" : "false") }))
                        .toggleStyle(.switch).labelsHidden().controlSize(.small)
                }
            }
            SettingsSection(title: "Closing", style: st) {
                SettingsRow(id: "confirm-close", style: st, nav: ctx.nav, divider: false) {
                    Picker("", selection: Binding(get: { ctx.s.confirmClose }, set: { ctx.set("confirm-close", $0.rawValue) })) {
                        Text("When something runs").tag(CloseGuard.Policy.running)
                        Text("Always").tag(CloseGuard.Policy.always)
                        Text("Never").tag(CloseGuard.Policy.never)
                    }
                    .labelsHidden().fixedSize()
                }
            }
        }
    }
}

struct NotificationsPage: View {
    let ctx: SettingsContext

    var body: some View {
        let st = ctx.style
        VStack(alignment: .leading, spacing: 0) {
            SettingsSection(title: "Agents", style: st) {
                SettingsRow(id: "notify", style: st, nav: ctx.nav, divider: false) {
                    Picker("", selection: Binding(get: { ctx.s.notify }, set: { ctx.set("notify", $0) })) {
                        Text("When hive is in the background").tag("background")
                        Text("Always").tag("always")
                        Text("Never").tag("never")
                    }
                    .labelsHidden().fixedSize()
                }
            }
            SettingsSection(title: "By state", note: "Each state notifies with its own sound. The tab is marked either way.", style: st) {
                StateRow(ctx: ctx, id: "notify-blocked", color: st.blocked, on: ctx.s.notifyBlocked, sound: ctx.s.soundBlocked,
                         soundKey: "sound-blocked", divider: false)
                StateRow(ctx: ctx, id: "notify-done", color: st.done, on: ctx.s.notifyDone, sound: ctx.s.soundDone, soundKey: "sound-done")
                StateRow(ctx: ctx, id: "notify-stopped", color: st.blocked, on: ctx.s.notifyStopped, sound: ctx.s.soundStopped,
                         soundKey: "sound-stopped")
            }
            SettingsSection(title: "Dock", style: st) {
                SettingsRow(id: "dock-badge", style: st, nav: ctx.nav, divider: false) {
                    Toggle("", isOn: Binding(get: { ctx.s.dockBadge }, set: { ctx.set("dock-badge", $0 ? "true" : "false") }))
                        .toggleStyle(.switch).labelsHidden().controlSize(.small)
                }
            }
        }
    }
}

/// One agent state: its hexagon, whether it notifies, its sound and a Play button.
struct StateRow: View {
    let ctx: SettingsContext
    let id: String
    let color: NSColor
    let on: Bool
    let sound: String
    let soundKey: String
    var divider = true

    var body: some View {
        let st = ctx.style
        let e = SettingsIndex.entry(id)
        VStack(spacing: 0) {
            if divider { Rectangle().fill(st.border.swiftUI.opacity(0.6)).frame(height: 1).padding(.leading, 12) }
            HStack(spacing: 10) {
                Image(systemName: "hexagon.fill").font(.system(size: 15)).foregroundStyle(color.swiftUI.opacity(on ? 1 : 0.35))
                Text(e.label).font(.system(size: 12.5)).foregroundStyle(st.text.swiftUI)
                Spacer(minLength: 8)
                Picker("", selection: Binding(get: { sound }, set: { ctx.set(soundKey, $0) })) {
                    Text("No sound").tag("none")
                    ForEach(HiveCore.Settings.sounds, id: \.self) { Text($0).tag($0) }
                }
                .labelsHidden().fixedSize().disabled(!on)
                .accessibilityLabel("\(e.label) sound")
                Button { NSSound(named: NSSound.Name(sound))?.play() } label: {
                    Image(systemName: "play.fill").font(.system(size: 9)).frame(width: 22, height: 22)
                        .background(Circle().fill(st.text.swiftUI.opacity(0.07)))
                }
                .buttonStyle(.plain).foregroundStyle(st.secondary.swiftUI)
                .disabled(!on || sound == "none")
                .help("Play")
                .accessibilityLabel("Play the \(e.label) sound")
                Toggle("", isOn: Binding(get: { on }, set: { ctx.set(id, $0 ? "true" : "false") }))
                    .toggleStyle(.switch).labelsHidden().controlSize(.small)
                    .accessibilityLabel("Notify: \(e.label)")
            }
            .padding(.horizontal, 12).frame(minHeight: 40)
        }
        .id(id)
        .help(e.help)
    }
}

struct AgentsPage: View {
    let ctx: SettingsContext
    @StateObject private var claude = ClaudeHooks()

    var body: some View {
        let st = ctx.style
        VStack(alignment: .leading, spacing: 0) {
            SettingsSection(title: "Coding agents", style: st) {
                SettingsRow(id: "agents-claude", style: st, nav: ctx.nav, divider: false) {
                    HStack(spacing: 10) {
                        Text(claude.state.word).font(.system(size: 11.5)).foregroundStyle(claude.state == .on ? st.doneText.swiftUI : st.secondary.swiftUI)
                        Button(claude.state == .on ? "Disconnect" : "Connect") { claude.toggle() }
                            .disabled(claude.state == .busy || claude.state == .unknown)
                    }
                }
                if let p = claude.problem {
                    Text(p).font(.system(size: 11.5)).foregroundStyle(st.blockedText.swiftUI).padding(.horizontal, 12).padding(.bottom, 8)
                }
            }
            SettingsSection(title: "Scripts", style: st) {
                SettingsRow(id: "scripts", style: st, nav: ctx.nav, divider: false) {
                    Toggle("", isOn: Binding(get: { ctx.s.scripts }, set: { ctx.setServer("scripts", $0 ? "true" : "false") }))
                        .toggleStyle(.switch).labelsHidden().controlSize(.small)
                }
                SettingsRow(id: "script-remote-run", style: st, nav: ctx.nav) {
                    Toggle("", isOn: Binding(get: { ctx.s.scriptRemoteRun }, set: { ctx.setServer("script-remote-run", $0 ? "true" : "false") }))
                        .toggleStyle(.switch).labelsHidden().controlSize(.small)
                }
            }
            HStack {
                Spacer()
                Button("Open Scripts…") { ctx.actions.scripts() }
            }
            .padding(.top, 10)
        }
        .onAppear { claude.check() }
    }
}

/// Whether hive's hooks are in Claude Code's settings, read with a dry run
/// (`hive agent install claude --dry-run` changes nothing when they are).
@MainActor
final class ClaudeHooks: ObservableObject {
    enum State {
        case unknown, on, off, busy
        var word: String {
            switch self {
            case .unknown, .busy: return ""
            case .on: return "Connected"
            case .off: return "Not connected"
            }
        }
    }
    @Published var state = State.unknown
    @Published var problem: String?

    func check() {
        DispatchQueue.global().async {
            let r = SettingsStore.run(["agent", "install", "claude", "--dry-run"])
            let out = r.map { String(decoding: $0.out, as: UTF8.self) } ?? ""
            let s: State = r?.status == 0 ? (out.hasPrefix("(dry run) unchanged") ? .on : .off) : .unknown
            let problem = r == nil ? "hive isn't installed with the app"
                : r?.status != 0 ? (r?.err ?? "").replacingOccurrences(of: "Error: ", with: "").trimmingCharacters(in: .whitespacesAndNewlines) : nil
            Task { @MainActor in
                self.state = s
                if s == .unknown { self.problem = problem?.isEmpty == false ? problem : "Couldn’t read Claude Code’s settings." }
            }
        }
    }

    func toggle() {
        let sub = state == .on ? "uninstall" : "install"
        state = .busy
        problem = nil
        DispatchQueue.global().async {
            let r = SettingsStore.run(["agent", sub, "claude"])
            Task { @MainActor in
                if r?.status != 0 { self.problem = r.map { $0.err.replacingOccurrences(of: "Error: ", with: "") } ?? "hive isn't installed with the app" }
                self.check()
            }
        }
    }
}

struct HostsPage: View {
    let ctx: SettingsContext

    var body: some View {
        let st = ctx.style
        VStack(alignment: .leading, spacing: 0) {
            SettingsSection(title: "Hosts", style: st) {
                ForEach(Array(ctx.model.hostRows.enumerated()), id: \.element.id) { i, h in
                    VStack(spacing: 0) {
                        if i > 0 { Rectangle().fill(st.border.swiftUI.opacity(0.6)).frame(height: 1).padding(.leading, 12) }
                        HStack(spacing: 10) {
                            Image(systemName: h.local ? "laptopcomputer" : "globe").foregroundStyle(st.secondary.swiftUI).frame(width: 18)
                            Text(h.local ? "This Mac" : h.name).font(.system(size: 12.5)).foregroundStyle(st.text.swiftUI)
                            Spacer()
                            Text(h.connected ? "Connected" : h.idle ? "Not connected" : (h.problem ?? "Away"))
                                .font(.system(size: 11.5)).foregroundStyle(h.connected ? st.doneText.swiftUI : st.secondary.swiftUI)
                                .lineLimit(1)
                            if !h.local {
                                if h.connected || !h.idle {
                                    Button("Disconnect") { ctx.model.disconnect(host: h.id) }
                                } else {
                                    Button("Connect") { ctx.model.connect(host: h.id) }
                                }
                            }
                        }
                        .padding(.horizontal, 12).frame(minHeight: 38)
                    }
                }
            }
            HStack(spacing: 8) {
                Spacer()
                Button("Share Hosts with Phone…") { ctx.actions.shareHosts() }.help(SettingsIndex.entry("share-hosts").help)
                Button("Add Host…") { ctx.actions.addHost() }.help(SettingsIndex.entry("add-host").help)
            }
            .padding(.top, 10)
        }
    }
}

struct ShortcutsPage: View {
    let ctx: SettingsContext

    var body: some View {
        let st = ctx.style
        VStack(alignment: .leading, spacing: 0) {
            SettingsSection(title: "Shortcuts", style: st) {
                SettingsRow(id: "shortcuts", style: st, nav: ctx.nav, divider: false) {
                    Button("Show Shortcuts") { ctx.actions.shortcuts() }
                }
            }
            SettingsSection(title: "Keyboard", style: st) {
                SettingsRow(id: "key-hints", style: st, nav: ctx.nav, divider: false) {
                    Toggle("", isOn: Binding(get: { ctx.s.keyHints }, set: { ctx.set("key-hints", $0 ? "true" : "false") }))
                        .toggleStyle(.switch).labelsHidden().controlSize(.small)
                }
            }
            Text("Change a shortcut with a keybind line in the settings file, such as keybind = cmd+shift+t=client.new_tab.")
                .font(.system(size: 11.5)).foregroundStyle(st.secondary.swiftUI).padding(.top, 6).padding(.leading, 2)
                .textSelection(.enabled)
        }
    }
}

struct AdvancedPage: View {
    let ctx: SettingsContext

    var body: some View {
        let st = ctx.style
        VStack(alignment: .leading, spacing: 0) {
            SettingsSection(title: "Settings file", style: st) {
                VStack(alignment: .leading, spacing: 8) {
                    HStack {
                        Text(ctx.s.path.replacingOccurrences(of: NSHomeDirectory(), with: "~"))
                            .font(.system(size: 11.5, design: .monospaced)).foregroundStyle(st.secondary.swiftUI)
                            .lineLimit(1).truncationMode(.middle).textSelection(.enabled)
                        Spacer()
                        Button("Open Settings File") { ctx.store.openInEditor() }
                    }
                    Text(SettingsIndex.entry("config-file").help).font(.system(size: 11.5)).foregroundStyle(st.secondary.swiftUI)
                }
                .padding(12)
            }
            if let problem = ctx.store.writeError.map({ "Settings: " + $0 }) ?? ctx.s.banner {
                Label(problem, systemImage: "exclamationmark.triangle.fill")
                    .font(.system(size: 12)).foregroundStyle(st.blockedText.swiftUI).padding(.top, 10)
            }
        }
    }
}

/// Search results: each row with where it lives and what it does.
struct SettingsSearchResults: View {
    @ObservedObject var nav: SettingsNav
    let style: Style

    var body: some View {
        let found = SettingsIndex.search(nav.query)
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text("All settings").font(.system(size: 12.5, weight: .semibold)).foregroundStyle(style.text.swiftUI)
                Spacer()
                Text(found.count == 1 ? "1 result" : "\(found.count) results").font(.system(size: 11.5)).foregroundStyle(style.secondary.swiftUI)
            }
            .padding(.top, 4).padding(.bottom, 6)
            if found.isEmpty {
                Text("No setting matches “\(nav.query)”.").font(.system(size: 12.5)).foregroundStyle(style.secondary.swiftUI).padding(.top, 8)
            } else {
                VStack(spacing: 0) {
                    ForEach(Array(found.enumerated()), id: \.element.id) { i, e in
                        ResultRow(entry: e, style: style, divider: i > 0) { nav.go(e.page, spotlight: e.id) }
                    }
                }
                .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(style.card.swiftUI.opacity(style.dark ? 0.7 : 1)))
                .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(style.border.swiftUI.opacity(0.8), lineWidth: 1))
            }
        }
    }

    struct ResultRow: View {
        let entry: SettingsIndex.Entry
        let style: Style
        let divider: Bool
        let action: () -> Void
        @State private var hover = false

        var body: some View {
            Button(action: action) {
                VStack(spacing: 0) {
                    if divider { Rectangle().fill(style.border.swiftUI.opacity(0.6)).frame(height: 1).padding(.leading, 12) }
                    HStack(spacing: 10) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("\(entry.page.title), \(entry.section)").font(.system(size: 11)).foregroundStyle(style.secondary.swiftUI)
                            Text(entry.label).font(.system(size: 12.5, weight: .medium)).foregroundStyle(style.text.swiftUI)
                            Text(entry.help).font(.system(size: 11.5)).foregroundStyle(style.secondary.swiftUI)
                                .fixedSize(horizontal: false, vertical: true).multilineTextAlignment(.leading)
                        }
                        Spacer()
                        Image(systemName: "chevron.right").font(.system(size: 11, weight: .semibold)).foregroundStyle(style.secondary.swiftUI)
                    }
                    .padding(.horizontal, 12).padding(.vertical, 9)
                    .background(style.text.swiftUI.opacity(hover ? 0.04 : 0))
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .onHover { hover = $0 }
        }
    }
}

enum SettingsFormat {
    static func number(_ v: Double) -> String { v == v.rounded() ? String(Int(v)) : String(format: "%.1f", v) }

    /// As the settings file writes a size: 16m.
    static func size(_ b: UInt64) -> String {
        b % (1 << 30) == 0 ? "\(b >> 30)g" : b % (1 << 20) == 0 ? "\(b >> 20)m" : b % (1 << 10) == 0 ? "\(b >> 10)k" : "\(b)"
    }

    /// As people read a size: 16 MB.
    static func megabytes(_ b: UInt64) -> String {
        b % (1 << 30) == 0 ? "\(b >> 30) GB" : b % (1 << 20) == 0 ? "\(b >> 20) MB" : b % (1 << 10) == 0 ? "\(b >> 10) KB" : "\(b) bytes"
    }

    static func scrollbackChoices(current: UInt64) -> [UInt64] {
        var c: [UInt64] = [4 << 20, 16 << 20, 64 << 20, 256 << 20]
        if !c.contains(current) { c.append(current); c.sort() }
        return c
    }
}

/// The installed families font-family accepts (FontSet's own test).
@MainActor
enum MonospacedFonts {
    static let families: [String] = FontSet.monospacedFamilies()
}
