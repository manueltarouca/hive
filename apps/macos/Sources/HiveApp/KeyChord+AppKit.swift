import AppKit
import HiveCore

extension KeyChord {
    /// The chord a key press is, in the server's spelling.
    init?(event e: NSEvent) {
        let names: [UInt16: String] = [
            36: "enter", 76: "enter", 53: "escape", 48: "tab", 49: "space", 51: "backspace", 117: "delete",
            123: "left", 124: "right", 125: "down", 126: "up", 115: "home", 119: "end", 116: "pageup", 121: "pagedown",
            122: "f1", 120: "f2", 99: "f3", 118: "f4", 96: "f5", 97: "f6", 98: "f7", 100: "f8", 101: "f9", 109: "f10", 103: "f11", 111: "f12",
        ]
        let key: String
        if let n = names[e.keyCode] {
            key = n
        } else {
            // The key itself, without Shift: shift+cmd+] is "]", not "}".
            guard let ch = e.characters(byApplyingModifiers: []) ?? e.charactersIgnoringModifiers, ch.count == 1 else { return nil }
            key = ch.lowercased()
        }
        let m = e.modifierFlags
        self.init(ctrl: m.contains(.control), alt: m.contains(.option), shift: m.contains(.shift), cmd: m.contains(.command), key: key)
    }

    /// The menu's key equivalent for it, or nil for keys a menu can't show.
    var menuKey: String? {
        let names: [String: String] = [
            "enter": "\r", "escape": "\u{1B}", "tab": "\t", "space": " ", "backspace": "\u{8}", "delete": "\u{7F}",
            "left": "\u{F702}", "right": "\u{F703}", "up": "\u{F700}", "down": "\u{F701}",
            "home": "\u{F729}", "end": "\u{F72B}", "pageup": "\u{F72C}", "pagedown": "\u{F72D}",
        ]
        if let n = names[key] { return n }
        if key.hasPrefix("f"), let i = Int(key.dropFirst()), (1...12).contains(i) {
            return String(UnicodeScalar(0xF704 + i - 1)!)
        }
        return key.count == 1 && !key.hasPrefix("[") ? key : nil
    }

    /// As macOS draws it: ⌃⌥⇧⌘ then the key.
    var display: String {
        let names = ["enter": "↩", "escape": "⎋", "tab": "⇥", "space": "Space", "backspace": "⌫", "delete": "⌦",
                     "left": "←", "right": "→", "up": "↑", "down": "↓"]
        return (ctrl ? "⌃" : "") + (alt ? "⌥" : "") + (shift ? "⇧" : "") + (cmd ? "⌘" : "") + (names[key] ?? key.uppercased())
    }
}
