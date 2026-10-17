import CGhosttyVT
import Testing
@testable import HiveCore

/// The Mac text-editing keys terminals send as readline bytes: Ghostty's
/// built-in ⌘ bindings, plus the ⌥ word jumps and ⇧↩ from the maintainer's Ghostty
/// config.
@Suite struct MacBindingsTests {
    @Test func commandArrowsAndBackspace() {
        #expect(MacBindings.text(key: Key(GHOSTTY_KEY_ARROW_LEFT), mods: .command) == "\u{01}")   // start of line
        #expect(MacBindings.text(key: Key(GHOSTTY_KEY_ARROW_RIGHT), mods: .command) == "\u{05}")  // end of line
        #expect(MacBindings.text(key: Key(GHOSTTY_KEY_BACKSPACE), mods: .command) == "\u{15}")    // delete to start
    }

    @Test func optionArrowsJumpWords() {
        #expect(MacBindings.text(key: Key(GHOSTTY_KEY_ARROW_LEFT), mods: .alt) == "\u{1b}b")
        #expect(MacBindings.text(key: Key(GHOSTTY_KEY_ARROW_RIGHT), mods: .alt) == "\u{1b}f")
    }

    @Test func shiftEnterIsANewline() {
        #expect(MacBindings.text(key: Key(GHOSTTY_KEY_ENTER), mods: .shift) == "\n")
    }

    /// Other combinations keep their normal encoding (and the menu's ⌥⌘ and
    /// ⌃⌘ arrows keep moving focus and borders).
    @Test func othersAreLeftAlone() {
        #expect(MacBindings.text(key: Key(GHOSTTY_KEY_ARROW_LEFT), mods: []) == nil)
        #expect(MacBindings.text(key: Key(GHOSTTY_KEY_ARROW_LEFT), mods: [.command, .alt]) == nil)
        #expect(MacBindings.text(key: Key(GHOSTTY_KEY_ARROW_LEFT), mods: [.command, .ctrl]) == nil)
        #expect(MacBindings.text(key: Key(GHOSTTY_KEY_ARROW_UP), mods: .command) == nil)
        #expect(MacBindings.text(key: Key(GHOSTTY_KEY_ENTER), mods: []) == nil)
    }
}
