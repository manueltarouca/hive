import CoreGraphics
import Foundation
import XCTest
@testable import HiveCore

final class TearOffTests: XCTestCase {
    func testTearsOnlyPastTheThreshold() {
        XCTAssertFalse(TearOff.tears(across: 10, horizontalRow: true))
        XCTAssertTrue(TearOff.tears(across: 40, horizontalRow: true))
        // Up from the tab bar is the screen's edge: not a tear.
        XCTAssertFalse(TearOff.tears(across: -60, horizontalRow: true))
        // Out of the sidebar, either side.
        XCTAssertTrue(TearOff.tears(across: 40, horizontalRow: false))
        XCTAssertTrue(TearOff.tears(across: -40, horizontalRow: false))
        XCTAssertFalse(TearOff.tears(across: 20, horizontalRow: false))
    }

    func testWindowFrameHangsFromThePointerAndStaysOnScreen() {
        let screen = CGRect(x: 0, y: 0, width: 1440, height: 900)
        let f = TearOff.windowFrame(dropAt: CGPoint(x: 500, y: 600), size: CGSize(width: 900, height: 600), screen: screen)
        XCTAssertEqual(f.size, CGSize(width: 900, height: 600))
        XCTAssertEqual(f.maxY, 619)  // the title bar's middle under the pointer
        XCTAssertEqual(f.minX, 380)
        // Near the bottom right corner: pushed back on screen.
        let g = TearOff.windowFrame(dropAt: CGPoint(x: 1430, y: 100), size: CGSize(width: 900, height: 600), screen: screen)
        XCTAssertEqual(g.maxX, 1440)
        XCTAssertEqual(g.minY, 0)
        // Bigger than the screen: the screen's size.
        let h = TearOff.windowFrame(dropAt: .zero, size: CGSize(width: 3000, height: 2000), screen: screen)
        XCTAssertEqual(h, screen)
    }

    func testTornTabsRoundTripAndRestoreOnlyLiveOnes() {
        let list = [TornTab(session: "s1", window: "w1", frame: "{{1, 2}, {3, 4}}"), TornTab(session: "s2", window: "w2")]
        XCTAssertEqual(TornTab.decode(TornTab.encode(list)), list)
        XCTAssertEqual(TornTab.decode(Data("nonsense".utf8)), [])
        XCTAssertEqual(TornTab.decode(nil), [])
        XCTAssertEqual(TornTab.restorable(list, session: "s1", windows: ["w1", "w9"]).map(\.window), ["w1"])
        XCTAssertEqual(TornTab.restorable(list, session: "s1", windows: ["w9"]), [])
    }

    func testMainTabKeepsItsPlaceWhenTheActiveOneIsTorn() {
        let all = ["a", "b", "c"]
        XCTAssertEqual(MainTab.pick(serverActive: "b", last: "a", windows: all, torn: []), "b")
        // Another device (or a torn window's focus on an old server) made a torn tab active.
        XCTAssertEqual(MainTab.pick(serverActive: "b", last: "c", windows: all, torn: ["b"]), "c")
        // What it showed is gone or torn too: the first left.
        XCTAssertEqual(MainTab.pick(serverActive: "b", last: "b", windows: all, torn: ["b"]), "a")
        XCTAssertNil(MainTab.pick(serverActive: "a", last: nil, windows: all, torn: Set(all)))
        XCTAssertNil(MainTab.pick(serverActive: nil, last: nil, windows: [], torn: []))
    }
}
