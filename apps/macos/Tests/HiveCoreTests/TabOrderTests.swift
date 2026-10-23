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

    /// Applies a drop as the app does (pin state, then one move in the session) and returns the
    /// shown order, as the tabs' original session indices.
    private func applied(_ pinned: [Bool], drag id: Int, to: Int) -> [Int] {
        let d = TabOrder.drop(id, to: to, pinned: pinned)
        var session = Array(pinned.indices)
        session.remove(at: id)
        session.insert(id, at: d.sessionIndex)
        var p = pinned
        p[id] = d.pinned
        return TabOrder.shown(pinned: session.map { p[$0] }).map { session[$0] }
    }

    /// Wherever it's dropped, it shows exactly there: within a group, and across the pins' edge.
    @Test func aDropLandsWhereItWasDropped() {
        let cases: [[Bool]] = [[false, false, true], [true, false, true, false], [false, false, false, false], [true, true, false]]
        for pinned in cases {
            let shown = TabOrder.shown(pinned: pinned)
            for from in shown.indices {
                for to in shown.indices {
                    var want = shown
                    let id = want.remove(at: from)
                    want.insert(id, at: to)
                    #expect(applied(pinned, drag: id, to: to) == want, "pinned \(pinned): \(from) to \(to)")
                }
            }
        }
    }

    /// Dropped among the pins it's pinned; among the rest, not.
    @Test func itTakesThePinOfWhereItLands() {
        // Session [U0, U1, P2] shows as [P2, U0, U1].
        #expect(TabOrder.drop(1, to: 0, pinned: [false, false, true]).pinned)
        #expect(!TabOrder.drop(2, to: 2, pinned: [false, false, true]).pinned)
    }
}
