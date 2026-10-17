import Foundation
import Testing
@testable import HiveCore

/// Frames at most as often as a display shows them; the first after a
/// quiet spell at once.
@Suite struct FramePacerTests {
    @Test func firstFrameIsImmediate() {
        #expect(FramePacer().delay(at: 100) == 0)
    }

    @Test func nextFrameWaitsForTheInterval() {
        var p = FramePacer()
        p.built(at: 10)
        #expect(abs(p.delay(at: 10.002) - (FramePacer.interval - 0.002)) < 1e-9)
        #expect(p.delay(at: 10 + FramePacer.interval) == 0)
        #expect(p.delay(at: 11) == 0)  // quiet: at once
    }

    /// 120 Hz, ProMotion's rate: no display shows more.
    @Test func intervalIsADisplayFrame() {
        #expect(abs(FramePacer.interval - 1.0 / 120) < 1e-12)
    }
}
