import Testing
@testable import HiveCore

/// A program's copy (OSC 52) reaches the Mac clipboard only from the pane the
/// user is working in, so a background agent can't overwrite it.
@Suite struct ClipboardPolicyTests {
    @Test func onlyTheFocusedPaneOfTheActiveApp() {
        #expect(ClipboardPolicy.accepts(text: "hi", from: "b1", focusedBlock: "b1", appActive: true))
        #expect(!ClipboardPolicy.accepts(text: "hi", from: "b2", focusedBlock: "b1", appActive: true))
        #expect(!ClipboardPolicy.accepts(text: "hi", from: "b1", focusedBlock: "b1", appActive: false))
        #expect(!ClipboardPolicy.accepts(text: "hi", from: "b1", focusedBlock: nil, appActive: true))
    }

    @Test func emptyAndOversizedAreDropped() {
        #expect(!ClipboardPolicy.accepts(text: "", from: "b1", focusedBlock: "b1", appActive: true))
        let big = String(repeating: "x", count: ClipboardPolicy.maxBytes + 1)
        #expect(!ClipboardPolicy.accepts(text: big, from: "b1", focusedBlock: "b1", appActive: true))
    }
}
