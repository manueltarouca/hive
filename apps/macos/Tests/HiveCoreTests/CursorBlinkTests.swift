import Foundation
import Testing
@testable import HiveCore

/// Ghostty's blink: 600 ms shown, 600 ms hidden, shown again at once after
/// typing or output.
@Suite struct CursorBlinkTests {
    @Test func alternatesEveryInterval() {
        let b = CursorBlink(at: 100)
        #expect(b.visible(at: 100))
        #expect(b.visible(at: 100.59))
        #expect(!b.visible(at: 100.6))
        #expect(!b.visible(at: 101.19))
        #expect(b.visible(at: 101.2))
        #expect(b.nextChange(after: 100.1) == 100.6)
        #expect(b.nextChange(after: 100.6) == 101.2)
    }

    @Test func resetShowsTheCursorAndRestartsThePhase() {
        var b = CursorBlink(at: 0)
        #expect(!b.visible(at: 0.7))
        b.reset(at: 0.7)
        #expect(b.visible(at: 0.7))
        #expect(b.visible(at: 1.2))
        #expect(!b.visible(at: 1.3))
    }

    /// Time going backwards (a clock change) shows the cursor.
    @Test func earlierTimesShowTheCursor() {
        let b = CursorBlink(at: 10)
        #expect(b.visible(at: 9))
    }
}
