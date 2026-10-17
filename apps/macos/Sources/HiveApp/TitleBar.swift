import AppKit
import SwiftUI

/// The title bar: session chip, tabs, palette and new-tab buttons. Lives in
/// the window's title bar area, right of the traffic lights.
struct TitleBar: View {
    @ObservedObject var model: AppModel
    var openPicker: () -> Void
    var openPalette: () -> Void

    static let height: CGFloat = 38   // = the title bar; traffic lights are centered at 19
    /// Room for the traffic lights.
    static let leading: CGFloat = 78

    var body: some View {
        let s = model.style
        HStack(spacing: 10) {
            if !model.sidebar {
                BarButton(symbol: "sidebar.left", help: "Show agents (⇧⌘S)", style: s) { model.sidebar = true }
                    .padding(.trailing, -4)
            }
            SessionChip(model: model, action: openPicker)
            // With the sidebar open, tabs live there, as in the reference app's vertical tabs.
            if !model.sidebar { Tabs(model: model) }
            Spacer(minLength: 4)
            HStack(spacing: 2) {
                BarButton(symbol: "command", help: "Command palette (⇧⌘P)", style: s, action: openPalette)
                BarButton(symbol: "plus", help: "New tab (⌘T)", style: s) { model.newTab() }
            }
        }
        .padding(.leading, model.sidebar ? 12 : TitleBar.leading)
        .padding(.trailing, 10)
        .frame(height: TitleBar.height)
    }
}

/// The session and host. Opens the session picker.
struct SessionChip: View {
    @ObservedObject var model: AppModel
    let action: () -> Void
    @State private var hover = false

    var body: some View {
        let s = model.style
        Button(action: action) {
            HStack(spacing: 8) {
                Image(systemName: "laptopcomputer").font(.system(size: 13, weight: .regular))
                    .foregroundStyle(s.secondary.swiftUI)
                    .frame(width: 18)
                VStack(alignment: .leading, spacing: 0) {
                    Text(model.sessionLabel.isEmpty ? "hive" : model.sessionLabel)
                        .font(.system(size: 12.5, weight: .semibold)).foregroundStyle(s.text.swiftUI)
                    Text(model.host).font(.system(size: 10)).foregroundStyle(s.secondary.swiftUI)
                }
                .lineLimit(1)
                Image(systemName: "chevron.down").font(.system(size: 8, weight: .bold))
                    .foregroundStyle(s.secondary.swiftUI.opacity(hover ? 1 : 0.6))
            }
            .padding(.horizontal, 8).frame(height: 32)
            .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(s.text.swiftUI.opacity(hover ? 0.07 : 0)))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hover = $0 }
        .help("Sessions (⌘K)")
        .fixedSize()
    }
}

struct Tabs: View {
    @ObservedObject var model: AppModel

    var body: some View {
        HStack(spacing: 4) {
            ForEach(Array(model.windows.enumerated()), id: \.element.id) { i, w in
                TabPill(model: model, window: w, index: i, active: w.id == model.active?.id)
            }
        }
    }
}

struct TabPill: View {
    @ObservedObject var model: AppModel
    let window: WindowInfo
    let index: Int
    let active: Bool
    @State private var hover = false

    var body: some View {
        let s = model.style
        let info = window.focused.flatMap { model.blocks[$0] }
        let parts = window.titleParts(model.blocks)
        let status = model.status(of: window)
        HStack(spacing: 7) {
            ZStack {
                RoundedRectangle(cornerRadius: 4, style: .continuous)
                    .fill(active ? s.accent.swiftUI.opacity(0.18) : s.text.swiftUI.opacity(0.08))
                Image(systemName: info?.symbol ?? "apple.terminal").font(.system(size: 9, weight: .semibold))
                    .foregroundStyle(active ? s.accent.swiftUI : s.secondary.swiftUI)
            }
            .frame(width: 20, height: 16)
            (Text(parts.primary).foregroundColor(active ? s.text.swiftUI : s.secondary.swiftUI)
                + Text(parts.secondary.isEmpty ? "" : " " + parts.secondary).foregroundColor(s.secondary.swiftUI.opacity(0.8)))
                .font(.system(size: 12.5, weight: active ? .medium : .regular))
                .lineLimit(1)
                .fixedSize()                      // never an ellipsis…
                .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading) // minWidth 0: may be narrower than the text
                .clipped()
                .mask(FadeMask())                 // …the end fades instead
            if let status { StatusBadge(state: status == "blocked" ? "blocked-dot" : status, style: s) }
        }
        .padding(.leading, 6).padding(.trailing, 10)
        .frame(width: 210, height: 28, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .fill(active ? s.tabActive.swiftUI : (hover ? s.text.swiftUI.opacity(0.05) : .clear))
                .shadow(color: .black.opacity(active && !s.dark ? 0.06 : 0), radius: 1, y: 0.5)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .strokeBorder(active ? s.border.swiftUI : .clear, lineWidth: 1)
        )
        .contentShape(Rectangle())
        .onTapGesture { model.selectWindow(index: index) }
        .onHover { hover = $0 }
        .help("Tab \(index + 1) (⌘\(index + 1))")
    }
}

/// Fades the end of a title instead of an ellipsis.
struct FadeMask: View {
    var body: some View {
        LinearGradient(stops: [.init(color: .black, location: 0.82), .init(color: .clear, location: 1)],
                       startPoint: .leading, endPoint: .trailing)
    }
}

struct BarButton: View {
    let symbol: String
    let help: String
    let style: Style
    let action: () -> Void
    @State private var hover = false
    var body: some View {
        Button(action: action) {
            Image(systemName: symbol).font(.system(size: 13, weight: .regular))
                .foregroundStyle(style.secondary.swiftUI)
                .frame(width: 28, height: 28)
                .background(RoundedRectangle(cornerRadius: 7, style: .continuous).fill(style.text.swiftUI.opacity(hover ? 0.08 : 0)))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hover = $0 }
        .help(help)
    }
}

/// A hosting view in the title bar that still lets the window be dragged
/// from its empty parts.
final class TitleBarHost<V: View>: NSHostingView<V> {
    override var mouseDownCanMoveWindow: Bool { true }
}
