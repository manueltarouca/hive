import CoreGraphics
import Testing
@testable import HiveCore

@Suite struct CombTests {
    func centers(_ l: Comb.Layout) -> [CGPoint] {
        l.origins.map { CGPoint(x: $0.x + l.cell * 0.866 / 2, y: $0.y + l.cell / 2) }
    }
    func distance(_ a: CGPoint, _ b: CGPoint) -> CGFloat { ((a.x - b.x) * (a.x - b.x) + (a.y - b.y) * (a.y - b.y)).squareRoot() }

    /// Neighbouring cells touch edge to edge: their centres are one cell
    /// width apart, and every cell fits the tile's height.
    @Test(arguments: [2, 3])
    func cellsTouchAndFit(count: Int) {
        let l = Comb.layout(count: count, height: 24)
        let c = centers(l)
        let w = l.cell * 0.866
        for i in 0..<c.count { for j in (i + 1)..<c.count {
            #expect(abs(distance(c[i], c[j]) - w) < 0.01, "cells \(i) and \(j)")
        } }
        for o in l.origins {
            #expect(o.x >= 0 && o.y >= 0 && o.x + w <= l.width + 0.01 && o.y + l.cell <= 24.01)
        }
        #expect(l.width <= 24)
    }

    @Test func oneCellFillsTheTile() {
        #expect(Comb.layout(count: 1, height: 24) == Comb.Layout(cell: 24, origins: [.zero], width: 24 * 0.866))
        #expect(Comb.layout(count: 5, height: 24).origins.count == 3)
    }
}
