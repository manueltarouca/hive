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
    @State private var dragging: String?

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
                        TabRow(model: model, window: w, index: i)
                            .onDrag {
                                dragging = w.id
                                return NSItemProvider(object: w.id as NSString)
                            }
                            // Moves use the session's order; pins stay first wherever it lands.
                            .onDrop(of: [.text], delegate: TabDrop(model: model, target: model.windows.firstIndex { $0.id == w.id } ?? i,
                                                                   dragging: $dragging))
                    }
                }
                .padding(.horizontal, 8).padding(.top, 4).padding(.bottom, 12)
            }
            // Rows fade out under the title bar instead of being cut off.
            .mask(VStack(spacing: 0) {
                LinearGradient(colors: [.clear, .black], startPoint: .top, endPoint: .bottom).frame(height: 10)
                Rectangle()
            })
        }
        .frame(width: AgentSidebar.width)
        .background(s.sidebar.swiftUI)
        .overlay(alignment: .trailing) { Rectangle().fill(s.border.swiftUI).frame(width: 1) }
        .contextMenu { HiveContextMenu(model: model) }
    }
}

/// Dropping a dragged tab onto a row moves it to that row's place.
struct TabDrop: DropDelegate {
    let model: AppModel
    let target: Int
    @Binding var dragging: String?

    func dropUpdated(info: DropInfo) -> DropProposal? { DropProposal(operation: .move) }

    func performDrop(info: DropInfo) -> Bool {
        defer { dragging = nil }
        guard let id = dragging, let from = model.windows.firstIndex(where: { $0.id == id }), from != target,
              model.windows.indices.contains(target) else { return false }
        // Dropped among the pins, it's pinned; among the rest, unpinned.
        let w = model.windows[from], onto = model.windows[target]
        if w.pinned != onto.pinned { model.pinTab(w, onto.pinned) }
        model.moveWindow(id, to: target)
        return true
    }

    func dropExited(info: DropInfo) {}
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
                Image(systemName: "hexagon.fill").font(.system(size: 8))
                    .foregroundStyle(badge == "blocked" ? style.blocked.swiftUI : style.done.swiftUI)
                    .background(Image(systemName: "hexagon.fill").font(.system(size: 11)).foregroundStyle(style.sidebar.swiftUI))
                    .offset(x: 4, y: -4)
            }
        }
    }
}

struct TabRow: View {
    @ObservedObject var model: AppModel
    let window: WindowInfo
    let index: Int
    @State private var hover = false

    var body: some View {
        let s = model.style
        let active = window.id == model.active?.id
        let info = window.focused.flatMap { model.blocks[$0] }
        let sum = model.summary(of: window)
        let blocked = sum.state == "blocked"
        HStack(alignment: sum.detail == nil ? .center : .top, spacing: 10) {
            AppTile(program: model.program(of: window), behind: model.otherPrograms(of: window), style: s,
                    badge: blocked ? "blocked" : nil)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    if window.pinned {
                        Image(systemName: "pin.fill").font(.system(size: 8, weight: .semibold)).foregroundStyle(s.accent.swiftUI)
                            .accessibilityLabel("Pinned")
                    }
                    Text(sum.title).font(.system(size: 12.5, weight: active ? .semibold : .medium))
                        .foregroundStyle(s.text.swiftUI.opacity(active ? 1 : 0.86)).lineLimit(1)
                    Spacer(minLength: 4)
                    if blocked {
                        StatusBadge(state: "blocked", style: s)
                    } else if let st = sum.state {
                        StatusBadge(state: st, style: s)
                    }
                    if !blocked, index < 9, hover || active {
                        Text("⌘\(index + 1)").font(.system(size: 10)).foregroundStyle(s.secondary.swiftUI)
                    }
                }
                if let detail = sum.detail {
                    // Why it's waiting matters most: up to two lines.
                    Text(detail).font(.system(size: 11))
                        .foregroundStyle(blocked ? s.blocked.swiftUI.opacity(0.9) : s.secondary.swiftUI)
                        .lineLimit(blocked ? 2 : 1).truncationMode(sum.isAgent ? .tail : .head)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
        }
        .padding(.horizontal, 8).padding(.vertical, sum.detail == nil ? 7 : 8)
        .background(RoundedRectangle(cornerRadius: 10, style: .continuous)
            .fill(active ? s.tabActive.swiftUI : (hover ? s.text.swiftUI.opacity(0.05) : .clear)))
        .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(active ? s.border.swiftUI : .clear, lineWidth: 1))
        .contentShape(Rectangle())
        .onTapGesture { model.selectWindow(index: index) }
        .onHover { hover = $0 }
        .contextMenu {
            Button(window.pinned ? "Unpin Tab" : "Pin Tab") { model.pinTab(window, !window.pinned) }
        }
        .help(info?.shortDir ?? "")
    }
}
