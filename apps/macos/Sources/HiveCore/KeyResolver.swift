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

    /// In words, for VoiceOver: "Control A", "Shift Command D".
    public var spoken: String {
        ([ctrl ? "Control" : nil, alt ? "Option" : nil, shift ? "Shift" : nil, cmd ? "Command" : nil].compactMap { $0 }
            + [key.count > 1 ? key.capitalized : key.uppercased()]).joined(separator: " ")
    }

    /// As keycaps show it: ⌃A, ⇧⌘D, ↩.
    public var display: String {
        let names = ["enter": "↩", "return": "↩", "escape": "⎋", "esc": "⎋", "tab": "⇥", "space": "Space", "backspace": "⌫",
                     "delete": "⌦", "left": "←", "right": "→", "up": "↑", "down": "↓", "pageup": "⇞", "pagedown": "⇟",
                     "home": "↖", "end": "↘", "insert": "Ins"]
        return (ctrl ? "⌃" : "") + (alt ? "⌥" : "") + (shift ? "⇧" : "") + (cmd ? "⌘" : "")
            + (names[key] ?? (key.count > 1 ? key.uppercased() : ShortcutText.capital(key)))
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
        var title = ""
    }

    /// One key that can come next, and what it does.
    public struct Hint: Equatable, Sendable {
        public var keys: String
        public var title: String
        /// The keys in words, for VoiceOver.
        public var spoken = ""
        public static func == (a: Hint, b: Hint) -> Bool { a.keys == b.keys && a.title == b.title }
    }

    /// What was typed so far, in words.
    public var typedSpoken: String { prefix.map(\.spoken).joined(separator: ", ") }

    private var bindings: [Binding] = []
    private var exclusive: Set<String> = []
    private var prefix: [KeyChord] = []
    public private(set) var mode: String?

    public init(_ keymap: JSON = .null) {
        for b in keymap["bindings"].array {
            guard let keys = KeyChord.parse(b["key"].string ?? ""), let action = b["action"].string else { continue }
            bindings.append(Binding(keys: keys, mode: b["mode"].string ?? "", action: action, args: b["args"], title: b["title"].string ?? ""))
        }
        // A binding without a title takes the title another key of the same action has (the default's).
        // Keyed by action and arguments (Focus Left), then by action alone.
        var titles: [String: String] = [:]
        for b in bindings where !b.title.isEmpty {
            let k = "\(b.action)\(b.args)"
            if titles[k] == nil { titles[k] = b.title }
            if b.args == .null, titles[b.action] == nil { titles[b.action] = b.title }
        }
        for i in bindings.indices where bindings[i].title.isEmpty {
            let b = bindings[i]
            bindings[i].title = titles["\(b.action)\(b.args)"] ?? (b.args == .null ? titles[b.action] : nil) ?? ""
        }
        for m in keymap["modes"].array where m["exclusive"].bool == true {
            if let n = m["name"].string { exclusive.insert(n) }
        }
    }

    public var isEmpty: Bool { bindings.isEmpty }

    /// Forgets a half-typed sequence.
    public mutating func cancel() { prefix = [] }

    /// What was typed of a sequence so far, as keycaps (empty: none).
    public var typed: String { prefix.map(\.display).joined(separator: " ") }

    /// The keys that can come next: after a sequence's first keys, the rest
    /// of each sequence; in a mode, its keys. Empty otherwise.
    public func hints() -> [Hint] {
        func name(_ b: Binding) -> String {
            if !b.title.isEmpty { return b.title }
            if b.action == "client.mode", let m = b.args["mode"].string { return m.prefix(1).uppercased() + m.dropFirst() + " mode" }
            // An action without a title: its name, without the namespace.
            let short = b.action.split(separator: ".").last.map(String.init) ?? b.action
            return short.replacingOccurrences(of: "_", with: " ").prefix(1).uppercased() + short.replacingOccurrences(of: "_", with: " ").dropFirst()
        }
        if let m = mode, prefix.isEmpty {
            return bindings.filter { $0.mode == m && $0.keys.count == 1 }.map { Hint(keys: $0.keys[0].display, title: name($0), spoken: $0.keys[0].spoken) }
        }
        guard !prefix.isEmpty else { return [] }
        return bindings.filter { $0.mode.isEmpty && $0.keys.count > prefix.count && Array($0.keys.prefix(prefix.count)) == prefix }
            .map { b in
                let rest = b.keys.dropFirst(prefix.count)
                return Hint(keys: rest.map(\.display).joined(separator: " "), title: name(b), spoken: rest.map(\.spoken).joined(separator: ", "))
            }
    }

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
    /// typeable says whether the keyboard in use has a key for a character
    /// without Option (on a Portuguese one, [ and / need it): the first such
    /// binding wins, so the menu shows keys you can press.
    public func chord(for action: String, args: JSON = .null, typeable: (String) -> Bool = { _ in true }) -> KeyChord? {
        let all = bindings.filter { $0.mode.isEmpty && $0.action == action && $0.keys.count == 1 && (args == .null || $0.args == args) }
        return (all.first { b in b.keys[0].key.count > 1 || typeable(b.keys[0].key) } ?? all.first)?.keys[0]
    }
}
