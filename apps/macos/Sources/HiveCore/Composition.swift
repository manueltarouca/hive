/// What a key press sends while an input method composes (dead keys like ~
/// on a Portuguese layout, Korean, Japanese), following Ghostty's
/// SurfaceView_AppKit.keyDown.
public enum Composition {
    public struct Outcome: Equatable, Sendable {
        /// Text the input method committed, sent as text with no key.
        public var texts: [String] = []
        /// Text sent with the key itself: ordinary typing.
        public var keyTexts: [String] = []
        /// The key sent alone, with no text.
        public var plainKey = false

        public init(texts: [String] = [], keyTexts: [String] = [], plainKey: Bool = false) {
            self.texts = texts; self.keyTexts = keyTexts; self.plainKey = plainKey
        }
    }

    /// hadMarked: composing before this key; markedNow: composing after it;
    /// committed: what the input method inserted while handling it.
    public static func decide(hadMarked: Bool, markedNow: Bool, committed: [String], key: Key, mods: KeyMods) -> Outcome {
        let composing = hadMarked || markedNow
        // A lone control character committed while composing belongs to the
        // input method (Backspace or ctrl+h cancelling), not the program.
        let kept = committed.filter { !(composing && isControl($0)) }
        if hadMarked, !committed.isEmpty {
            // The composition ended (or a new one began): send what it made,
            // then keys that still mean something after it, like arrows.
            return Outcome(texts: kept, plainKey: replays(key, mods))
        }
        if !committed.isEmpty { return Outcome(keyTexts: kept) }
        if composing { return Outcome() }
        return Outcome(plainKey: true)
    }

    static func isControl(_ s: String) -> Bool {
        s.unicodeScalars.count == 1 && s.unicodeScalars.first!.value < 0x20
    }

    /// Ghostty replays ↑ ↓ → after a commit, and ← only with a modifier: a
    /// Korean input method already leaves the caret in place for plain ←.
    static func replays(_ key: Key, _ mods: KeyMods) -> Bool {
        switch key {
        case .arrowUp, .arrowDown, .arrowRight: return true
        case .arrowLeft: return !mods.subtracting(.capsLock).isEmpty
        default: return false
        }
    }
}
