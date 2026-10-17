import SwiftUI

/// The agents sidebar: every agent in every session, the ones that need
/// you first, then this session's tabs. the reference app's vertical tabs, organized
/// around agents.
struct AgentSidebar: View {
    @ObservedObject var model: AppModel
    static let width: CGFloat = 248

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
                VStack(alignment: .leading, spacing: 4) {
                    HStack(spacing: 7) {
                        Image(systemName: "laptopcomputer").font(.system(size: 12))
                        Text(model.host).font(.system(size: 12.5, weight: .semibold))
                    }
                    .foregroundStyle(s.text.swiftUI)
                    .padding(.horizontal, 10).padding(.top, 4)

                    SectionTitle(text: "Agents", style: s)
                    if model.agents.isEmpty {
                        Text("Agents started in hive show up here.")
                            .font(.system(size: 11.5)).foregroundStyle(s.secondary.swiftUI)
                            .padding(.horizontal, 10).padding(.vertical, 6)
                    }
                    ForEach(model.agents) { a in
                        AgentRow(model: model, agent: a)
                    }

                    SectionTitle(text: model.sessionLabel.isEmpty ? "Tabs" : model.sessionLabel, style: s)
                    ForEach(Array(model.windows.enumerated()), id: \.element.id) { i, w in
                        TabRow(model: model, window: w, index: i)
                    }
                }
                .padding(.horizontal, 8).padding(.bottom, 12)
            }
        }
        .frame(width: AgentSidebar.width)
        .background(s.sidebar.swiftUI)
        .overlay(alignment: .trailing) { Rectangle().fill(s.border.swiftUI).frame(width: 1) }
    }
}

struct SectionTitle: View {
    let text: String
    let style: Style
    var body: some View {
        Text(text.uppercased()).font(.system(size: 10, weight: .semibold)).tracking(0.6)
            .foregroundStyle(style.secondary.swiftUI)
            .padding(.horizontal, 10).padding(.top, 14).padding(.bottom, 2)
    }
}

/// An app tile: a rounded square with a symbol, and a status hexagon on
/// its corner when the agent needs attention.
struct AppTile: View {
    let symbol: String
    let tint: Color
    let style: Style
    var badge: String? = nil
    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: 6, style: .continuous).fill(tint.opacity(0.16))
            Image(systemName: symbol).font(.system(size: 11, weight: .semibold)).foregroundStyle(tint)
        }
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

struct AgentRow: View {
    @ObservedObject var model: AppModel
    let agent: AgentInfo
    @State private var hover = false

    var body: some View {
        let s = model.style
        let selected = model.focusedBlock == agent.blockID
        let claude = agent.harness == "claude"
        HStack(alignment: .top, spacing: 10) {
            AppTile(symbol: claude ? "sparkle" : "hexagon", tint: claude ? Color(red: 0.85, green: 0.47, blue: 0.34) : s.accent.swiftUI,
                    style: s, badge: agent.state == "blocked" || agent.state == "done" ? agent.state : nil)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(agent.task).font(.system(size: 12.5, weight: .medium)).foregroundStyle(s.text.swiftUI).lineLimit(1)
                    Spacer(minLength: 4)
                    if agent.state == "blocked" { StatusBadge(state: "blocked-dot", style: s) }
                    if agent.state == "working" { StatusBadge(state: "working", style: s) }
                    Text(age).font(.system(size: 10.5)).foregroundStyle(s.secondary.swiftUI)
                }
                // Why it's waiting matters most: full width, up to two lines.
                Text(subtitle).font(.system(size: 11)).foregroundStyle(subtitleColor(s))
                    .lineLimit(agent.state == "blocked" ? 2 : 1)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(.horizontal, 8).padding(.vertical, 8)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(selected ? s.tabActive.swiftUI : (hover ? s.text.swiftUI.opacity(0.05) : .clear))
        )
        .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(selected ? s.border.swiftUI : .clear, lineWidth: 1))
        .contentShape(Rectangle())
        .onTapGesture { model.jump(to: agent) }
        .onHover { hover = $0 }
        .help("\(agent.sessionLabel) · \(agent.cwd)")
    }

    var subtitle: String {
        switch agent.state {
        case "blocked": return agent.message.isEmpty ? "Needs you" : agent.message
        case "working": return "Working · \(agent.sessionLabel)"
        case "done": return "Done · \(agent.sessionLabel)"
        default: return "Idle · \(agent.sessionLabel)"
        }
    }

    func subtitleColor(_ s: Style) -> Color {
        agent.state == "blocked" ? s.blocked.swiftUI.opacity(0.9) : s.secondary.swiftUI
    }

    var age: String {
        guard let d = agent.updatedAt else { return "" }
        let secs = -d.timeIntervalSinceNow
        switch secs {
        case ..<60: return "now"
        case ..<3600: return "\(Int(secs / 60))m"
        case ..<86400: return "\(Int(secs / 3600))h"
        default: return "\(Int(secs / 86400))d"
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
        let parts: (primary: String, secondary: String) = window.label.isEmpty ? (info?.titleParts ?? ("shell", "")) : (window.label, "")
        HStack(spacing: 10) {
            AppTile(symbol: info?.symbol ?? "apple.terminal", tint: active ? s.accent.swiftUI : s.secondary.swiftUI, style: s)
            VStack(alignment: .leading, spacing: 1) {
                Text(parts.primary).font(.system(size: 12.5, weight: active ? .medium : .regular))
                    .foregroundStyle(active ? s.text.swiftUI : s.secondary.swiftUI).lineLimit(1)
                if !parts.secondary.isEmpty {
                    Text(parts.secondary).font(.system(size: 10.5)).foregroundStyle(s.secondary.swiftUI).lineLimit(1).truncationMode(.head)
                }
            }
            Spacer(minLength: 4)
            if let st = model.status(of: window) { StatusBadge(state: st == "blocked" ? "blocked-dot" : st, style: s) }
            Text("⌘\(index + 1)").font(.system(size: 10)).foregroundStyle(s.secondary.swiftUI.opacity(hover || active ? 1 : 0))
        }
        .padding(.horizontal, 8).padding(.vertical, 6)
        .background(RoundedRectangle(cornerRadius: 10, style: .continuous)
            .fill(active ? s.tabActive.swiftUI : (hover ? s.text.swiftUI.opacity(0.05) : .clear)))
        .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(active ? s.border.swiftUI : .clear, lineWidth: 1))
        .contentShape(Rectangle())
        .onTapGesture { model.selectWindow(index: index) }
        .onHover { hover = $0 }
    }
}
