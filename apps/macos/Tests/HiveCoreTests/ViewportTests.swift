import Testing
@testable import HiveCore

@Suite struct ViewportTests {
    /// A pane sized by another window can be taller than this one. It shows
    /// from the top until the cursor would fall off, then keeps the
    /// cursor's row as the last one shown.
    @Test func cursorStaysInView() {
        #expect(Viewport.rowOffset(rows: 40, visible: 25, cursorRow: 10) == 0)
        #expect(Viewport.rowOffset(rows: 40, visible: 25, cursorRow: 24) == 0)
        #expect(Viewport.rowOffset(rows: 40, visible: 25, cursorRow: 25) == 1)
        #expect(Viewport.rowOffset(rows: 40, visible: 25, cursorRow: 39) == 15)
    }

    /// A pane that fits never moves, and a cursor out of range can't push it
    /// past the last row.
    @Test func fittingPanesDontMove() {
        #expect(Viewport.rowOffset(rows: 20, visible: 25, cursorRow: 19) == 0)
        #expect(Viewport.rowOffset(rows: 25, visible: 25, cursorRow: 24) == 0)
        #expect(Viewport.rowOffset(rows: 40, visible: 25, cursorRow: 90) == 15)
        #expect(Viewport.rowOffset(rows: 40, visible: 0, cursorRow: 5) == 0)
    }
}
