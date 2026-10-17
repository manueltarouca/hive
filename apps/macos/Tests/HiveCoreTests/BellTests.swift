import Foundation
import Testing
@testable import HiveCore

/// What a terminal bell does, by the `bell` setting, rate-limited so a
/// program ringing in a loop can't flood the screen or the Dock.
@Suite struct BellTests {
    let t0 = Date(timeIntervalSince1970: 1000)

    @Test func settingDecidesTheEffects() {
        var p = BellPolicy()
        let r1 = p.ring(block: "a", setting: "flash", appActive: true, now: t0)
        #expect(r1 == [.flash])
        p = BellPolicy()
        let r2 = p.ring(block: "a", setting: "sound", appActive: true, now: t0)
        #expect(r2 == [.sound])
        p = BellPolicy()
        let r3 = p.ring(block: "a", setting: "flash+sound", appActive: true, now: t0)
        #expect(r3 == [.flash, .sound])
        p = BellPolicy()
        let r4 = p.ring(block: "a", setting: "none", appActive: false, now: t0)
        #expect(r4 == [])
    }

    /// In the background the Dock bounces once, not once per pane.
    @Test func dockBouncesOnceAcrossPanes() {
        var p = BellPolicy()
        let r5 = p.ring(block: "a", setting: "flash", appActive: false, now: t0)
        #expect(r5.contains(.bounce))
        let r6 = p.ring(block: "b", setting: "flash", appActive: false, now: t0 + 2)
        #expect(!r6.contains(.bounce))
        let r7 = p.ring(block: "c", setting: "flash", appActive: false, now: t0 + 11)
        #expect(r7.contains(.bounce))
    }

    /// One pane rings at most once a second.
    @Test func eachPaneIsRateLimited() {
        var p = BellPolicy()
        let r8 = p.ring(block: "a", setting: "flash", appActive: true, now: t0)
        #expect(!r8.isEmpty)
        let r9 = p.ring(block: "a", setting: "flash", appActive: true, now: t0 + 0.4)
        #expect(r9.isEmpty)
        let r10 = p.ring(block: "b", setting: "flash", appActive: true, now: t0 + 0.4)
        #expect(!r10.isEmpty)
        let r11 = p.ring(block: "a", setting: "flash", appActive: true, now: t0 + 1.1)
        #expect(!r11.isEmpty)
    }
}

@Suite struct BellPruneTests {
    /// Closed panes are forgotten: the policy's memory doesn't grow forever.
    @Test func forgetsClosedPanes() {
        var p = BellPolicy()
        _ = p.ring(block: "a", setting: "flash", appActive: true, now: Date(timeIntervalSince1970: 1))
        _ = p.ring(block: "b", setting: "flash", appActive: true, now: Date(timeIntervalSince1970: 1))
        p.prune(keeping: ["b"])
        #expect(p.remembered == ["b"])
    }
}
