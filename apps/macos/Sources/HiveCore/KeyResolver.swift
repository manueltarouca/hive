import HiveKit

/// One key with its modifiers, as the server writes it: shift+cmd+d.
public struct KeyChord: Hashable, Sendable, CustomStringConvertible {
    public var ctrl = false, alt = false, shift = false, cmd = false
    /// A character ("d", "[", "1") or a name ("enter", "left", "f5").
    public var key: String

    public init(ctrl: Bool = false, alt: Bool = false, shift: Bool = false, cmd: Bool = false, key: String) {
        self.ctrl = ctrl; self.alt = alt; self.shift = shift; self.cmd = cmd; self.key = key
    }

    /// A key or sequence in the server's spelling; nil if it isn't one.
    public static func parse(_ s: String) -> [KeyChord]? {
        var out: [KeyChord] = []
        for part in s.split(separator: ">", omittingEmptySubsequences: false) {
            var c = KeyChord(key: "")
            let bits = part.split(separator: "+", omittingEmptySubsequences: false).map(String.init)
            for (i, bit) in bits.enumerated() {
                let last = i == bits.count - 1
                switch bit {
                case "ctrl" where !last: c.ctrl = true
                case "alt" where !last: c.alt = true
                case "shift" where !last: c.shift = true
                case "cmd" where !last: c.cmd = true
                default:
                    guard last, !bit.isEmpty else { return nil }
                    c.key = bit
                }
            }
            if c.key.isEmpty { return nil }
            out.append(c)
        }
        return out.isEmpty ? nil : out
    }

    public var description: String {
        (ctrl ? "ctrl+" : "") + (alt ? "alt+" : "") + (shift ? "shift+" : "") + (cmd ? "cmd+" : "") + key
    }
}

/// Resolves key presses against a keymap: chords act, a sequence's first
/// key waits for the next, and a mode's keys apply until Escape.
public struct KeyResolver: Sendable {
    public enum Outcome: Equatable, Sendable {
        /// Not a binding: the key goes to the terminal.
        case pass
        /// The start of a sequence: wait for the next key.
        case pending
        case action(action: String, args: JSON)
        case enteredMode(String)
        case leftMode
        /// In an exclusive mode, a key it doesn't bind.
        case swallowed
    }

    struct Binding: Sendable {
        var keys: [KeyChord]
        var mode: String
        var action: String
        var args: JSON
    }

    private var bindings: [Binding] = []
    private var exclusive: Set<String> = []
    private var prefix: [KeyChord] = []
    public private(set) var mode: String?

    public init(_ keymap: JSON = .null) {
        for b in keymap["bindings"].array {
            guard let keys = KeyChord.parse(b["key"].string ?? ""), let action = b["action"].string else { continue }
            bindings.append(Binding(keys: keys, mode: b["mode"].string ?? "", action: action, args: b["args"]))
        }
        for m in keymap["modes"].array where m["exclusive"].bool == true {
            if let n = m["name"].string { exclusive.insert(n) }
        }
    }

    public var isEmpty: Bool { bindings.isEmpty }

    /// Enters a mode (client.perform client.mode), or leaves with nil.
    public mutating func enter(_ m: String?) { mode = m; prefix = [] }

    public mutating func press(_ c: KeyChord) -> Outcome {
        if let m = mode {
            if c == KeyChord(key: "escape") { mode = nil; prefix = []; return .leftMode }
            if let b = bindings.first(where: { $0.mode == m && $0.keys == [c] }) { return outcome(b) }
            if exclusive.contains(m) { return .swallowed }
        }
        let keys = prefix + [c]
        let global = bindings.filter { $0.mode.isEmpty }
        if let b = global.first(where: { $0.keys == keys }) {
            prefix = []
            return outcome(b)
        }
        if global.contains(where: { $0.keys.count > keys.count && Array($0.keys.prefix(keys.count)) == keys }) {
            prefix = keys
            return .pending
        }
        prefix = []
        return .pass
    }

    private mutating func outcome(_ b: Binding) -> Outcome {
        if b.action == "client.mode", let m = b.args["mode"].string {
            mode = m
            return .enteredMode(m)
        }
        return .action(action: b.action, args: b.args)
    }

    /// The single chord bound to an action (with these arguments), for a
    /// menu item's key; nil when it has none or only a sequence or mode key.
    public func chord(for action: String, args: JSON = .null) -> KeyChord? {
        bindings.first { $0.mode.isEmpty && $0.action == action && $0.keys.count == 1 && (args == .null || $0.args == args) }?.keys[0]
    }
}
