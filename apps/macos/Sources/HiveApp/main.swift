import AppKit
import HiveCore
import Combine
import HiveRender
import SwiftUI

/// The window's background: a soft vertical gradient from the style.
final class RootView: NSView {
    let gradient = CAGradientLayer()
    var sidebarWidth: () -> CGFloat = { 0 }
    override init(frame: NSRect) {
        super.init(frame: frame)
        wantsLayer = true
        layer?.addSublayer(gradient)
    }
    required init?(coder: NSCoder) { fatalError() }
    override var isFlipped: Bool { true }
    func apply(_ s: Style) {
        gradient.colors = [s.windowTop.cgColor, s.windowBottom.cgColor]
    }
    override func layout() {
        super.layout()
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        gradient.frame = bounds
        CATransaction.commit()
        let side = sidebarWidth()
        for v in subviews {
            if v is TitleBarHost<TitleBar> {
                v.frame = NSRect(x: side, y: 0, width: bounds.width - side, height: TitleBar.height)
            } else if v is TitleBarHost<AgentSidebar> {
                v.isHidden = side == 0
                v.frame = NSRect(x: 0, y: 0, width: AgentSidebar.width, height: bounds.height)
            } else if v is PanesView || v is HostAwayHost {
                // Cards start just under the bar, clear of the tabs.
                let top = TitleBar.height + 4
                v.frame = NSRect(x: side, y: top, width: bounds.width - side, height: bounds.height - top)
            }
        }
    }
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate {
    let model = AppModel()
    var window: NSWindow!
    var root: RootView!
    var panes: PanesView!
    var renderer: Renderer!
    let palette = PalettePanel()
    let shortcuts = ShortcutsPanel()
    let settingsWindow = SettingsWindow()
    var bag: Set<AnyCancellable> = []
    var appearanceObserver: NSKeyValueObservation?
    var appearanceItems: [(Appearance, NSMenuItem)] = []

    func applicationDidFinishLaunching(_ note: Notification) {
        let scale = NSScreen.main?.backingScaleFactor ?? 2
        let s = model.settings
        let fonts = FontSet(family: s.fontFamily, pointSize: s.fontSize, scale: scale)
        model.settingsStore.fontProblem = fonts.problem
        renderer = try! Renderer(fonts: fonts)

        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1280, height: 800),
                          styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
                          backing: .buffered, defer: false)
        window.titlebarAppearsTransparent = true
        window.titleVisibility = .hidden
        // An empty toolbar makes the title bar taller and centers the
        // traffic lights in it, so they line up with our bar.
        let toolbar = NSToolbar(identifier: "hive")
        toolbar.showsBaselineSeparator = false
        window.toolbar = toolbar
        window.toolbarStyle = .unifiedCompact
        window.title = "Hive"
        window.minSize = NSSize(width: 720, height: 420)
        window.delegate = self

        root = RootView(frame: window.contentLayoutRect)
        panes = PanesView(model: model, renderer: renderer)
        let bar = TitleBarHost(rootView: TitleBar(model: model, openPicker: { [weak self] in self?.showSessionPicker() },
                                                  openPalette: { [weak self] in self?.showPalette() }))
        // It sits in the title bar on purpose: no safe-area inset.
        bar.safeAreaRegions = []
        let sidebar = TitleBarHost(rootView: AgentSidebar(model: model))
        sidebar.safeAreaRegions = []
        root.sidebarWidth = { [weak self] in (self?.model.sidebar ?? false) ? AgentSidebar.width : 0 }
        root.addSubview(panes)
        let away = HostAwayHost(rootView: HostAway(model: model))
        away.safeAreaRegions = []
        away.isHidden = true
        root.addSubview(away)
        // Shown while the host is away (connecting at launch isn't that).
        model.$trouble.removeDuplicates().sink { [weak away] t in away?.isHidden = !t }.store(in: &bag)
        root.addSubview(bar)
        root.addSubview(sidebar)
        window.contentView = root

        // Screenshot runs use their own defaults suite: they mustn't take or
        // change the size of the user's own window.
        if AppModel.defaults == .standard { window.setFrameAutosaveName("HiveMain") }
        if AppModel.defaults != .standard || window.frame.width < 900 || window.frame.height < 560 {
            window.setContentSize(NSSize(width: 1280, height: 800))
            window.center()
        }

