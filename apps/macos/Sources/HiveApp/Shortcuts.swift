import AppKit
import HiveCore
import SwiftUI

/// The Keyboard Shortcuts window (⌘/). Menu shortcuts are read from the menu
/// bar, so the list matches the real bindings; editing keys and mouse
/// gestures come from HiveCore's ShortcutGuide.
@MainActor
final class ShortcutsPanel {
    private var panel: NSPanel?
    private var monitor: Any?

    var isOpen: Bool { panel != nil }

    func toggle(over window: NSWindow, style: Style) {
        if isOpen { close(); return }
        let f = window.frame
        // Fits a small window too: the list scrolls.
        let size = NSSize(width: min(900, f.width - 40), height: min(560, f.height - 60))
        let p = KeyPanel(contentRect: NSRect(origin: .zero, size: size),
                         styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: false)
        p.isOpaque = false
        p.backgroundColor = .clear
        p.hasShadow = true
        p.level = .floating
        p.contentView = NSHostingView(rootView: ShortcutsView(sections: ShortcutsPanel.sections(), style: style) { [weak self] in self?.close() })
        p.setFrame(NSRect(x: f.midX - size.width / 2, y: f.midY - size.height / 2 + 20, width: size.width, height: size.height), display: true)
        window.addChildWindow(p, ordered: .above)
        p.makeKeyAndOrderFront(nil)
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.05) { p.invalidateShadow() }
        panel = p
        monitor = NSEvent.addLocalMonitorForEvents(matching: [.leftMouseDown]) { [weak self] e in
            if e.window !== self?.panel { self?.close() }
            return e
        }
    }

    func close() {
        if let monitor { NSEvent.removeMonitor(monitor) }
        monitor = nil
        panel?.parent?.removeChildWindow(panel!)
        panel?.orderOut(nil)
        panel = nil
    }

    /// AppKit's own items that are hive's too. Everything else AppKit adds
    /// (dictation, emoji, full screen) is left out: it draws those itself.
    static let systemItems: Set<String> = ["Copy", "Paste", "Hide Hive", "Quit Hive"]

    static func shortcut(_ item: NSMenuItem) -> MenuShortcut? {
        guard !item.isSeparatorItem, !item.isHidden, !item.isAlternate, !item.keyEquivalent.isEmpty,
              item is ClosureMenuItem || systemItems.contains(item.title) else { return nil }
        let m = item.keyEquivalentModifierMask
        var mods: KeyMods = []
        if m.contains(.command) { mods.insert(.command) }
        if m.contains(.shift) { mods.insert(.shift) }
        if m.contains(.option) { mods.insert(.alt) }
        if m.contains(.control) { mods.insert(.ctrl) }
        return MenuShortcut(title: item.title, key: item.keyEquivalent, mods: mods, function: m.contains(.function))
    }

    /// The shortcut of the menu item with this title, as the menu bar shows
    /// it. AppKit localizes key equivalents for the keyboard layout (on a
    /// Portuguese one, ⇧⌘] becomes ⇧⌘~), so it's read back from the menu,
    /// never written by hand.
    static func keys(for title: String) -> String {
        for top in NSApp.mainMenu?.items ?? [] {
            for item in top.submenu?.items ?? [] where item.title == title {
                if let keys = shortcut(item)?.keys { return keys }
            }
        }
        return ""
    }

    /// The menu bar's shortcuts by menu, then the editing keys and mouse.
    static func sections() -> [(String, [ShortcutRow])] {
        var out: [(String, [ShortcutRow])] = []
        for top in NSApp.mainMenu?.items ?? [] {
            guard let menu = top.submenu else { continue }
            let rows = ShortcutGuide.rows(menu.items.compactMap(shortcut))
            if !rows.isEmpty { out.append((menu.title == "Hive" ? "App" : menu.title, rows)) }
        }
        out.append(("Editing in terminals", ShortcutGuide.editing))
        out.append(("Mouse", ShortcutGuide.mouse))
        return out
    }
}

struct ShortcutsView: View {
    let sections: [(String, [ShortcutRow])]
    let style: Style
    let close: () -> Void
    @State private var query = ""
    @FocusState private var focused: Bool

