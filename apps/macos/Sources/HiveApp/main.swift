import AppKit
import SwiftUI

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate {
    let model = AppModel()
    var window: NSWindow!
    var terminal: TerminalView!

    func applicationDidFinishLaunching(_ note: Notification) {
        terminal = TerminalView(model: model)

        let tabs = NSHostingView(rootView: TabBar(model: model))
        let right = MainPane(tabs: tabs, terminal: terminal)
        let split = NSSplitViewController()
        let sidebarItem = NSSplitViewItem(sidebarWithViewController: NSHostingController(rootView: Sidebar(model: model)))
        sidebarItem.minimumThickness = 200
        sidebarItem.maximumThickness = 320
        let mainVC = NSViewController()
        mainVC.view = right
        split.addSplitViewItem(sidebarItem)
        split.addSplitViewItem(NSSplitViewItem(viewController: mainVC))

        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1280, height: 800),
                          styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
                          backing: .buffered, defer: false)
        window.title = "Hive"
        window.titlebarAppearsTransparent = true
        window.appearance = NSAppearance(named: .darkAqua)
        window.backgroundColor = NSColor(red: 0x1d / 255, green: 0x1f / 255, blue: 0x21 / 255, alpha: 1)
        window.contentViewController = split
        window.minSize = NSSize(width: 720, height: 420)
        window.setFrameAutosaveName("HiveMain")
        // The controller sized the window to its fitting size, and a saved
        // frame may be from a bad launch: never start smaller than useful.
        if window.frame.width < 900 || window.frame.height < 560 {
            window.setContentSize(NSSize(width: 1280, height: 800))
            window.center()
        }
        window.delegate = self
        window.makeKeyAndOrderFront(nil)
        window.makeFirstResponder(terminal)

        buildMenu()
        model.start()
        NSApp.activate(ignoringOtherApps: true)
    }

    func windowDidBecomeKey(_ notification: Notification) {
        terminal.claimSizes()
        terminal.needsDisplay = true
    }

    func windowDidResignKey(_ notification: Notification) { terminal.needsDisplay = true }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }

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

        menu("Hive", [
            NSMenuItem(title: "About Hive", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: ""),
            .separator(),
            NSMenuItem(title: "Hide Hive", action: #selector(NSApplication.hide(_:)), keyEquivalent: "h"),
            NSMenuItem(title: "Quit Hive", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q"),
        ])
        menu("Shell", [
            item("New Session", "n", [.command, .shift]) { [weak self] in self?.model.newSession() },
            item("New Tab", "t") { [weak self] in self?.model.newTab() },
            item("Split Right", "d") { [weak self] in
                guard let self else { return }
                let s = terminal.splitSize(horizontal: true)
                model.split(horizontal: true, cols: s?.0, rows: s?.1)
            },
            item("Split Down", "d", [.command, .shift]) { [weak self] in
                guard let self else { return }
                let s = terminal.splitSize(horizontal: false)
                model.split(horizontal: false, cols: s?.0, rows: s?.1)
            },
            .separator(),
            item("Close Pane", "w") { [weak self] in self?.model.closePane() },
        ])
        menu("Edit", [
            NSMenuItem(title: "Copy", action: #selector(TerminalView.copy(_:)), keyEquivalent: "c"),
            NSMenuItem(title: "Paste", action: #selector(TerminalView.paste(_:)), keyEquivalent: "v"),
        ])
        var paneItems: [NSMenuItem] = [item("Zoom Pane", "\r", [.command, .shift]) { [weak self] in self?.model.toggleZoom() }, .separator()]
        for (name, key) in arrows {
            paneItems.append(item("Focus \(name)", key, [.command, .option]) { [weak self] in self?.model.focus(name.lowercased()) })
        }
        paneItems.append(.separator())
        for (name, key) in arrows {
            paneItems.append(item("Resize \(name)", key, [.command, .control]) { [weak self] in self?.model.resizePane(name.lowercased()) })
        }
        menu("Pane", paneItems)
        var windowItems: [NSMenuItem] = [
            item("Next Tab", "]", [.command, .shift]) { [weak self] in self?.model.selectWindow(offset: 1) },
            item("Previous Tab", "[", [.command, .shift]) { [weak self] in self?.model.selectWindow(offset: -1) },
            .separator(),
        ]
        for i in 1...9 {
            windowItems.append(item("Tab \(i)", "\(i)") { [weak self] in self?.model.selectWindow(index: i - 1) })
        }
        menu("Window", windowItems)
        NSApp.mainMenu = main
    }
}

/// The tab bar on top and the terminal below, laid out by hand: stack
/// views left the Metal view at its minimum size.
final class MainPane: NSView {
    let tabs: NSView, terminal: NSView
    let tabHeight: CGFloat = 36
    init(tabs: NSView, terminal: NSView) {
        self.tabs = tabs; self.terminal = terminal
        super.init(frame: NSRect(x: 0, y: 0, width: 1000, height: 700))
        addSubview(tabs)
        addSubview(terminal)
    }
    required init?(coder: NSCoder) { fatalError() }
    override var isFlipped: Bool { true }
    override func layout() {
        super.layout()
        tabs.frame = NSRect(x: 0, y: 0, width: bounds.width, height: tabHeight)
        terminal.frame = NSRect(x: 0, y: tabHeight, width: bounds.width, height: max(0, bounds.height - tabHeight))
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
