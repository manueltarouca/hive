import Foundation
import HiveKit

/// A script in a server's scripts folder, as script.list says: what it is,
/// may do, its mode and its switches.
public struct ScriptInfo: Equatable, Sendable, Identifiable {
    public var id: String { name }
    public var name: String
    public var state: String
    public var description: String
    public var permissions: [String]
    public var asks: [String]
    public var mode: String
    public var error: String
    public var path: String
    public var options: [ScriptOption]

    public init(_ j: JSON) {
        name = j["name"].string ?? ""
        state = j["state"].string ?? "failed"
        description = j["description"].string ?? ""
        permissions = j["permissions"].array.compactMap(\.string)
        asks = j["asks"].array.compactMap(\.string)
        mode = j["mode"].string ?? "act"
        error = j["error"].string ?? ""
        path = j["path"].string ?? ""
        options = j["options"].array.map(ScriptOption.init)
    }

    public static func list(_ j: JSON) -> [ScriptInfo] {
        j["scripts"].array.map(ScriptInfo.init).sorted { $0.name < $1.name }
    }

    public var on: Bool { state != "disabled" && state != "quarantined" }
    public var watching: Bool { mode == "watch" }

    /// Its state in plain words.
    public var stateText: String {
        switch state {
        case "running": return watching ? "Running, watch only" : "Running"
        case "pending": return "Waiting for your OK"
        case "disabled": return "Turned off"
        case "stopped": return "Stopped by itself"
        case "quarantined": return "Paused: it ran too long too often"
        case "failed": return error.isEmpty ? "Failed" : "Failed: " + (error.split(separator: "\n").first.map(String.init) ?? "")
        default: return state.capitalized
        }
    }

    /// Whether it needs the user: waiting for a grant, or failed.
    public var needsYou: Bool { state == "pending" || state == "failed" || state == "quarantined" }

    /// What a pending script asks for, or what a running one may do.
    public var mayDo: [String] { Self.words(state == "pending" ? asks : permissions) }

    /// One line, when it asks for both: it can read and type.
    public var warning: String? {
        let p = Set(state == "pending" ? asks : permissions)
        return p.contains("screen") && p.contains("input") ? "It can read your terminals and type into them, agents included." : nil
    }

    /// Permissions in plain words; read is every script's and goes unsaid.
    public static func words(_ perms: [String]) -> [String] { perms.filter { $0 != "read" }.map { word[$0]?.text ?? $0 } }

    /// What a permission lets a script do, and an SF Symbol for it.
    public static let word: [String: (text: String, symbol: String)] = [
        "input": ("Type into terminals, agents included", "keyboard"),
        "screen": ("Read what terminals show", "eye"),
        "ui": ("Notify you, mark panes and bring hive to the front", "bell"),
        "control": ("Arrange, open and close panes", "rectangle.split.2x1"),
        "spawn": ("Open panes running programs", "play.rectangle"),
        "config": ("Change settings", "gearshape"),
        "server": ("Manage the server", "server.rack"),
    ]

    /// The permissions to list: what it asks for while pending, else what it may do.
    public var shownPermissions: [String] { (state == "pending" ? asks : permissions).filter { $0 != "read" } }
}

/// One of a script's switches.
public struct ScriptOption: Equatable, Sendable, Identifiable {
    public var id: String { key }
    public var key: String
    public var title: String
    public var type: String
    public var value: JSON
    public var min: Double?
    public var max: Double?

    public init(_ j: JSON) {
        key = j["key"].string ?? ""
        title = j["title"].string ?? ""
        type = j["type"].string ?? "text"
        value = j["value"]
        min = j["min"].double
        max = j["max"].double
    }

    /// Its label: its title, else its key in words.
    public var label: String { title.isEmpty ? key.replacingOccurrences(of: "_", with: " ").capitalized : title }
}

/// One line of what scripts did (audit.list), newest last.
public struct ActivityEntry: Equatable, Sendable, Identifiable {
    public var id: String { "\(seq)-\(time?.timeIntervalSince1970 ?? 0)-\(method)" }
    public var time: Date?
    public var script: String
    public var principal: String
    public var method: String
    public var summary: String
    public var preview: String
    public var result: String
    public var block: String?
    public var seq: UInt64

    public init(_ j: JSON) {
        time = j["t"].string.flatMap { ISO8601DateFormatter.hiveNote.date(from: $0) ?? ISO8601DateFormatter().date(from: $0) }
        principal = j["principal"].string ?? ""
        script = j["script"].string ?? (principal.hasPrefix("script:") ? String(principal.dropFirst(7)) : "")
        method = j["method"].string ?? ""
        summary = j["summary"].string ?? ""
        preview = j["preview"].string ?? ""
        result = j["result"].string ?? ""
        block = j["block_id"].string
        seq = UInt64(j["seq"].double ?? 0)
    }

    public static func list(_ j: JSON) -> [ActivityEntry] { j["entries"].array.map(ActivityEntry.init) }

    /// Done by a person (turned off, a switch), not by the script.
    public var byPerson: Bool { !principal.hasPrefix("script:") }

    /// What happened, in a line; a switch by its title.
    public func text(options: [ScriptOption] = []) -> String {
        guard byPerson else {
            var t = summary.isEmpty ? method : summary
            if !preview.isEmpty { t += " \u{201C}\(preview)\u{201D}" }
            return t.prefix(1).uppercased() + t.dropFirst()
        }
        let fromNote = principal.hasSuffix("(notification button)") ? " from a notification" : ""
        switch method {
        case "script.grant": return "You allowed it" + fromNote
        case "script.revoke": return "You took its permissions back" + fromNote
        case "script.enable": return "You turned it on" + fromNote
        case "script.disable": return "You turned it off" + fromNote
        case "script.set_mode":
            return (summary.hasSuffix("watch") ? "You set it to watch only" : "You let it act again") + fromNote
        case "script.set_option":
            // "key = value"
            let parts = summary.components(separatedBy: " = ")
            guard parts.count == 2 else { return "You changed a switch" + fromNote }
            let label = options.first { $0.key == parts[0] }?.label ?? parts[0]
            let value = parts[1] == "true" ? "on" : parts[1] == "false" ? "off" : parts[1]
            return "You set \u{201C}\(label)\u{201D} to \(value)" + fromNote
        default:
            return "You: " + (summary.isEmpty ? method : summary) + fromNote
        }
    }

    /// The result, when it isn't plain success.
    public var problem: String? {
        switch result {
        case "ok", "": return nil
        case "watching": return "Refused: watch only"
        case "conflict": return "Not sent: it had moved on"
        case "permission_denied": return "Refused: not allowed"
        default: return "Failed: " + result.replacingOccurrences(of: "_", with: " ")
        }
    }
}

/// A short-lived mark on a pane: a script just did something there.
public struct ActedChip: Equatable, Sendable {
    public var script: String
    public var text: String
    public var refused: Bool
    public var at: Date

    /// From a script_acted event's payload.
    public init?(_ p: JSON, now: Date = Date()) {
        // Only what a script did: people's changes name no script acting.
        guard let s = p["script"].string, p["block_id"].string != nil,
              (p["by"].string ?? "script:").hasPrefix("script:") else { return nil }
        script = s
        refused = p["result"].string == "watching"
        text = refused ? "\(s) was stopped from acting" : "\(s) \(p["summary"].string ?? "acted")"
        at = now
    }

    /// The same chip saying something else (where it happened).
    public init(copying c: ActedChip, text: String) {
        script = c.script; refused = c.refused; at = c.at
        self.text = text
    }

    /// How long a chip shows.
    public static let life: TimeInterval = 10
}
