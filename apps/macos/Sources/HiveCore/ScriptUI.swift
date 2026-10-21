import Foundation
import HiveKit

/// A badge a script (or the CLI) put on a pane: a state and a short message.
public struct ScriptBadge: Equatable, Sendable, Identifiable {
    public var block: String
    public var app: String
    public var id: String
    public var state: String
    public var msg: String
    public var title: String
    public var progress: Int?

    public init(_ j: JSON) {
        block = j["block_id"].string ?? ""
        app = j["app"].string ?? ""
        id = j["id"].string ?? ""
        state = j["state"].string ?? "idle"
        msg = j["msg"].string ?? ""
        title = j["title"].string ?? ""
        progress = j["progress"].int
    }

    var key: String { app + "/" + id }

    /// What it says: its message, else its title, else its name.
    public var text: String { !msg.isEmpty ? msg : !title.isEmpty ? title : id }

    /// Badges that need you sort first.
    var rank: Int {
        switch state {
        case "error", "blocked": return 0
        case "working": return 1
        case "done": return 2
        default: return 3
        }
    }
}

/// Every pane's badges on one host, from status.list and status_changed.
public struct BadgeBook: Equatable, Sendable {
    private var byBlock: [String: [String: ScriptBadge]] = [:]

    public init() {}

    /// Replaces everything with a status.list answer.
    public mutating func load(_ list: JSON) {
        byBlock = [:]
        for b in list["badges"].array { apply(b) }
    }

    /// Applies one badge from status_changed; state "clear" removes it.
    public mutating func apply(_ j: JSON) {
        let b = ScriptBadge(j)
        guard !b.block.isEmpty else { return }
        if b.state == "clear" {
            byBlock[b.block]?[b.key] = nil
            if byBlock[b.block]?.isEmpty == true { byBlock[b.block] = nil }
        } else {
            byBlock[b.block, default: [:]][b.key] = b
        }
    }

    public mutating func forget(block: String) { byBlock[block] = nil }

    /// A pane's badges, those that need you first, then by name.
    public func badges(for block: String) -> [ScriptBadge] {
        (byBlock[block] ?? [:]).values.sorted { ($0.rank, $0.id, $0.app) < ($1.rank, $1.id, $1.app) }
    }
}

/// A notification a script sent.
public struct ScriptNote: Equatable, Sendable {
    public struct Action: Equatable, Sendable {
        public var title: String
        public var action: String
        public var args: JSON
        public var step: String
    }

    public var id: String
    public var time: Date?
    public var title: String
    public var body: String
    public var level: String
    public var block: String?
    public var session: String?
    public var source: String
    public var actions: [Action]
    public var acked: Bool

    public init(_ j: JSON) {
        id = j["notification_id"].string ?? ""
        time = j["time"].string.flatMap { ISO8601DateFormatter.hiveNote.date(from: $0) ?? ISO8601DateFormatter().date(from: $0) }
        title = j["title"].string ?? ""
        body = j["body"].string ?? ""
        level = j["level"].string ?? "info"
        block = j["block_id"].string
        session = j["session_id"].string
        source = j["source"].string ?? ""
        actions = j["actions"].array.map {
            Action(title: $0["title"].string ?? "", action: $0["action"].string ?? "", args: $0["args"], step: $0["step"].string ?? "")
        }
        acked = j["acked"].bool ?? false
    }

    /// The body, then what each button does: a notification's buttons only
    /// show their titles, and you should know what pressing one sends.
    public var bodyWithSteps: String {
        let steps = actions.filter { !$0.step.isEmpty }.map { "\($0.title): \($0.step)" }
        return ([body].filter { !$0.isEmpty } + steps).joined(separator: "\n")
    }
}

/// What to show of the notifications a server kept while the app was
/// closed: the newest few, and how many more there were.
public struct NoteReplay: Equatable, Sendable {
    public let show: [ScriptNote]
    public let more: Int
    /// The newest one's time, to ask from next time.
    public let latest: Date?

    public init(_ list: JSON, seenUntil: Date?, alreadyShown: Set<String>, max: Int = 3) {
        let fresh = list["notifications"].array.map(ScriptNote.init).filter { n in
            !n.acked && !alreadyShown.contains(n.id) && (seenUntil == nil || (n.time ?? .distantPast) > seenUntil!)
        }
        show = Array(fresh.suffix(max))
        more = fresh.count - show.count
        latest = fresh.compactMap(\.time).max()
    }
}

extension ISO8601DateFormatter {
    /// The server's times, with fractions of a second.
    nonisolated(unsafe) public static let hiveNote: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()
}
