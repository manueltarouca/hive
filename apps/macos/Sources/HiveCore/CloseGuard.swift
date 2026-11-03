import Foundation

/// Whether closing a tab, or a tab's last pane, asks first (confirm-close).
public enum CloseGuard {
    public enum Policy: String, Sendable, CaseIterable {
        /// Ask when a program other than the shell runs in it.
        case running
        case always, never
    }

    /// One pane being closed: its foreground program and whether it ended.
    public struct Pane: Sendable {
        public var foreground: String?
        public var exited: Bool
        /// The name people know it by ("Claude Code"), for the question.
        public var name: String
        public init(foreground: String?, exited: Bool, name: String) {
            self.foreground = foreground; self.exited = exited; self.name = name
        }

        /// A program is running that closing would end: not a shell at its
        /// prompt, not a pane whose program already exited.
        public var running: Bool {
            guard !exited, let f = foreground, !f.isEmpty else { return false }
            return !Programs.atShell(f)
        }
    }

    /// What to ask before closing these panes, or nil to close at once.
    public static func question(closing panes: [Pane], what: String, policy: Policy) -> (title: String, detail: String)? {
        let running = panes.filter(\.running)
        switch policy {
        case .never: return nil
        case .running where running.isEmpty: return nil
        default: break
        }
        let title = "Close “\(what)”?"
        let names = Array(NSOrderedSet(array: running.map(\.name))) as? [String] ?? []
        let detail: String
        switch names.count {
        case 0: detail = panes.count > 1 ? "It has \(panes.count) panes." : "Nothing else is running in it."
        case 1: detail = "\(names[0]) is still running in it. Closing ends it."
        default: detail = "\(names.dropLast().joined(separator: ", ")) and \(names.last!) are still running in it. Closing ends them."
        }
        return (title, detail)
    }
}
