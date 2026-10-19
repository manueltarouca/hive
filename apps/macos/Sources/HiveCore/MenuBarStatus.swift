/// The menu bar's quick status: what the icon shows, the line under "hive",
/// and the agents, those that need you first. Agents of a host that can't
/// be reached aren't shown: what was last known of them is out of date.
public struct MenuBarStatus: Equatable, Sendable {
    public struct Host: Equatable, Sendable {
        public var id: String
        public var name: String
        public var connected: Bool
        public var local = false
        /// Saved, not connected on purpose: not away.
        public var idle = false
        /// Why it isn't connected, as the title bar says it.
        public var problem: String?
        public init(id: String, name: String, connected: Bool, local: Bool = false, idle: Bool = false, problem: String? = nil) {
            self.id = id; self.name = name; self.connected = connected; self.local = local; self.idle = idle; self.problem = problem
        }
    }

    public struct Agent: Equatable, Sendable {
        public var id: String
        public var state: String
        public var doneSeen: Bool
        public var host: String
        public init(id: String, state: String, doneSeen: Bool, host: String) {
            self.id = id; self.state = state; self.doneSeen = doneSeen; self.host = host
        }
    }

    public enum Icon: Equatable, Sendable {
        case quiet, working
        /// Agents waiting on you: blocked, or done and not looked at.
        case needsYou(Int)
        /// This Mac's server can't be reached.
        case away
    }

    public let icon: Icon
    public let subtitle: String
    public let needsYou: [Agent]
    public let working: [Agent]
    /// Idle agents, counted only.
    public let quiet: Int

    public init(hosts: [Host], agents: [Agent]) {
        let reachable = Set(hosts.filter(\.connected).map(\.id))
        let live = agents.filter { reachable.contains($0.host) }
        needsYou = live.filter { $0.state == "blocked" } + live.filter { $0.state == "done" && !$0.doneSeen }
        working = live.filter { $0.state == "working" }
        quiet = live.filter { $0.state == "idle" }.count

        let localHost = hosts.first { $0.local && !$0.connected }
        let localAway = localHost != nil
        let away = hosts.filter { !$0.connected && !$0.idle }.count
        let up = hosts.filter(\.connected).count
        if localAway {
            subtitle = localHost?.problem ?? "Reconnecting…"
        } else if away > 0 {
            subtitle = "\(away) of \(hosts.count) hosts away"
        } else if up > 1 {
            subtitle = "Connected to \(up) hosts"
        } else {
            subtitle = "Connected"
        }

        if localAway {
            icon = .away
        } else if !needsYou.isEmpty {
            icon = .needsYou(needsYou.count)
        } else if !working.isEmpty {
            icon = .working
        } else {
            icon = .quiet
        }
    }

    /// One line of at most max characters for a menu row: a menu can't fade
    /// a long title, so it's cut at a word with an ellipsis, as macOS does.
    public static func clip(_ text: String, max: Int) -> String {
        let one = text.split(whereSeparator: \.isWhitespace).joined(separator: " ")
        guard one.count > max else { return one }
        var cut = String(one.prefix(max - 1))
        // Mid-word: back to the last whole word, if that keeps most of it.
        let next = one[one.index(one.startIndex, offsetBy: max - 1)]
        if next != " ", let space = cut.lastIndex(of: " "), cut.distance(from: cut.startIndex, to: space) > max / 2 {
            cut = String(cut[..<space])
        }
        return cut.trimmingCharacters(in: .whitespaces) + "…"
    }
}
