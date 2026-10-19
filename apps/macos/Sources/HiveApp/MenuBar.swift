import AppKit
import Combine
import HiveCore
import SwiftUI

/// hive in the menu bar: a quick status and the agents, those that need you
/// first. It stays while the window is closed; picking an agent brings the
/// window back on its pane.
@MainActor
final class MenuBar: NSObject, NSMenuDelegate {
    private let model: AppModel
    private let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    private let menu = NSMenu()
    private var bag: Set<AnyCancellable> = []
    /// Shows the window (and brings the app forward).
    var showWindow: () -> Void = {}
    var showSettings: () -> Void = {}
    var showShareHosts: () -> Void = {}

    init(model: AppModel) {
        self.model = model
        super.init()
        menu.delegate = self
        menu.autoenablesItems = false
        item.menu = menu
        item.button?.imagePosition = .imageLeading
        // seenDone too: looking at a finished agent quiets it without the
        // agent list changing. Computed from the values sent, not the model:
        // @Published sends before it sets.
        Publishers.CombineLatest3(model.$agents, model.$hostRows, model.$seenDone)
            .debounce(for: .milliseconds(80), scheduler: DispatchQueue.main)
            .sink { [weak self] agents, hosts, seen in self?.updateButton(self?.status(agents, hosts, seen)) }
            .store(in: &bag)
    }

    // MARK: status

    private func status(_ agents: [AgentInfo], _ hosts: [AppModel.HostRow], _ seen: [String: Date]) -> MenuBarStatus {
        let hs = hosts.map {
            MenuBarStatus.Host(id: $0.id, name: $0.name, connected: $0.connected, local: $0.local, idle: $0.idle, problem: $0.problem)
        }
        let live = agents.filter { $0.state != "ended" && model.isLive($0) }
        let xs = live.map { a in
            MenuBarStatus.Agent(id: a.blockID, state: a.state, doneSeen: a.updatedAt.map { d in (seen[a.blockID] ?? .distantPast) >= d } ?? false,
                                host: a.host)
        }
        return MenuBarStatus(hosts: hs, agents: xs)
    }

    private var current: MenuBarStatus { status(model.agents, model.hostRows, model.seenDone) }

    private func updateButton(_ s: MenuBarStatus?) {
        guard let s, let b = item.button else { return }
        b.appearsDisabled = s.icon == .away
        // Always a template: the menu bar colours it for its own background
        // (hive's Day or Night theme doesn't reach the menu bar). What needs
        // you is told by the count.
        switch s.icon {
        case .quiet, .away:
            b.image = MenuBar.comb(filled: false)
            b.title = ""
        case .working:
            b.image = MenuBar.comb(filled: true)
            b.title = ""
        case .needsYou(let n):
            b.image = MenuBar.comb(filled: true)
            b.title = n > 9 ? "9+" : "\(n)"
        }
        let what: String
        switch s.icon {
        case .needsYou(let n): what = n == 1 ? "1 agent needs you" : "\(n) agents need you"
        case .working: what = "agents working"
        case .away: what = s.subtitle
        case .quiet: what = s.subtitle
        }
        b.setAccessibilityLabel("hive, \(what)")
        b.toolTip = "hive: \(what)"
    }

    /// The brand's three cells, as a template; filled: the top cell solid.
    static func comb(filled: Bool) -> NSImage {
        let size = NSSize(width: 18, height: 16)
        let img = NSImage(size: size, flipped: true) { _ in
            let layout = Comb.layout(count: 3, height: size.height)
            let x0 = (size.width - layout.width) / 2
            for (i, o) in layout.origins.enumerated() {
                let r = CGRect(x: x0 + o.x, y: o.y, width: layout.cell * 0.866, height: layout.cell).insetBy(dx: 0.6, dy: 0.6)
                let p = MenuBar.hexagon(in: r)
                if i == 0, filled {
                    NSColor.black.setFill()
                    p.fill()
                } else {
                    NSColor.black.setStroke()
                    p.lineWidth = 1.2
                    p.stroke()
                }
            }
            return true
        }
        img.isTemplate = true
        img.accessibilityDescription = "hive"
        return img
    }

    static func hexagon(in r: CGRect) -> NSBezierPath {
        let p = NSBezierPath()
        let c = CGPoint(x: r.midX, y: r.midY), rad = min(r.width / 0.866, r.height) / 2
        for i in 0..<6 {
            let a = (Double(i) * 60 - 90) * .pi / 180
            let pt = CGPoint(x: c.x + rad * cos(a), y: c.y + rad * sin(a))
            if i == 0 { p.move(to: pt) } else { p.line(to: pt) }
        }
        p.close()
        return p
    }

