import SwiftUI

/// The sidebar: sessions, and every agent across them, blocked first.
struct Sidebar: View {
    @ObservedObject var model: AppModel

    var body: some View {
        List {
            Section("Sessions") {
                ForEach(model.sessions) { s in
                    HStack(spacing: 8) {
                        Image(systemName: "rectangle.split.2x1")
                            .foregroundStyle(s.id == model.currentSession ? Color.accentColor : .secondary)
                        Text(s.label.isEmpty ? String(s.id.suffix(6)) : s.label).lineLimit(1)
                        Spacer()
                        if s.agents > 0 {
                            Text("\(s.agents)").font(.caption2).monospacedDigit()
                                .padding(.horizontal, 5).padding(.vertical, 1)
                                .background(Capsule().fill(Color.secondary.opacity(0.2)))
                        }
                    }
                    .contentShape(Rectangle())
                    .onTapGesture { model.select(session: s.id) }
                    .listRowBackground(s.id == model.currentSession ? Color.accentColor.opacity(0.15) : Color.clear)
                }
            }
            Section("Agents") {
                if model.agents.isEmpty {
                    Text("Start claude inside hive").font(.caption).foregroundStyle(.secondary)
                }
                ForEach(model.agents) { a in
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 6) {
                            StatusDot(state: a.state)
                            Text(a.harness).font(.callout.weight(.medium))
                            Text(a.state).font(.caption).foregroundStyle(.secondary)
                        }
                        Text([a.sessionLabel, (a.cwd as NSString).lastPathComponent].filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.caption2).foregroundStyle(.secondary).lineLimit(1)
                        if !a.message.isEmpty, a.state == "blocked" {
                            Text(a.message).font(.caption2).lineLimit(2).foregroundStyle(.orange)
                        }
                    }
                    .contentShape(Rectangle())
                    .onTapGesture { model.jump(to: a) }
                }
            }
        }
        .listStyle(.sidebar)
        .safeAreaInset(edge: .bottom) {
            HStack {
                Circle().fill(model.connected ? Color.green : Color.red).frame(width: 6, height: 6)
                Text(model.status).font(.caption2).foregroundStyle(.secondary).lineLimit(1)
                Spacer()
            }
            .padding(8)
        }
    }
}

struct StatusDot: View {
    let state: String
    var color: Color {
        switch state {
        case "blocked": return .orange
        case "working": return .green
        case "done": return .blue
        default: return .secondary
        }
    }
    var body: some View {
        Circle().fill(color).frame(width: 7, height: 7)
            .overlay(Circle().stroke(color.opacity(0.4), lineWidth: state == "working" ? 3 : 0))
    }
}

/// Tabs: the session's windows.
struct TabBar: View {
    @ObservedObject var model: AppModel

    var body: some View {
        HStack(spacing: 2) {
            ForEach(Array(model.windows.enumerated()), id: \.element.id) { i, w in
                let active = w.id == (model.active?.id)
                HStack(spacing: 6) {
                    Text("\(i + 1)").font(.caption2.monospacedDigit()).foregroundStyle(.secondary)
                    Text(title(w)).lineLimit(1)
                    if w.zoomed { Image(systemName: "arrow.up.left.and.arrow.down.right").font(.caption2) }
                }
                .font(.callout)
                .padding(.horizontal, 12).padding(.vertical, 5)
                .background(RoundedRectangle(cornerRadius: 6).fill(active ? Color.white.opacity(0.10) : Color.clear))
                .contentShape(Rectangle())
                .onTapGesture { model.selectWindow(index: i) }
            }
            Button { model.newTab() } label: { Image(systemName: "plus") }
                .buttonStyle(.borderless).padding(.leading, 4)
            Spacer()
            Text(model.sessionLabel).font(.caption).foregroundStyle(.secondary).padding(.trailing, 10)
        }
        .padding(.horizontal, 8).padding(.vertical, 4)
        .background(Color(red: 0x1d / 255, green: 0x1f / 255, blue: 0x21 / 255))
    }

    func title(_ w: WindowInfo) -> String {
        if !w.label.isEmpty { return w.label }
        if let f = w.focused, let b = model.blocks[f] {
            if !b.label.isEmpty { return b.label }
            if let fg = b.foreground, !["zsh", "bash", "fish", "sh"].contains(fg) { return fg }
            if !b.title.isEmpty { return b.title }
        }
        return "shell"
    }
}
