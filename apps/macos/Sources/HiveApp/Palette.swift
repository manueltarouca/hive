import AppKit
import HiveCore
import SwiftUI

/// One row of the command palette or session picker.
struct PaletteItem: Identifiable {
    /// What the row stands for, when its title isn't unique (a session's ID).
    var key: String = ""
    /// Stable across redraws: a new identity each time would rebuild the
    /// row under the pointer and lose its hover.
    var id: String { key.isEmpty ? section + "\u{1}" + title : key }
    var title: String
    var detail: String = ""   // shortcut or current value, right-aligned
    var symbol: String? = nil
    var checked = false
    var section: String = ""
    /// The section header's icon (a host's: laptop or globe).
    var sectionSymbol: String? = nil
    /// A tooltip: the full reason behind a short row.
    var help: String = ""
    var action: () -> Void
}

/// A floating search-and-pick panel: the command palette and the session
/// picker are both one of these.
@MainActor
final class PalettePanel {
    private var panel: NSPanel?
    private var monitor: Any?

    /// token: a chip before the field naming what's being entered ("Add
    /// Host"), in place of the magnifying glass. query: what the field
    /// starts with.
    func show(over window: NSWindow, at origin: CGPoint? = nil, width: CGFloat, placeholder: String, style: Style,
              token: String? = nil, query: String = "",
              items: @escaping (String) -> [PaletteItem], onCreate: ((String) -> Void)? = nil) {
        close()
        let p = KeyPanel(contentRect: NSRect(x: 0, y: 0, width: width, height: 360),
                         styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: false)
        p.isOpaque = false
        p.backgroundColor = .clear
        p.hasShadow = true
        p.level = .floating
        p.acceptsMouseMovedEvents = true // hover picks rows
        // Test runs capture a window behind the user's: keep it showing.
        if ProcessInfo.processInfo.environment["HIVE_DRAW_COVERED"] == "1" { p.hidesOnDeactivate = false }
        // The panel fits the card: a taller clear panel would catch hovers
        // and clicks meant for the window below and swallow them.
        let view = PaletteView(placeholder: placeholder, style: style, token: token, initialQuery: query, items: items, onCreate: onCreate,
                               onHeight: { [weak p] h in
                                   guard let p, h > 0, abs(p.frame.height - h) > 0.5 else { return }
                                   p.setFrame(NSRect(x: p.frame.minX, y: p.frame.maxY - h, width: p.frame.width, height: h), display: true)
                                   p.invalidateShadow()
                               }) { [weak self] in self?.close() }
        p.contentView = NSHostingView(rootView: view)
        let frame = window.frame
        let x = origin.map { frame.minX + $0.x } ?? frame.midX - width / 2
        let top = origin.map { frame.maxY - $0.y } ?? frame.maxY - 90
        p.setFrame(NSRect(x: x, y: top - 360, width: width, height: 360), display: true)
        window.addChildWindow(p, ordered: .above)
        p.makeKeyAndOrderFront(nil)
        // A clear panel's shadow follows its drawn content: recompute it once
        // SwiftUI has drawn the card.
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.05) { p.invalidateShadow() }
        panel = p
        // Clicking anywhere else closes it.
        monitor = NSEvent.addLocalMonitorForEvents(matching: [.leftMouseDown]) { [weak self] e in
            if e.window !== self?.panel { self?.close() }
            return e
        }
    }

    func close() {
        if let monitor { NSEvent.removeMonitor(monitor) }
        monitor = nil
        if let panel {
            panel.parent?.removeChildWindow(panel)
            panel.orderOut(nil)
        }
        panel = nil
    }

    var isOpen: Bool { panel != nil }
}

final class KeyPanel: NSPanel {
    override var canBecomeKey: Bool { true }
}

struct PaletteView: View {
    let placeholder: String
    let style: Style
    var token: String? = nil
    var initialQuery = ""
    let items: (String) -> [PaletteItem]
    let onCreate: ((String) -> Void)?
    var onHeight: (CGFloat) -> Void = { _ in }
    let dismiss: () -> Void
    @State private var query = ""
    @State private var selected = 0
    @State private var hoverGate = HoverGate()
    @FocusState private var focused: Bool