    /// Opens the menu, for screenshot runs (HIVE_OPEN=menubar). Blocks
    /// while the menu tracks: dispatched.
    func open() { DispatchQueue.main.async { [weak self] in self?.item.button?.performClick(nil) } }

    #if DEBUG
    /// Each icon on a light and a dark menu bar, at 2x, for screenshot runs:
    /// the menu bar itself isn't one of Hive's windows.
    func writeIcons(to dir: String) {
        let states: [(String, NSImage, String)] = [
            ("quiet", MenuBar.comb(filled: false), ""),
            ("working", MenuBar.comb(filled: true), ""),
            ("needs-you", MenuBar.comb(filled: true), "2"),
        ]
        for (dark, name) in [(false, "light"), (true, "dark")] {
            let size = NSSize(width: 150, height: 24)
            let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: 300, pixelsHigh: 48, bitsPerSample: 8, samplesPerPixel: 4,
                                       hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
            rep.size = size
            NSAppearance(named: dark ? .darkAqua : .aqua)!.performAsCurrentDrawingAppearance {
                NSGraphicsContext.saveGraphicsState()
                NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
                (dark ? NSColor(white: 0.15, alpha: 1) : NSColor(white: 0.93, alpha: 1)).setFill()
                NSRect(origin: .zero, size: size).fill()
                for (i, (_, img, count)) in states.enumerated() {
                    let x = 12 + CGFloat(i) * 48
                    let r = NSRect(x: x, y: 4, width: 18, height: 16)
                    if img.isTemplate {
                        // As the menu bar draws a template: in its text colour.
                        let tinted = NSImage(size: img.size, flipped: false) { b in
                            img.draw(in: b); NSColor.labelColor.set(); b.fill(using: .sourceAtop); return true
                        }
                        tinted.draw(in: r)
                    } else {
                        img.draw(in: r)
                    }
                    (count as NSString).draw(at: NSPoint(x: x + 20, y: 4), withAttributes: [.font: NSFont.menuBarFont(ofSize: 0), .foregroundColor: NSColor.labelColor])
                }
                NSGraphicsContext.restoreGraphicsState()
            }
            try? rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: "\(dir)/icons-\(name).png"))
        }
    }
    #endif

    // MARK: the menu

    func menuNeedsUpdate(_ menu: NSMenu) {
        let s = current
        menu.removeAllItems()

        let header = NSMenuItem()
        let view = NSHostingView(rootView: MenuHeader(subtitle: s.subtitle, problem: s.icon == .away))
        view.frame = NSRect(x: 0, y: 0, width: 280, height: 46)
        header.view = view
        menu.addItem(header)
        menu.addItem(.separator())

        // Hosts: this Mac, then each remote one with what you can do.
        let hosts = model.hostRows
        if let local = hosts.first(where: \.local) {
            // As Tailscale's "This Device": picked, it shows how your other
            // devices reach this Mac (the phone's QR).
            let mac = ClosureMenuItem(title: "This Mac: \(AppModel.macName)\(local.connected ? "" : ", not connected")", key: "") { [weak self] in
                self?.showShareHosts()
            }
            mac.toolTip = "How your phone and other Macs reach this one"
            menu.addItem(mac)
        }
        let remote = hosts.filter { !$0.local }
        let hostsItem = NSMenuItem(title: "Hosts", action: nil, keyEquivalent: "")
        let sub = NSMenu()
        sub.autoenablesItems = false
        for h in remote {
            let up = h.connected
            let state = up ? "Connected" : h.idle ? "Not connected" : (h.problem ?? "Away")
            let row = NSMenuItem(title: h.name, action: nil, keyEquivalent: "")
            row.attributedTitle = twoLines(h.name, state)
            let actions = NSMenu()
            if up || !h.idle {
                actions.addItem(ClosureMenuItem(title: "Disconnect", key: "") { [weak self] in self?.model.disconnect(host: h.id) })
            } else {
                actions.addItem(ClosureMenuItem(title: "Connect", key: "") { [weak self] in self?.model.connect(host: h.id) })
            }
            row.submenu = actions
            sub.addItem(row)
        }
        if !remote.isEmpty { sub.addItem(.separator()) }
        sub.addItem(ClosureMenuItem(title: "Share Hosts with Phone…", key: "") { [weak self] in self?.showShareHosts() })
        hostsItem.submenu = sub
        menu.addItem(hostsItem)
        menu.addItem(.separator())

        // The agents: what needs you, then what's working.
        // Block ids are per server: keyed by host too.
        let byID = Dictionary(model.agents.map { ("\($0.host)/\($0.blockID)", $0) }, uniquingKeysWith: { a, _ in a })
        if s.needsYou.isEmpty && s.working.isEmpty {
            menu.addItem(disabled(s.quiet > 0 ? "No agent needs you" : "No agents running"))
        }
        if !s.needsYou.isEmpty {
            menu.addItem(.sectionHeader(title: "Needs you"))
            for a in s.needsYou { if let info = byID["\(a.host)/\(a.id)"] { menu.addItem(row(info)) } }
        }
        if !s.working.isEmpty {
            menu.addItem(.sectionHeader(title: "Working"))
            for a in s.working { if let info = byID["\(a.host)/\(a.id)"] { menu.addItem(row(info)) } }
        }
        if s.quiet > 0 {
            menu.addItem(disabled(s.quiet == 1 ? "1 idle agent" : "\(s.quiet) idle agents"))
        }
        menu.addItem(.separator())

        let new = ClosureMenuItem(title: "New Session", key: "n") { [weak self] in self?.showWindow(); self?.model.newSession() }
        new.keyEquivalentModifierMask = [.command, .shift]
        menu.addItem(new)
        menu.addItem(ClosureMenuItem(title: "Open Hive", key: "") { [weak self] in self?.showWindow() })
        menu.addItem(ClosureMenuItem(title: "Settings…", key: ",") { [weak self] in self?.showSettings() })
        menu.addItem(.separator())
        menu.addItem(NSMenuItem(title: "Quit Hive", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q"))
    }

    private func disabled(_ title: String) -> NSMenuItem {
        let i = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        i.isEnabled = false
        return i
    }

    /// An agent: its state's hexagon, what it's on, then where and what it said.
    private func row(_ a: AgentInfo) -> NSMenuItem {
        let block = a.blockID, host = a.host
        let i = ClosureMenuItem(title: a.task, key: "") { [weak self] in
            guard let self else { return }
            // Looked up now: the menu may have been open while it changed.
            let now = self.model.agents.first { $0.blockID == block && $0.host == host }
            // Jumped first, so the window opens on it, not on what it showed.
            if let now { self.model.jump(to: now) }
            self.showWindow()
        }
        let where_ = model.hostRows.first { $0.id == a.host }
        let session = a.sessionLabel.isEmpty ? "a session" : a.sessionLabel
        let place = where_.map { $0.local ? session : "\(session) on \($0.name)" } ?? session
        let word = a.state == "blocked" ? "Blocked" : a.state == "done" ? "Done" : "Working"
        let said = a.message.isEmpty ? word : "\(word): \(a.message)"
        i.attributedTitle = twoLines(MenuBarStatus.clip(a.task, max: 44), MenuBarStatus.clip("\(place), \(said)", max: 58))
        i.setAccessibilityLabel("\(a.task), \(word), in \(place)")
        let color: NSColor = a.state == "blocked" ? model.style.blocked : a.state == "done" ? model.style.done : model.style.accent
        let conf = NSImage.SymbolConfiguration(pointSize: 11, weight: .regular).applying(.init(paletteColors: [color]))
        i.image = NSImage(systemSymbolName: "hexagon.fill", accessibilityDescription: word)?.withSymbolConfiguration(conf)
        return i
    }

    /// The menu's own colour for the first line (it inverts on highlight),
    /// a smaller grey second line.
    private func twoLines(_ first: String, _ second: String) -> NSAttributedString {
        let s = NSMutableAttributedString(string: first, attributes: [.font: NSFont.menuFont(ofSize: 0)])
        s.append(NSAttributedString(string: "\n" + second, attributes: [
            .font: NSFont.systemFont(ofSize: NSFont.smallSystemFontSize), .foregroundColor: NSColor.secondaryLabelColor,
        ]))
        return s
    }
}

/// "hive" and how it is, at the top of the menu.
private struct MenuHeader: View {
    let subtitle: String
    let problem: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 1) {
            Text("hive").font(.system(size: 13, weight: .semibold))
            Text(subtitle).font(.system(size: 11)).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        // In line with the items' text below it.
        .padding(.leading, 16.5).padding(.vertical, 6)
        .accessibilityElement(children: .combine)
    }
}
