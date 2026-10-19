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
    /// Says something, does nothing: never selected, Return skips it.
    var inert = false
    /// A warning: the icon in the warning colour, the text muted.
    var warning = false
    /// Opens a list of its own (rows ending in >).
    var chevron = false
    /// A divider above this row: actions after the list they act on.
    var dividerAbove = false
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
              token: String? = nil, query: String = "", notice: (title: String, detail: String)? = nil,
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
        let view = PaletteView(placeholder: placeholder, style: style, token: token, initialQuery: query, notice: notice, items: items, onCreate: onCreate,
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
    /// Something to say under the field (why an Add Host didn't work).
    var notice: (title: String, detail: String)? = nil
    let items: (String) -> [PaletteItem]
    let onCreate: ((String) -> Void)?
    var onHeight: (CGFloat) -> Void = { _ in }
    let dismiss: () -> Void
    @State private var query = ""
    @State private var selected = 0
    @State private var hoverGate = HoverGate()
    /// The list's own height, measured: headers and dividers vary, so a
    /// count of rows times a constant clipped the last row.
    @State private var listHeight: CGFloat = 0
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
            if let notice {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Image(systemName: "exclamationmark.triangle.fill").font(.system(size: 11)).foregroundStyle(style.blocked.swiftUI)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(notice.title).font(.system(size: 12.5)).foregroundStyle(style.text.swiftUI)
                        if !notice.detail.isEmpty {
                            Text(notice.detail).font(.system(size: 11)).foregroundStyle(style.secondary.swiftUI)
                                .lineLimit(2).fixedSize(horizontal: false, vertical: true)
                        }
                    }
                    Spacer(minLength: 0)
                }
                .padding(.horizontal, 12).padding(.bottom, 10)
            }
            Divider().overlay(style.border.swiftUI)
            ScrollViewReader { proxy in
                ScrollView(showsIndicators: total > 9) {
                    VStack(alignment: .leading, spacing: 1) {
                        ForEach(Array(rows.enumerated()), id: \.element.id) { i, item in
                            if item.dividerAbove, i > 0 {
                                Divider().overlay(style.border.swiftUI).padding(.horizontal, 4).padding(.vertical, 4)
                            }
                            if !item.section.isEmpty, i == 0 || rows[i - 1].section != item.section {
                                // In the rows' columns: icon over icon, text over text.
                                HStack(spacing: 8) {
                                    Group {
                                        if let sym = item.sectionSymbol { Image(systemName: sym) } else { Color.clear }
                                    }
                                    .font(.system(size: 10.5)).frame(width: 16)
                                    Text(item.section).font(.system(size: 11, weight: .medium))
                                        .lineLimit(1).truncationMode(.middle)
                                }
                                .foregroundStyle(style.secondary.swiftUI)
                                .padding(.horizontal, 10).padding(.top, i == 0 ? 2 : 8).padding(.bottom, 2)
                            }
                            row(item, selected: i == selected && !item.inert).id(i).help(item.help)
                                .onTapGesture { guard !item.inert else { return }; item.action(); dismiss() }
                                .onContinuousHover { phase in
                                    // The pointer picks the row it moves onto; rows
                                    // scrolling under a resting pointer don't.
                                    if case .active = phase, !item.inert, hoverGate.accept(pointer: pointerOnScreen()) { selected = i }
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
                    .background(GeometryReader { g in Color.clear.preference(key: ListHeight.self, value: g.size.height) })
                }
                .onPreferenceChange(ListHeight.self) { listHeight = $0 }
                // Up to about nine rows, then it scrolls; the estimate only until measured.
                .frame(height: min(listHeight > 0 ? listHeight : CGFloat(total) * 31 + 12 + sectionHeight(rows) + dividers(rows),
                                   9 * 31 + 12 + sectionHeight(rows) + dividers(rows)))
                .onChange(of: selected) { _, v in proxy.scrollTo(v) }
            }
        }
        .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(style.card.swiftUI))
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(style.border.swiftUI, lineWidth: 1))
        .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
        .background(GeometryReader { g in Color.clear.preference(key: CardHeight.self, value: g.size.height) })
        .onPreferenceChange(CardHeight.self) { onHeight($0) }
        .frame(maxHeight: .infinity, alignment: .top)
        .onAppear {
            focused = true
            selected = firstLive(rows, from: 0, step: 1)
            // After focus, so the caret lands at the end instead of the
            // field selecting what it starts with.
            if !initialQuery.isEmpty { DispatchQueue.main.async { if query.isEmpty { query = initialQuery } } }
        }
        .onChange(of: query) { _, q in selected = firstLive(items(q), from: 0, step: 1) }
        .onKeyPress(.downArrow) {
            // Past the last live row (inert ones only below): stay put.
            let n = firstLive(rows, from: selected + 1, step: 1)
            if n < total { selected = n }
            return .handled
        }
        .onKeyPress(.upArrow) { selected = max(firstLive(rows, from: selected - 1, step: -1), 0); return .handled }
        .onKeyPress(.escape) { dismiss(); return .handled }
    }

    /// The first row from `from` that can be selected, going by `step`.
    private func firstLive(_ rows: [PaletteItem], from: Int, step: Int) -> Int {
        var i = from
        while i >= 0, i < rows.count, rows[i].inert { i += step }
        if i < 0 { return selected }
        return i
    }

    private func dividers(_ rows: [PaletteItem]) -> CGFloat {
        CGFloat(rows.enumerated().filter { $0.offset > 0 && $0.element.dividerAbove }.count) * 9
    }

    private func sectionHeight(_ rows: [PaletteItem]) -> CGFloat {
        var n = 0
        for (i, r) in rows.enumerated() where !r.section.isEmpty && (i == 0 || rows[i - 1].section != r.section) { n += 1 }
        return CGFloat(n) * 22
    }

    private func run(_ rows: [PaletteItem], _ creating: Bool) {
        if selected < rows.count {
            guard !rows[selected].inert else { return }
            rows[selected].action()
        } else if creating {
            onCreate?(query)
        }
        dismiss()
    }

    private func row(_ item: PaletteItem, selected: Bool) -> some View {
        // A checked row without an icon of its own shows its check where
        // the icon goes (the session you're in).
        let leadingCheck = item.checked && item.symbol == nil
        return HStack(spacing: 8) {
            Group {
                if leadingCheck {
                    Image(systemName: "checkmark").fontWeight(.semibold).foregroundStyle(style.accent.swiftUI)
                        .accessibilityLabel("Current")
                } else if let sym = item.symbol {
                    Image(systemName: sym)
                        .foregroundStyle(item.warning ? style.blocked.swiftUI : selected ? style.accent.swiftUI : style.secondary.swiftUI)
                        .accessibilityHidden(true)
                } else {
                    Color.clear
                }
            }
            .font(.system(size: 11.5)).frame(width: 16)
            Text(item.title).font(.system(size: 13))
                .foregroundStyle(item.warning || item.inert ? style.secondary.swiftUI : style.text.swiftUI).lineLimit(1)
            Spacer(minLength: 8)
            if item.checked, !leadingCheck { Image(systemName: "checkmark").font(.system(size: 10.5, weight: .semibold)).foregroundStyle(style.accent.swiftUI) }
            Text(item.detail).font(.system(size: 11.5)).foregroundStyle(style.secondary.swiftUI)
            if item.chevron { Image(systemName: "chevron.right").font(.system(size: 9.5, weight: .semibold)).foregroundStyle(style.secondary.swiftUI) }
        }
        .padding(.horizontal, 10).frame(height: 30)
        .background(RoundedRectangle(cornerRadius: 7, style: .continuous).fill(selected ? style.accent.swiftUI.opacity(0.16) : .clear))
        .contentShape(Rectangle())
        // A host's problem is read with its host: "hive: No hive server running".
        .accessibilityElement(children: .combine)
        .accessibilityLabel(item.inert && !item.section.isEmpty ? Text("\(item.section): \(item.title)") : Text(item.title))
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

private struct ListHeight: PreferenceKey {
    static let defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }
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
