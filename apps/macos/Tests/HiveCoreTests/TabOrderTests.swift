import Testing
@testable import HiveCore

/// Pinned tabs first, on the Mac as on the phone; ⌘1–9 and next/previous
/// follow the order shown.
@Suite struct TabOrderTests {
    @Test func pinnedFirstKeepingOrder() {
        #expect(TabOrder.shown(pinned: [false, true, false, true]) == [1, 3, 0, 2])
    }

    @Test func noPins() {
        #expect(TabOrder.shown(pinned: [false, false, false]) == [0, 1, 2])
        #expect(TabOrder.shown(pinned: []) == [])
    }

    @Test func stepWraps() {
        #expect(TabOrder.step(from: 2, by: 1, count: 3) == 0)
        #expect(TabOrder.step(from: 0, by: -1, count: 3) == 2)
        #expect(TabOrder.step(from: 1, by: 1, count: 3) == 2)
    }
}