    var body: some View {
        let rows = items(query)
        let creating = onCreate != nil && !query.trimmingCharacters(in: .whitespaces).isEmpty
            && !rows.contains { $0.title.caseInsensitiveCompare(query) == .orderedSame }
        let total = rows.count + (creating ? 1 : 0)
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                if let token {
                    Text(token).font(.system(size: 11.5, weight: .semibold)).foregroundStyle(style.text.swiftUI)
                        .padding(.horizontal, 7).padding(.vertical, 3)
                        .background(RoundedRectangle(cornerRadius: 5, style: .continuous).fill(style.text.swiftUI.opacity(0.1)))
                } else {
                    Image(systemName: "magnifyingglass").font(.system(size: 12)).foregroundStyle(style.secondary.swiftUI)
                }
                // The placeholder is drawn by hand: macOS ignores prompt colors.
                TextField("", text: $query)
                    .textFieldStyle(.plain)
                    .tint(style.accent.swiftUI)
                    .overlay(alignment: .leading) {
                        if query.isEmpty {
                            Text(placeholder).font(.system(size: 13.5)).foregroundStyle(style.secondary.swiftUI).allowsHitTesting(false)
                        }
                    }
                    .font(.system(size: 13.5))
                    .foregroundStyle(style.text.swiftUI)
                    .focused($focused)
                    .onSubmit { run(rows, creating) }
            }
            .padding(.horizontal, 12).frame(height: 40)
            Divider().overlay(style.border.swiftUI)
            ScrollViewReader { proxy in
                ScrollView(showsIndicators: total > 9) {
                    VStack(alignment: .leading, spacing: 1) {
                        ForEach(Array(rows.enumerated()), id: \.element.id) { i, item in
                            if !item.section.isEmpty, i == 0 || rows[i - 1].section != item.section {
                                HStack(spacing: 5) {
                                    if let sym = item.sectionSymbol { Image(systemName: sym).font(.system(size: 10)) }
                                    Text(item.section).font(.system(size: 10.5, weight: .semibold))
                                        .lineLimit(1).truncationMode(.middle)
                                }
                                .foregroundStyle(style.secondary.swiftUI)
                                .padding(.horizontal, 10).padding(.top, i == 0 ? 2 : 8).padding(.bottom, 2)
                            }
                            row(item, selected: i == selected).id(i).help(item.help)
                                .onTapGesture { item.action(); dismiss() }
                                .onContinuousHover { phase in
                                    // The pointer picks the row it moves onto; rows
                                    // scrolling under a resting pointer don't.
                                    if case .active = phase, hoverGate.accept(pointer: pointerOnScreen()) { selected = i }
                                }
                        }
                        if creating {
                            row(PaletteItem(title: "Create “\(query)”", symbol: "plus", action: {}), selected: selected == rows.count)
                                .id(rows.count)
                                .onTapGesture { onCreate?(query); dismiss() }
                                .onContinuousHover { phase in
                                    if case .active = phase, hoverGate.accept(pointer: pointerOnScreen()) { selected = rows.count }
                                }
                        }
                    }
                    .padding(6)
                }
                .frame(height: min(CGFloat(total) * 31 + 12 + sectionHeight(rows), 9 * 31 + 12 + sectionHeight(rows)))
                .onChange(of: selected) { _, v in proxy.scrollTo(v) }
            }
        }
        .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(style.card.swiftUI))
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(style.border.swiftUI, lineWidth: 1))
        .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
        .background(GeometryReader { g in Color.clear.preference(key: CardHeight.self, value: g.size.height) })
        .onPreferenceChange(CardHeight.self) { onHeight($0) }
        .frame(maxHeight: .infinity, alignment: .top)
        .onAppear { if query.isEmpty { query = initialQuery }; focused = true }
        .onChange(of: query) { _, _ in selected = 0 }
        .onKeyPress(.downArrow) { selected = min(selected + 1, max(0, total - 1)); return .handled }
        .onKeyPress(.upArrow) { selected = max(selected - 1, 0); return .handled }
        .onKeyPress(.escape) { dismiss(); return .handled }
    }

    private func sectionHeight(_ rows: [PaletteItem]) -> CGFloat {
        var n = 0
        for (i, r) in rows.enumerated() where !r.section.isEmpty && (i == 0 || rows[i - 1].section != r.section) { n += 1 }
        return CGFloat(n) * 22
    }

    private func run(_ rows: [PaletteItem], _ creating: Bool) {
        if selected < rows.count {
            rows[selected].action()
        } else if creating {
            onCreate?(query)
        }
        dismiss()
    }

    private func row(_ item: PaletteItem, selected: Bool) -> some View {
        HStack(spacing: 8) {
            Group {
                if let sym = item.symbol { Image(systemName: sym) } else { Color.clear }
            }
            .font(.system(size: 11.5)).frame(width: 16)
            .foregroundStyle(selected ? style.accent.swiftUI : style.secondary.swiftUI)
            Text(item.title).font(.system(size: 13)).foregroundStyle(style.text.swiftUI).lineLimit(1)
            Spacer(minLength: 8)
            if item.checked { Image(systemName: "checkmark").font(.system(size: 10.5, weight: .semibold)).foregroundStyle(style.accent.swiftUI) }
            Text(item.detail).font(.system(size: 11.5)).foregroundStyle(style.secondary.swiftUI)
        }
        .padding(.horizontal, 10).frame(height: 30)
        .background(RoundedRectangle(cornerRadius: 7, style: .continuous).fill(selected ? style.accent.swiftUI.opacity(0.16) : .clear))
        .contentShape(Rectangle())
    }
}

/// Where the pointer is, as the event being handled says: a mouse event's
/// own location (posted or real), else the pointer's (a key press moved the
/// rows under it).
@MainActor
func pointerOnScreen() -> CGPoint {
    if let e = NSApp.currentEvent, let w = e.window,
       [.mouseMoved, .leftMouseDragged, .mouseEntered, .mouseExited].contains(e.type) {
        return w.convertPoint(toScreen: e.locationInWindow)
    }
    return NSEvent.mouseLocation
}

private struct CardHeight: PreferenceKey {
    static let defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }
}

/// Simple subsequence fuzzy match.
func fuzzy(_ text: String, _ query: String) -> Bool {
    let q = query.lowercased().filter { !$0.isWhitespace }
    guard !q.isEmpty else { return true }
    var it = text.lowercased().makeIterator()
    for c in q {
        var found = false
        while let t = it.next() { if t == c { found = true; break } }
        if !found { return false }
    }
    return true
}