    var body: some View {
        let s = style
        let q = query.lowercased()
        let shown = sections.map { title, rows in
            (title, q.isEmpty ? rows : rows.filter { ShortcutGuide.matches($0, q) })
        }.filter { !$0.1.isEmpty }
        let column = ShortcutGuide.columns(shown.map(\.1.count), count: 3)
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 10) {
                Text("Keyboard Shortcuts").font(.system(size: 14, weight: .semibold)).foregroundStyle(s.text.swiftUI)
                    .accessibilityAddTraits(.isHeader)
                Text("esc to close").font(.system(size: 11)).foregroundStyle(s.secondary.swiftUI)
                Spacer()
                ZStack(alignment: .leading) {
                    if query.isEmpty { Text("Filter").foregroundStyle(s.secondary.swiftUI) }
                    TextField("", text: $query).textFieldStyle(.plain).focused($focused)
                        .accessibilityLabel("Filter shortcuts")
                }
                .font(.system(size: 13)).frame(width: 180)
                .padding(.horizontal, 10).padding(.vertical, 5)
                .background(RoundedRectangle(cornerRadius: 7, style: .continuous).fill(s.text.swiftUI.opacity(0.06)))
            }
            .padding(.horizontal, 20).padding(.vertical, 14)
            Rectangle().fill(s.border.swiftUI).frame(height: 1)
            ScrollView {
                HStack(alignment: .top, spacing: 26) {
                    ForEach(0..<3, id: \.self) { col in
                        VStack(alignment: .leading, spacing: 18) {
                            ForEach(Array(shown.enumerated()).filter { column[$0.offset] == col }, id: \.offset) { _, section in
                                VStack(alignment: .leading, spacing: 6) {
                                    Text(section.0.uppercased()).font(.system(size: 10, weight: .semibold)).tracking(0.6)
                                        .foregroundStyle(s.secondary.swiftUI).accessibilityAddTraits(.isHeader)
                                    ForEach(Array(section.1.enumerated()), id: \.offset) { _, row in
                                        HStack(alignment: .firstTextBaseline, spacing: 12) {
                                            Text(row.title).font(.system(size: 12.5)).foregroundStyle(s.text.swiftUI)
                                                .fixedSize(horizontal: false, vertical: true)
                                            Spacer(minLength: 8)
                                            Text(row.keys).font(.system(size: 12, design: .rounded)).foregroundStyle(s.secondary.swiftUI)
                                                .monospacedDigit()
                                        }
                                        .accessibilityElement(children: .combine)
                                    }
                                }
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .topLeading)
                    }
                }
                .padding(20)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(s.card.swiftUI))
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(s.border.swiftUI, lineWidth: 1))
        .onAppear { focused = true }
        .onKeyPress(.escape) { close(); return .handled }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Keyboard Shortcuts")
    }
}

/// Over the window: the keys that can follow a sequence's first key, or a
/// mode's keys, at the bottom. Clicks pass through to the panes.
final class KeyHintsHost: NSHostingView<KeyHintsView> {
    override func hitTest(_ point: NSPoint) -> NSView? { nil }
    // VoiceOver's pointer lands on the pane below, not this layer.
    override func accessibilityHitTest(_ point: NSPoint) -> Any? { nil }
}

struct KeyHintsView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        let s = model.style
        VStack {
            Spacer()
            if let k = model.keyHints {
                VStack(alignment: .leading, spacing: 8) {
                    Text(k.heading).font(.system(size: 12, weight: .semibold, design: .rounded)).foregroundStyle(s.accentText.swiftUI)
                    let cols = k.hints.count > 6 ? 2 : 1
                    LazyVGrid(columns: Array(repeating: GridItem(.flexible(minimum: 150), alignment: .leading), count: cols), alignment: .leading, spacing: 5) {
                        ForEach(Array(k.hints.prefix(k.hints.count > 18 ? 16 : 18).enumerated()), id: \.offset) { _, h in
                            HStack(spacing: 8) {
                                Text(h.keys).font(.system(size: 11.5, weight: .medium, design: .monospaced))
                                    .foregroundStyle(s.text.swiftUI)
                                    .padding(.horizontal, 6).padding(.vertical, 2)
                                    .frame(minWidth: 34)
                                    .background(RoundedRectangle(cornerRadius: 5, style: .continuous).fill(s.text.swiftUI.opacity(0.08)))
                                Text(h.title).font(.system(size: 12)).foregroundStyle(s.secondary.swiftUI).lineLimit(1)
                            }
                        }
                    }
                    if k.hints.count > 18 {
                        Text("and \(k.hints.count - 16) more, in Settings, Shortcuts").font(.system(size: 11.5)).foregroundStyle(s.secondary.swiftUI)
                    }
                }
                .padding(.horizontal, 14).padding(.vertical, 12)
                .frame(maxWidth: 460, alignment: .leading)
                .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(s.card.swiftUI)
                    .shadow(color: .black.opacity(s.dark ? 0.35 : 0.12), radius: 12, y: 4))
                .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(s.border.swiftUI, lineWidth: 1))
                .padding(.bottom, 22)
                .transition(.opacity.combined(with: .move(edge: .bottom)))
                .accessibilityElement(children: .combine)
                .accessibilityLabel("Keys after \(k.spokenHeading.isEmpty ? k.heading : k.spokenHeading): " + k.hints.map { "\($0.spoken.isEmpty ? $0.keys : $0.spoken), \($0.title)" }.joined(separator: "; "))
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .animation(model.motion == nil ? nil : .easeOut(duration: 0.15), value: model.keyHints)
    }
}