        model.onFrame = { [weak self] block in self?.panes.redraw(block) }
        model.openSettings = { [weak self] in self?.showSettings() }
        model.onFlash = { [weak self] block in self?.panes.card(block)?.flash() }
        // font-family and font-size apply live; a burst of changes (the size
        // stepper) swaps the font and re-sizes the panes once.
        model.settingsStore.$settings
            .map { "\($0.fontFamily)\u{1}\($0.fontSize)" }
            .removeDuplicates()
            .dropFirst()
            .debounce(for: .milliseconds(250), scheduler: DispatchQueue.main)
            .sink { [weak self] _ in self?.applyFont() }
            .store(in: &bag)
        model.settingsStore.$settings
            .map(\.cursorStyleBlink)
            .removeDuplicates()
            .sink { [weak self] b in
                self?.model.setCursorBlink(b)
            }
            .store(in: &bag)
        model.contextActions = { [weak self] in self?.paneContextActions() ?? [] }
        model.onLayout = { [weak self] in
            self?.panes.sync()
            self?.panes.claimSizes()
        }
        model.$style.sink { [weak self] s in self?.applyStyle(s) }.store(in: &bag)
        model.$sidebar.dropFirst().sink { [weak self] _ in
            DispatchQueue.main.async {
                self?.root.needsLayout = true
                self?.root.layoutSubtreeIfNeeded()
                self?.panes.sync()
                self?.panes.claimSizes()
            }
        }.store(in: &bag)
        buildMenu()
        followSystemAppearance()
        window.makeKeyAndOrderFront(nil)
        if ProcessInfo.processInfo.environment["HIVE_DEBUG"] != nil, let b = window.standardWindowButton(.closeButton) {
            let f = b.convert(b.bounds, to: nil)
            NSLog("hive traffic light center y (from top): %.1f  titlebar height: %.1f", window.frame.height - f.midY,
                  window.frame.height - window.contentLayoutRect.height)
        }
        model.start()
        NSApp.activate(ignoringOtherApps: true)
        #if DEBUG
        // For the screenshot loop: open an overlay once connected. Debug
        // builds only: one of these logs selected terminal text.
        if let what = ProcessInfo.processInfo.environment["HIVE_OPEN"] {
            DispatchQueue.main.asyncAfter(deadline: .now() + 2.5) { [weak self] in
                if what == "palette" { self?.showPalette() } else if what == "picker" { self?.showSessionPicker() }
                if what.hasPrefix("palette:") { self?.showPalette(query: String(what.dropFirst("palette:".count))) }
                if what == "hostlist:connect" { self?.showHostList(.connect) }
                if what == "hostlist:remove" { self?.showHostList(.remove) }
                if what == "shortcuts" { self?.toggleShortcuts() }
                if what == "settings" { self?.showSettings() }
                if what.hasPrefix("addhostproblem:") {
                    self?.showAddHost(address: String(what.dropFirst("addhostproblem:".count)), problem: "hosts add: no hive server at box.example.ts.net (tried https://box.example.ts.net, https://box.example.ts.net:4483, http://box.example.ts.net:4483)")
                } else if what.hasPrefix("addhost") { self?.showAddHost(address: String(what.dropFirst("addhost".count).drop { $0 == ":" })) }
                if what == "panemenu", let self, let b = self.model.focusedBlock, let card = self.panes.card(b) {
                    card.pane.showContextMenuForTesting()
                }
                if what == "preedit", let self, let b = self.model.focusedBlock, let card = self.panes.card(b) {
                    // A dead key waiting for the next key, as ~ on a Portuguese layout.
                    card.pane.setMarkedText("~", selectedRange: NSRange(location: 1, length: 0), replacementRange: NSRange(location: NSNotFound, length: 0))
                }
                if what == "select", let self, let b = self.model.focusedBlock, let card = self.panes.card(b) {
                    // The mouse path, driven by hand: press, drag along row 1.
                    let p = card.pane, cw = Double(p.renderer.fonts.cellWidth), ch = Double(p.renderer.fonts.cellHeight)
                    self.model.select(block: b, .press, col: 4, row: 1, x: 4 * cw + 2, y: ch + 4, cellWidth: cw, cellHeight: ch,
                                      repeatInterval: 0.5, repeatDistance: 8)
                    self.model.select(block: b, .drag, col: 30, row: 1, x: 30 * cw + 2, y: ch + 4, cellWidth: cw, cellHeight: ch,
                                      repeatInterval: 0.5, repeatDistance: 8)
                    let engine = self.model.engine
                    self.model.vt.asyncAfter(deadline: .now() + 0.3) { NSLog("hive selected: %@", engine.selectedText(block: b) ?? "<none>") }
                }
            }
        }
        #endif
    }

    // MARK: appearance

    private func followSystemAppearance() {
        updateAppearance()
        appearanceObserver = NSApp.observe(\.effectiveAppearance) { [weak self] _, _ in Task { @MainActor in self?.updateAppearance() } }
        model.$appearance.dropFirst().sink { [weak self] _ in DispatchQueue.main.async { self?.updateAppearance() } }.store(in: &bag)
    }

    /// Applies the theme setting. Day and Night set the app's appearance, so
    /// alerts, menus and panels follow too; System leaves it to macOS.
    private func updateAppearance() {
        let env = ProcessInfo.processInfo.environment["HIVE_STYLE"]
        let forced = env == "dark" || env == "light" || model.appearance != .system
        if !forced, NSApp.appearance != nil { NSApp.appearance = nil }
        let systemDark = NSApp.effectiveAppearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
        let night = model.appearance.isNight(env: env, systemDark: systemDark)
        if forced {
            let want = NSAppearance(named: night ? .darkAqua : .aqua)
            if NSApp.appearance?.name != want?.name { NSApp.appearance = want }
        }
        let s: Style = night ? .dark : .light
        if model.style != s { model.style = s }
        for (a, item) in appearanceItems { item.state = a == model.appearance ? .on : .off }
    }

    private func applyStyle(_ s: Style) {
        window.appearance = NSAppearance(named: s.dark ? .darkAqua : .aqua)
        window.backgroundColor = s.windowBottom
        root.apply(s)
        renderer.theme = s.terminal
        model.setTerminalTheme(s.terminal)
        // s, not model.style: @Published sends before the property changes,
        // so model.style still holds the old theme here.
        panes.restyle(s)
    }

    func windowDidBecomeKey(_ notification: Notification) {
        model.markSeen()
        panes.claimSizes()
        panes.sync()
    }

    func windowDidResignKey(_ notification: Notification) { panes.sync() }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }

    func applicationDidBecomeActive(_ notification: Notification) { model.markSeen() }

    // MARK: palette and picker

    func showPalette(query: String = "") {
        palette.show(over: window, width: 460, placeholder: "Search commands…", style: model.style, query: query) { [weak self] q in
            (self?.commands() ?? []).filter { fuzzy($0.title, q) }
        }
    }

    func showSessionPicker() {
        palette.show(over: window, at: CGPoint(x: TitleBar.leading - 4, y: TitleBar.height), width: 300,
                     placeholder: "Filter or create…", style: model.style, items: { [weak self] q in
            guard let self else { return [] }
            let m = self.model
            var rows: [PaletteItem] = []
            // A section per host: this Mac's sessions, then each remote
            // host's, or why it has none to show.
            for h in m.hostRows {
                for s in m.sessions where s.host == h.id && fuzzy(s.label, q) {
                    let need = m.attention(session: s.id)
                    let detail = !need.isEmpty
                        ? attentionCounts(need)
                        : s.agents > 0 ? "\(s.agents) agent\(s.agents == 1 ? "" : "s")" : ""  // no keys switch sessions by number
                    // No icon: the session you're in has its check there.
                    rows.append(PaletteItem(key: s.id, title: s.label.isEmpty ? String(s.id.suffix(6)) : s.label, detail: detail,
                                            checked: s.id == m.currentSession, section: h.name,
                                            sectionSymbol: h.local ? "laptopcomputer" : "globe") { [weak self] in
                        self?.model.select(session: s.id)
                    })
                }
                guard q.isEmpty else { continue }
                let icon = h.local ? "laptopcomputer" : "globe"
                if h.idle {
                    // Saved, not connected: connect, then show its sessions.
                    rows.append(PaletteItem(key: "connect:\(h.id)", title: "Connect", symbol: "link", section: h.name,
                                            sectionSymbol: icon) { [weak self] in
                        self?.model.connect(host: h.id) { [weak self] in self?.showSessionPicker() }
                    })
                } else if let problem = h.problem {
                    // What's wrong, in a word: it says, it doesn't act.
                    rows.append(PaletteItem(key: "host:\(h.id)", title: problem, symbol: "exclamationmark.triangle.fill", section: h.name,
                                            sectionSymbol: icon, help: h.detail ?? "", inert: true, warning: h.refused) {})
                } else if !h.local {
                    rows.append(PaletteItem(key: "new:\(h.id)", title: "New Session on \(h.name)", symbol: "plus", section: h.name,
                                            sectionSymbol: icon) { [weak self] in
                        self?.model.newSession(onHost: h.id)
                    })
                }
            }
            if q.isEmpty {
                rows.append(PaletteItem(title: "New Session", detail: ShortcutsPanel.keys(for: "New Session"), symbol: "plus", section: "",
                                        dividerAbove: true) { [weak self] in self?.model.newSession() })
                rows.append(PaletteItem(title: "Add Remote Host…", symbol: "globe", section: "") { [weak self] in
                    DispatchQueue.main.async { self?.showAddHost() }
                })
            }
            return rows
        }, onCreate: { [weak self] name in self?.model.newSession(label: name) })
    }

    func toggleShortcuts() { shortcuts.toggle(over: window, style: model.style) }

    func showSettings() { settingsWindow.show(model: model) }

    func applyFont() {
        let s = model.settings
        let fonts = FontSet(family: s.fontFamily, pointSize: s.fontSize, scale: window.backingScaleFactor)
        model.settingsStore.fontProblem = fonts.problem
        renderer.setFonts(fonts)
        panes.fontChanged()
        #if DEBUG
        NSLog("hive font: %@ %.1f -> cell %dx%d", s.fontFamily.isEmpty ? "bundled" : s.fontFamily, s.fontSize,
              renderer.fonts.cellWidth, renderer.fonts.cellHeight)
        #endif
    }

    /// The pane's context menu, after Copy and Paste: the menu bar's own
    /// items, so it shows the same keys.
    func paneContextActions() -> [NSMenuItem] {
        let m = model
        var items: [NSMenuItem] = []
        func add(_ title: String, _ action: @escaping () -> Void) {
            let i = ClosureMenuItem(title: title, key: "", action: action)
            // The key the menu bar shows for it, as the keyboard layout types it.
            if let bar = NSApp.mainMenu?.items.compactMap({ $0.submenu?.items.first { $0.title == title } }).first {
                i.keyEquivalent = bar.keyEquivalent
                i.keyEquivalentModifierMask = bar.keyEquivalentModifierMask
            }
            items.append(i)
        }
        add("Split Right") { [weak self] in self?.split(true) }
        add("Split Down") { [weak self] in self?.split(false) }
        add("Zoom Pane") { m.toggleZoom() }
        items.append(.separator())
        add("Close Pane") { m.closePane() }
        items.append(.separator())
        add("Settings…") { [weak self] in self?.showSettings() }
        return items
    }

    func commands() -> [PaletteItem] {
        let m = model
        let k = ShortcutsPanel.keys(for:)
        return [
            PaletteItem(title: "Keyboard Shortcuts", detail: k("Keyboard Shortcuts"), symbol: "keyboard") { [weak self] in
                DispatchQueue.main.async { self?.toggleShortcuts() }
            },
            PaletteItem(title: "Settings…", detail: k("Settings…"), symbol: "gearshape") { [weak self] in
                DispatchQueue.main.async { self?.showSettings() }
            },
            PaletteItem(title: "Open Config File", symbol: "doc.text") { m.settingsStore.openInEditor() },
            PaletteItem(title: "New Tab", detail: k("New Tab"), symbol: "plus.square") { m.newTab() },
            PaletteItem(title: m.sidebar ? "Hide Agents Sidebar" : "Show Agents Sidebar", detail: k("Toggle Agents Sidebar"), symbol: "sidebar.left") { m.sidebar.toggle() },
            PaletteItem(title: "New Session", detail: k("New Session"), symbol: "plus.rectangle.on.rectangle") { m.newSession() },
            PaletteItem(title: "Change Session…", detail: k("Change Session…"), symbol: "rectangle.stack") { [weak self] in
                DispatchQueue.main.async { self?.showSessionPicker() }
            },
            PaletteItem(title: "Split Right", detail: k("Split Right"), symbol: "rectangle.split.2x1") { [weak self] in self?.split(true) },
            PaletteItem(title: "Split Down", detail: k("Split Down"), symbol: "rectangle.split.1x2") { [weak self] in self?.split(false) },
            PaletteItem(title: "Zoom Pane", detail: k("Zoom Pane"), symbol: "arrow.up.left.and.arrow.down.right") { m.toggleZoom() },
            PaletteItem(title: "Close Pane", detail: k("Close Pane"), symbol: "xmark") { m.closePane() },
            PaletteItem(title: "Close Tab", detail: k("Close Tab"), symbol: "xmark.square") { m.closeTab() },
            PaletteItem(title: "Next Tab", detail: k("Next Tab"), symbol: "arrow.right") { m.selectWindow(offset: 1) },
            PaletteItem(title: "Previous Tab", detail: k("Previous Tab"), symbol: "arrow.left") { m.selectWindow(offset: -1) },
            PaletteItem(title: "Rename Session…", detail: m.sessionLabel, symbol: "pencil") { [weak self] in self?.prompt("Rename session", m.sessionLabel) { m.renameSession($0) } },
            PaletteItem(title: "Rename Tab…", detail: m.active?.label ?? "", symbol: "pencil") { [weak self] in self?.prompt("Rename tab", m.active?.label ?? "") { m.renameTab($0) } },
            PaletteItem(title: "Add Remote Host…", symbol: "globe") { [weak self] in DispatchQueue.main.async { self?.showAddHost() } },
        ] + hostCommands() + Appearance.allCases.map { a in
            PaletteItem(title: "Theme: \(a.title)", symbol: a == .night ? "moon" : a == .day ? "sun.max" : "circle.lefthalf.filled",
                        checked: m.appearance == a) { m.setAppearance(a) }
        }
    }

    /// The palette's host commands, one row per action (the reference app's > rows), each
    /// opening the hosts it applies to. Only the ones that apply now.
    private func hostCommands() -> [PaletteItem] {
        let remote = model.hostRows.filter { !$0.local }
        var rows: [PaletteItem] = []
        if remote.contains(where: \.idle) {
            rows.append(PaletteItem(title: "Connect to Remote Host", symbol: "link", chevron: true) { [weak self] in
                DispatchQueue.main.async { self?.showHostList(.connect) }
            })
        }
        if remote.contains(where: { !$0.idle }) {
            rows.append(PaletteItem(title: "Disconnect from Remote Host", symbol: "xmark.circle", chevron: true) { [weak self] in
                DispatchQueue.main.async { self?.showHostList(.disconnect) }
            })
        }
        if !remote.isEmpty {
            rows.append(PaletteItem(title: "Remove Remote Host", symbol: "minus.circle", chevron: true) { [weak self] in
                DispatchQueue.main.async { self?.showHostList(.remove) }
            })
        }
        return rows
    }

    enum HostAction { case connect, disconnect, remove }

    /// The hosts an action applies to, by their globe and address.
    func showHostList(_ action: HostAction) {
        let token = ["Connect", "Disconnect", "Remove"][[HostAction.connect, .disconnect, .remove].firstIndex(of: action)!]
        palette.show(over: window, width: 460, placeholder: "Filter hosts…", style: model.style, token: token) { [weak self] q in
            guard let self else { return [] }
            let m = self.model
            return m.hostRows.filter { h in
                !h.local && fuzzy(h.name + " " + h.label, q) && (action == .remove || (action == .connect) == h.idle)
            }.map { h in
                PaletteItem(key: "\(token):\(h.id)", title: h.name, detail: h.name == h.label ? "" : h.label, symbol: "globe") { [weak self] in
                    switch action {
                    case .connect: m.connect(host: h.id) { [weak self] in self?.showSessionPicker() }
                    case .disconnect: m.disconnect(host: h.id)
                    case .remove: m.removeHost(h.label)
                    }
                }
            }
        }
    }

    /// Add Host, as an inline prompt: an address, and one row to connect
    /// to it. hive checks the address and saves the URL it answers at; a
    /// problem shows under the field, with the address kept.
    func showAddHost(address: String = "", problem: String? = nil) {
        palette.show(over: window, width: 460, placeholder: "box.example.ts.net, or https://…", style: model.style,
                     token: "Add Host", query: address, notice: problem.map(AppDelegate.hostProblem)) { [weak self] q in
            let a = q.trimmingCharacters(in: .whitespaces)
            guard !a.isEmpty else { return [] }
            return [PaletteItem(key: "connect", title: "Connect to \u{201C}\(a)\u{201D}", detail: "↩", symbol: "globe") { [weak self] in
                self?.addHost(a)
            }]
        }
    }

    /// What `hive hosts add` said, as a person would: the gist, then the
    /// particulars.
    /// "https://h, https://h:4483, http://h:4483" as "Tried HTTPS on 443 and
    /// 4483, and HTTP on 4483": the host is in the title already.
    static func triedSummary(_ list: String) -> String {
        var ports: [(scheme: String, ports: [String])] = []
        for u in list.split(separator: ",").map({ $0.trimmingCharacters(in: .whitespaces) }) {
            guard let c = URLComponents(string: u), let scheme = c.scheme?.uppercased() else { continue }
            let port = c.port.map(String.init) ?? (scheme == "HTTPS" ? "443" : "80")
            if let i = ports.firstIndex(where: { $0.scheme == scheme }) { ports[i].ports.append(port) } else { ports.append((scheme, [port])) }
        }
        let parts = ports.map { "\($0.scheme) on \($0.ports.joined(separator: " and "))" }
        guard !parts.isEmpty else { return "" }
        return "Tried " + (parts.count == 1 ? parts[0] : parts.dropLast().joined(separator: ", ") + ", and " + parts.last!) + "."
    }

    static func hostProblem(_ raw: String) -> (title: String, detail: String) {
        var m = raw
        for prefix in ["hosts add: ", "server unreachable: "] where m.hasPrefix(prefix) { m = String(m.dropFirst(prefix.count)) }
        if let r = m.range(of: "no hive server at "), let open = m.range(of: " (tried ", range: r.upperBound..<m.endIndex) {
            let host = String(m[r.upperBound..<open.lowerBound])
            let tried = String(m[open.upperBound...]).trimmingCharacters(in: CharacterSet(charactersIn: ")"))
            return ("No hive server answers at \(host)", triedSummary(tried))
        }
        return (m.prefix(1).uppercased() + m.dropFirst(), "")
    }

    private func addHost(_ address: String) {
        let label = model.newHostLabel(for: address)
        model.addHost(label: label, address: address) { [weak self] err in
            guard let self else { return }
            if let err {
                self.showAddHost(address: address, problem: err)
            } else {
                // Just added: connect it now, and show its sessions.
                self.model.connect(host: HostConnection.remoteID(label)) { [weak self] in self?.showSessionPicker() }
            }
        }
    }

    private func prompt(_ title: String, _ value: String, done: @escaping (String) -> Void) {
        DispatchQueue.main.async {
            let alert = NSAlert()
            alert.messageText = title
            let field = NSTextField(string: value)
            field.frame = NSRect(x: 0, y: 0, width: 260, height: 24)
            alert.accessoryView = field
            alert.addButton(withTitle: "Rename")
            alert.addButton(withTitle: "Cancel")
            alert.window.initialFirstResponder = field
            if alert.runModal() == .alertFirstButtonReturn, !field.stringValue.isEmpty { done(field.stringValue) }
        }
    }

    private func split(_ horizontal: Bool) {
        let s = panes.splitSize(horizontal: horizontal)
        model.split(horizontal: horizontal, cols: s?.0, rows: s?.1)
    }

    // MARK: menu

    private func buildMenu() {
        let main = NSMenu()
        func menu(_ title: String, _ items: [NSMenuItem]) {
            let item = NSMenuItem(title: title, action: nil, keyEquivalent: "")
            let m = NSMenu(title: title)
            items.forEach(m.addItem)
            item.submenu = m
            main.addItem(item)
        }
        func item(_ title: String, _ key: String, _ mods: NSEvent.ModifierFlags = .command, _ action: @escaping () -> Void) -> NSMenuItem {
            let i = ClosureMenuItem(title: title, key: key, action: action)
            i.keyEquivalentModifierMask = mods
            return i
        }
        let arrows: [(String, String)] = [("Left", "\u{F702}"), ("Right", "\u{F703}"), ("Up", "\u{F700}"), ("Down", "\u{F701}")]
        let m = model

        menu("Hive", [
            NSMenuItem(title: "About Hive", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: ""),
            .separator(),
            item("Settings…", ",") { [weak self] in self?.showSettings() },
            item("Add Remote Host…", "") { [weak self] in self?.showAddHost() },
            .separator(),
            item("Command Palette…", "p", [.command, .shift]) { [weak self] in self?.showPalette() },
            .separator(),
            NSMenuItem(title: "Hide Hive", action: #selector(NSApplication.hide(_:)), keyEquivalent: "h"),
            NSMenuItem(title: "Quit Hive", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q"),
        ])
        menu("Shell", [
            item("New Session", "n", [.command, .shift]) { m.newSession() },
            item("Change Session…", "k") { [weak self] in self?.showSessionPicker() },
            item("New Tab", "t") { m.newTab() },
            item("Split Right", "d") { [weak self] in self?.split(true) },
            item("Split Down", "d", [.command, .shift]) { [weak self] in self?.split(false) },
            .separator(),
            item("Close Pane", "w") { m.closePane() },
            item("Close Tab", "w", [.command, .shift]) { m.closeTab() },
        ])
        menu("Edit", [
            NSMenuItem(title: "Copy", action: #selector(PaneView.copy(_:)), keyEquivalent: "c"),
            NSMenuItem(title: "Paste", action: #selector(PaneView.paste(_:)), keyEquivalent: "v"),
        ])
        appearanceItems = Appearance.allCases.map { a in (a, item(a.title, "") { m.setAppearance(a) }) }
        menu("View", [item("Toggle Agents Sidebar", "s", [.command, .shift]) { m.sidebar.toggle() }, .separator()]
             + appearanceItems.map(\.1))
        var paneItems: [NSMenuItem] = [item("Zoom Pane", "\r", [.command, .shift]) { m.toggleZoom() }, .separator()]
        for (name, key) in arrows {
            paneItems.append(item("Focus \(name)", key, [.command, .option]) { m.focus(name.lowercased()) })
        }
        paneItems.append(.separator())
        for (name, key) in arrows {
            paneItems.append(item("Resize \(name)", key, [.command, .control]) { m.resizePane(name.lowercased()) })
        }
        menu("Pane", paneItems)
        var windowItems: [NSMenuItem] = [
            item("Next Tab", "]", [.command, .shift]) { m.selectWindow(offset: 1) },
            item("Previous Tab", "[", [.command, .shift]) { m.selectWindow(offset: -1) },
            .separator(),
        ]
        for i in 1...9 {
            windowItems.append(item("Tab \(i)", "\(i)") { m.selectWindow(index: i - 1) })
        }
        menu("Window", windowItems)
        menu("Help", [item("Keyboard Shortcuts", "/") { [weak self] in self?.toggleShortcuts() }])
        NSApp.mainMenu = main
    }
}

/// A menu item that runs a closure.
final class ClosureMenuItem: NSMenuItem {
    private let run: () -> Void
    init(title: String, key: String, action: @escaping () -> Void) {
        run = action
        super.init(title: title, action: #selector(fire), keyEquivalent: key)
        target = self
    }
    required init(coder: NSCoder) { fatalError() }
    @objc private func fire() { run() }
}

MainActor.assumeIsolated {
    let app = NSApplication.shared
    let delegate = AppDelegate()
    app.delegate = delegate
    app.setActivationPolicy(.regular)
    app.run()
}
