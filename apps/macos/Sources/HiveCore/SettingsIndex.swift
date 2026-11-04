import Foundation

/// Every row of the Settings window: where it lives, its label, and the one
/// sentence that says what it does. Rows show the label; the sentence shows
/// under a row only when the label can't carry it alone (`always`), and in
/// search results with the row's place.
public enum SettingsIndex {
    public enum Page: String, CaseIterable, Sendable {
        case server, appearance, terminal, panes, notifications, agents, hosts, shortcuts, advanced

        public var title: String {
            switch self {
            case .server: return "Server"
            case .appearance: return "Appearance"
            case .terminal: return "Terminal"
            case .panes: return "Panes"
            case .notifications: return "Notifications"
            case .agents: return "Agents and Scripts"
            case .hosts: return "Hosts"
            case .shortcuts: return "Shortcuts"
            case .advanced: return "Advanced"
            }
        }

        /// SF Symbol for its tile in the sidebar.
        public var symbol: String {
            switch self {
            case .server: return "hexagon.fill"
            case .appearance: return "circle.lefthalf.filled"
            case .terminal: return "apple.terminal"
            case .panes: return "rectangle.split.2x1"
            case .notifications: return "bell"
            case .agents: return "sparkles"
            case .hosts: return "globe"
            case .shortcuts: return "command"
            case .advanced: return "wrench.adjustable"
            }
        }
    }

    public struct Entry: Equatable, Sendable, Identifiable {
        public let id: String
        public let page: Page
        public let section: String
        public let label: String
        public let help: String
        /// Show the sentence under the row, not only in search.
        public let always: Bool
    }

    static func e(_ id: String, _ page: Page, _ section: String, _ label: String, _ help: String, always: Bool = false) -> Entry {
        Entry(id: id, page: page, section: section, label: label, help: help, always: always)
    }

    public static let entries: [Entry] = [
        e("tailnet", .server, "Reach", "Reachable on your tailnet", "Lets your phone and your other Macs connect over Tailscale. Takes effect when the server starts again.", always: true),
        e("tailscale", .server, "Reach", "Built-in Tailscale", "Joins your tailnet as the server’s own device, so this Mac needs no Tailscale app. The first time, sign in here.", always: true),
        e("resume-agents", .server, "Sessions", "Resume agents after a restart", "Starts each agent again in its own conversation when the server comes back."),
        e("scrollback-limit", .server, "Sessions", "Scrollback", "History kept for each terminal. Lowering it drops the oldest lines."),
        e("theme", .appearance, "Theme", "Theme", "System follows macOS; Day and Night stay as they are."),
        e("font-family", .appearance, "Terminal text", "Font", "JetBrains Mono comes with hive. Any monospaced font on this Mac works."),
        e("font-size", .appearance, "Terminal text", "Font size", "Also ⌘+ and ⌘−, for every pane at once."),
        e("motion", .appearance, "Motion", "Animation", "How tabs move. Snappy settles quickly, Natural springs with a slight overshoot, Off moves them at once. Reduce Motion in macOS always moves them at once."),
        e("cursor-style-blink", .terminal, "Cursor", "Blinking cursor", "Programs can still ask for a steady cursor."),
        e("bell", .terminal, "Input and output", "Bell", "Flash the pane, play a sound, both, or nothing. The Dock bounces once while hive is behind."),
        e("confirm-paste", .terminal, "Input and output", "Ask before pasting several lines", "A program that didn’t ask for a paste may run each line as a command.", always: true),
        e("dim-panes", .panes, "Focus", "Other panes", "How much the panes you’re not typing in step back: a little, more, or not at all."),
        e("focus-follows-pointer", .panes, "Focus", "Focus follows the pointer", "Focuses a pane when the pointer moves over it, without a click."),
        e("balance-splits", .panes, "Splits", "Balance splits", "Gives every pane in a tab the same room when one opens or closes, replacing sizes you dragged."),
        e("copy-on-select", .terminal, "Input and output", "Copy on select", "Copies text as soon as you select it with the mouse. ⌘C still works."),
        e("confirm-close", .panes, "Closing", "Ask before closing", "Closing a tab, or a tab’s last pane, ends the programs in it. Ask when something other than the shell runs, always, or never.", always: true),
        e("notify-blocked", .notifications, "By state", "Needs you", "When an agent asks you something or waits for permission. Its sound plays with the notification."),
        e("notify-done", .notifications, "By state", "Done", "When an agent finishes what you asked. Its sound plays with the notification."),
        e("notify-stopped", .notifications, "By state", "Stopped", "When an agent stops and won’t go on by itself, such as at a usage limit. Its sound plays with the notification."),
        e("dock-badge", .notifications, "Dock", "Badge the Dock icon", "Counts the agents that need you on hive’s icon in the Dock."),
        e("notify", .notifications, "Agents", "Notify me", "When an agent needs you or is done: while hive is in the background, always, or never. The tab is marked either way.", always: true),
        e("agents-claude", .agents, "Coding agents", "Claude Code", "Adds hive’s hooks to Claude Code, so its panes show what it’s doing and when it needs you.", always: true),
        e("scripts", .agents, "Scripts", "Run scripts", "Starts the scripts in your scripts folder with the server."),
        e("script-remote-run", .agents, "Scripts", "Let other machines run scripts", "Lets callers on your tailnet start a script on this server."),
        e("share-hosts", .hosts, "Hosts", "Share hosts with your phone", "Shows a code your phone scans to add this Mac and your other hosts."),
        e("add-host", .hosts, "Hosts", "Add a host", "Connects to a hive server on another machine, by its address."),
        e("key-hints", .shortcuts, "Keyboard", "Show key sequence hints", "After a sequence’s first key, lists the keys that can follow and what each does; in a mode, its keys."),
        e("shortcuts", .shortcuts, "Shortcuts", "Every shortcut", "Lists what each key does. Change one with keybind in the settings file."),
        e("config-file", .advanced, "Settings file", "Settings file", "Everything here is saved in this file, and editing the file updates this window."),
    ]

    public static func entry(_ id: String) -> Entry {
        entries.first { $0.id == id } ?? Entry(id: id, page: .advanced, section: "", label: id, help: "", always: false)
    }

    /// Rows whose label or sentence has every word of the query, labels
    /// matching first.
    public static func search(_ query: String) -> [Entry] {
        let words = query.lowercased().split(whereSeparator: \.isWhitespace).map(String.init)
        guard !words.isEmpty else { return [] }
        func has(_ s: String) -> Bool { let l = s.lowercased(); return words.allSatisfy { l.contains($0) } }
        let all = entries.filter { has($0.label + " " + $0.help + " " + $0.page.title + " " + $0.section) }
        return all.filter { has($0.label) } + all.filter { !has($0.label) }
    }
}
