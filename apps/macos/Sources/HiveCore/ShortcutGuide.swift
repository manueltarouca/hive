/// Shortcuts as macOS draws them: ⌃⌥⇧⌘, then the key.
public enum ShortcutText {
    static let keyNames: [String: String] = [
        "\u{F702}": "←", "\u{F703}": "→", "\u{F700}": "↑", "\u{F701}": "↓",
        "\r": "↩", "\u{8}": "⌫", "\u{7F}": "⌫", "\t": "⇥", "\u{1B}": "⎋", " ": "Space",
    ]

    public static func display(key: String, control: Bool, option: Bool, shift: Bool, command: Bool) -> String {
        (control ? "⌃" : "") + (option ? "⌥" : "") + (shift ? "⇧" : "") + (command ? "⌘" : "")
            + (keyNames[key] ?? key.uppercased())
    }
}

/// One line of the shortcuts window.
public struct ShortcutRow: Equatable, Sendable {
    public var title: String
    public var keys: String
    /// For editing keys: the key and modifiers, to check against MacBindings.
    public var key: Key
    public var mods: KeyMods

    public init(title: String, keys: String, key: Key = .unidentified, mods: KeyMods = []) {
        self.title = title; self.keys = keys; self.key = key; self.mods = mods
    }
}

/// Shortcuts that aren't menu items: the menu's own are read from the menu
/// bar, so the window can't drift from the real bindings.
public enum ShortcutGuide {
    /// Text-editing keys in terminals (see MacBindings).
    public static let editing: [ShortcutRow] = [
        ShortcutRow(title: "Start of line", keys: "⌘←", key: .arrowLeft, mods: .command),
        ShortcutRow(title: "End of line", keys: "⌘→", key: .arrowRight, mods: .command),
        ShortcutRow(title: "Delete to start of line", keys: "⌘⌫", key: .backspace, mods: .command),
        ShortcutRow(title: "Back a word", keys: "⌥←", key: .arrowLeft, mods: .alt),
        ShortcutRow(title: "Forward a word", keys: "⌥→", key: .arrowRight, mods: .alt),
        ShortcutRow(title: "New line without sending", keys: "⇧↩", key: .enter, mods: .shift),
    ]

    public static let mouse: [ShortcutRow] = [
        ShortcutRow(title: "Select a word / a line", keys: "Double / triple click"),
        ShortcutRow(title: "Select in hive, in programs that use the mouse", keys: "⇧ Drag"),
        ShortcutRow(title: "Scroll hive's history, in programs that use the mouse", keys: "⇧ Scroll"),
        ShortcutRow(title: "Move a tab", keys: "Drag it in the sidebar"),
    ]
}

/// A menu item's shortcut, read from the menu bar.
public struct MenuShortcut: Sendable {
    public var title: String, key: String, mods: KeyMods, function: Bool
    public init(title: String, key: String, mods: KeyMods, function: Bool = false) {
        self.title = title; self.key = key; self.mods = mods; self.function = function
    }

    /// The shortcut as the menu bar draws it, or nil if it can't be written
    /// as text: fn keys, and keys like the dictation microphone that AppKit
    /// adds and draws itself.
    public var keys: String? {
        // Letters and ASCII marks stay, including letters AppKit localizes
        // for the layout (⇧⌘Ç); emoji and other symbols it draws itself go.
        guard !function, !key.isEmpty,
              ShortcutText.keyNames[key] != nil || key.unicodeScalars.allSatisfy({
                  $0.properties.isAlphabetic || ($0.isASCII && $0.value > 0x20 && $0.value < 0x7F)
              })
        else { return nil }
        // An uppercase key equivalent means Shift, as AppKit draws it.
        let shift = mods.contains(.shift) || (key.count == 1 && key != key.lowercased())
        return ShortcutText.display(key: key, control: mods.contains(.ctrl), option: mods.contains(.alt),
                                    shift: shift, command: mods.contains(.command))
    }
}

extension ShortcutGuide {
    /// One menu's rows: ⌘1…⌘9 fold into one, and items that can't be drawn
    /// are left out.
    public static func rows(_ items: [MenuShortcut]) -> [ShortcutRow] {
        var rows: [ShortcutRow] = [], numbered = 0
        for item in items {
            guard let keys = item.keys else { continue }
            if item.key.count == 1, let d = Int(item.key), (1...9).contains(d), item.mods == .command { numbered += 1; continue }
            rows.append(ShortcutRow(title: item.title.replacingOccurrences(of: "…", with: ""), keys: keys))
        }
        if numbered > 0 { rows.append(ShortcutRow(title: "Go to tab 1–\(numbered)", keys: "⌘1 … ⌘\(numbered)")) }
        return rows
    }

    /// The column of each section. Sections keep their order and fill the
    /// columns left to right, using the shortest height at which they fit.
    /// A section is its rows plus two for the heading and gap.
    public static func columns(_ rowCounts: [Int], count: Int) -> [Int] {
        let heights = rowCounts.map { $0 + 2 }
        func fill(_ limit: Int) -> [Int]? {
            var out: [Int] = [], column = 0, used = 0
            for h in heights {
                if used + h > limit, used > 0 { column += 1; used = 0 }
                guard column < count else { return nil }
                out.append(column); used += h
            }
            return out
        }
        var limit = heights.max() ?? 0
        while true {
            if let out = fill(limit) { return out }
            limit += 1
        }
    }

    static let modifierWords: [(String, String)] = [
        ("command", "⌘"), ("cmd", "⌘"), ("shift", "⇧"), ("option", "⌥"), ("opt", "⌥"), ("alt", "⌥"),
        ("control", "⌃"), ("ctrl", "⌃"),
    ]

    /// The filter matches titles and keys; words like "cmd" or "shift"
    /// match the key symbols.
    public static func matches(_ row: ShortcutRow, _ query: String) -> Bool {
        let title = row.title.lowercased(), keys = row.keys.lowercased()
        return query.lowercased().split(separator: " ").allSatisfy { word in
            let w = String(word)
            if title.contains(w) || keys.contains(w) { return true }
            return modifierWords.contains { $0.0 == w && keys.contains($0.1) }
        }
    }
}
