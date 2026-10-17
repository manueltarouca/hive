import Testing
@testable import HiveCore

/// What a key press sends while an input method composes, as Ghostty does
/// (SurfaceView_AppKit.keyDown).
@Suite struct CompositionTests {
    func decide(_ hadMarked: Bool, _ markedNow: Bool, _ committed: [String], _ key: Key = .unidentified,
                _ mods: KeyMods = []) -> Composition.Outcome {
        Composition.decide(hadMarked: hadMarked, markedNow: markedNow, committed: committed, key: key, mods: mods)
    }

    @Test func plainTypingSendsTheKeyWithItsText() {
        #expect(decide(false, false, ["a"]) == .init(keyTexts: ["a"]))
        #expect(decide(false, false, []) == .init(plainKey: true))
    }

    /// ~ on a Portuguese layout: the dead key composes, the next key commits.
    @Test func deadKeyComposesThenCommitsAsText() {
        #expect(decide(false, true, []) == .init())
        #expect(decide(true, false, ["~"]) == .init(texts: ["~"]))
        #expect(decide(true, false, ["~", "h"]) == .init(texts: ["~", "h"]))
    }

    /// Korean: one key commits 한 and starts composing the next syllable.
    @Test func commitAndNewCompositionInOneKey() {
        #expect(decide(true, true, ["한"]) == .init(texts: ["한"]))
    }

    /// An arrow that ends a composition still moves, after the text.
    @Test func arrowsAfterACommitAreReplayed() {
        #expect(decide(true, false, ["~"], .arrowRight) == .init(texts: ["~"], plainKey: true))
        #expect(decide(true, false, ["~"], .arrowLeft) == .init(texts: ["~"]))
        #expect(decide(true, false, ["~"], .arrowLeft, .shift) == .init(texts: ["~"], plainKey: true))
        #expect(decide(true, false, ["~"], .enter) == .init(texts: ["~"]))
    }

    /// Cancelling a composition (Backspace, ctrl+h) sends nothing, and a
    /// lone control character the input method commits while composing is
    /// dropped without losing the text around it.
    @Test func controlInputWhileComposingIsDropped() {
        #expect(decide(true, false, []) == .init())
        #expect(decide(true, false, ["\u{8}", "~"]) == .init(texts: ["~"]))
        #expect(decide(true, true, ["\u{7F}"]) == .init(texts: ["\u{7F}"]))
    }
}
