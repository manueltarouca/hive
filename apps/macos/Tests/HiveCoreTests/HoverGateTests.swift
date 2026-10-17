import CoreGraphics
import Testing
@testable import HiveCore

/// In a list driven by both keys and the pointer, hover selects only when
/// the pointer really moved: rows scrolling under a resting pointer (arrow
/// keys) must not take the selection back.
@Suite struct HoverGateTests {
    @Test func onlyRealMovementSelects() {
        var gate = HoverGate()
        let first = gate.accept(pointer: CGPoint(x: 10, y: 10))      // first hover
        let still = gate.accept(pointer: CGPoint(x: 10, y: 10))      // rows moved, pointer didn't
        let jitter = gate.accept(pointer: CGPoint(x: 10.4, y: 10.2)) // jitter under a point
        let moved = gate.accept(pointer: CGPoint(x: 10, y: 40))      // moved to another row
        #expect(first && !still && !jitter && moved)
    }
}
