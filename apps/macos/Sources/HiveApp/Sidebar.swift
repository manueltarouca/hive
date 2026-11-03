import HiveCore
import SwiftUI
import UniformTypeIdentifiers

/// Vertical tabs: the current session's tabs, in order. A tab running an
/// agent shows the agent's task and state on the row itself, so the tab is
/// the agent and nothing is listed twice. Agents in other sessions that need
/// the user show as a badge on the session chip and in the session picker.
struct AgentSidebar: View {
    @ObservedObject var model: AppModel
    static let width: CGFloat = 248
    @StateObject private var reorder = Reorder(horizontal: false, spacing: 2)

    var body: some View {
        let s = model.style
        VStack(alignment: .leading, spacing: 0) {
            // The top strip belongs to the traffic lights.
            HStack {
                Spacer()
                BarButton(symbol: "sidebar.left", help: "Hide sidebar (⇧⌘S)", style: s) { model.sidebar = false }
            }
            .frame(height: TitleBar.height).padding(.trailing, 8)

            ScrollView(showsIndicators: false) {
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(model.shownWindows, id: \.window.id) { t in
                        let i = t.index, w = t.window
                        // Drag to reorder: the others make room where it will land; pins stay first.
                        TabRow(model: model, window: w, index: i, dragged: reorder.dragging == w.id)
                            .reorderable(w.id, in: reorder, order: { model.shownWindows.map(\.window.id) }, space: "sidebar-tabs") { id, to in
                                model.moveShown(id, to: to)
                            }
                            .transition(TabMotion.transition(anchor: .top))
                    }
                }
                // A new tab grows in, a closed one shrinks away, the others slide over.
                .animation(model.motion, value: model.shownWindows.map(\.window.id))
                .reorderSpace(reorder, name: "sidebar-tabs", order: model.shownWindows.map(\.window.id))
                .padding(.horizontal, 8).padding(.top, 4).padding(.bottom, 12)
            }
            // Rows fade out under the title bar instead of being cut off.
            .mask(VStack(spacing: 0) {
                LinearGradient(colors: [.clear, .black], startPoint: .top, endPoint: .bottom).frame(height: 10)
                Rectangle()
            })
        }
        // The window's own surface, with no dividing line: the panes sit beside it as cards.
        .frame(width: AgentSidebar.width)
        .contextMenu { HiveContextMenu(model: model) }
    }
}

/// An app tile: the program's icon, and a dot on its corner when the agent
/// needs the user.
struct AppTile: View {
    let program: Program
    var behind: [Program] = []
    let style: Style
    var badge: String? = nil
    var body: some View {
        // A fixed column, so titles line up however many panes a tab has.
        ProgramTile(program: program, size: 24, behind: behind)
            .frame(width: 24, height: 24)
        .overlay(alignment: .topTrailing) {
            if let badge {
                // The ring is cut out of the tile, so it shows whatever is behind the row.
                ZStack {
                    Image(systemName: "hexagon.fill").font(.system(size: 11)).blendMode(.destinationOut)
                    Image(systemName: "hexagon.fill").font(.system(size: 8))
                        .foregroundStyle(badge == "blocked" ? style.blocked.swiftUI : style.done.swiftUI)
                }
                .offset(x: 4, y: -4)
            }
        }
        .compositingGroup()
    }
}

struct TabRow: View {
    @ObservedObject var model: AppModel
    let window: WindowInfo
    let index: Int
    var dragged = false
    @State private var hover = false

    var body: some View {
        let s = model.style
        let active = window.id == model.active?.id
        let info = window.focused.flatMap { model.blocks[$0] }
        let sum = model.summary(of: window)
        let blocked = sum.state == "blocked" || sum.state == "stopped"
        HStack(alignment: sum.detail == nil ? .center : .top, spacing: 10) {
            AppTile(program: model.program(of: window), behind: model.otherPrograms(of: window), style: s,
                    badge: blocked ? "blocked" : nil)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    if window.pinned {
                        Image(systemName: "pin.fill").font(.system(size: 8, weight: .semibold)).foregroundStyle(s.accent.swiftUI)
                            .accessibilityLabel("Pinned")
                    }
                    Text(sum.title).font(.system(size: 12.5, weight: active ? .medium : .regular))
                        .foregroundStyle(s.text.swiftUI.opacity(active ? 1 : 0.86)).lineLimit(1)
                    Spacer(minLength: 4)
                    // Pointing at a row swaps its state for the close button.
                    if hover && !dragged {
                        TabCloseButton(style: s) { model.closeTab(window) }
                            .transition(.opacity)
                    } else if blocked {
                        StatusBadge(state: sum.state ?? "blocked", style: s)
                    } else if let st = sum.state {
                        StatusBadge(state: st, style: s)
                    }
                }
                if let detail = sum.detail {
                    // Why it's waiting matters most: up to two lines.
                    Text(detail).font(.system(size: 11))
                        .foregroundStyle(blocked ? s.blockedText.swiftUI : s.secondary.swiftUI)
                        .lineLimit(blocked ? 2 : 1).truncationMode(sum.isAgent ? .tail : .head)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
        }
        .padding(.horizontal, 8).padding(.vertical, sum.detail == nil ? 7 : 8)
        // Dragged: lifted on the bar's own surface, outlined in honey, so it reads as the one moving.
        .background(RoundedRectangle(cornerRadius: 10, style: .continuous)
            .fill(dragged ? s.tabActive.swiftUI : active ? s.tabActive.swiftUI : (hover ? s.text.swiftUI.opacity(0.05) : .clear)))
        .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous)
            .strokeBorder(dragged ? s.accent.swiftUI.opacity(0.8) : .clear, lineWidth: 1.5))
        .animation(.easeOut(duration: 0.12), value: hover)
        .contentShape(Rectangle())
        .onTapGesture { model.selectWindow(index: index) }
        .onHover { hover = $0 }
        .onReceive(model.$shotHover) { if let i = $0 { hover = model.shownWindows.firstIndex { $0.window.id == window.id } == i } }
        .contextMenu {
            Button(window.pinned ? "Unpin Tab" : "Pin Tab") { model.pinTab(window, !window.pinned) }
            Button("Move Tab Up") { model.moveTab(window.id, by: -1) }
            Button("Move Tab Down") { model.moveTab(window.id, by: 1) }
        }
        .accessibilityAction(named: "Close Tab") { model.closeTab(window) }
        .accessibilityAction(named: "Move Tab Up") { model.moveTab(window.id, by: -1) }
        .accessibilityAction(named: "Move Tab Down") { model.moveTab(window.id, by: 1) }
        .help([info?.shortDir, index < 9 ? "⌘\(index + 1)" : nil].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: "\n"))
    }
}
