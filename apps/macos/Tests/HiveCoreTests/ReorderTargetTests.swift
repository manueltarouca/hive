import XCTest
@testable import HiveCore

final class ReorderTargetTests: XCTestCase {
    // Five tabs 100 wide: middles at 50, 150, 250, 350, 450.
    let m: [CGFloat?] = [50, 150, 250, 350, 450]

    func testItStaysUntilItsMiddleCrossesTheNext() {
        XCTAssertEqual(ReorderTarget.index(from: 0, middle: 149, middles: m), 0)
        XCTAssertEqual(ReorderTarget.index(from: 0, middle: 151, middles: m), 1)
        XCTAssertEqual(ReorderTarget.index(from: 0, middle: 310, middles: m), 2) // 2.6 tabs along: after the third
        XCTAssertEqual(ReorderTarget.index(from: 0, middle: 9999, middles: m), 4) // past the end: last
    }

    func testBackwardsToo() {
        XCTAssertEqual(ReorderTarget.index(from: 4, middle: 351, middles: m), 4)
        XCTAssertEqual(ReorderTarget.index(from: 4, middle: 349, middles: m), 3)
        XCTAssertEqual(ReorderTarget.index(from: 4, middle: -50, middles: m), 0)
    }

    func testTheOthersMakeRoomTowardWhereItLeft() {
        // One dragged from 0 to 2: the second and third step back one; the rest stay.
        XCTAssertEqual((0..<5).map { ReorderTarget.shift(index: $0, from: 0, to: 2) }, [0, -1, -1, 0, 0])
        XCTAssertEqual((0..<5).map { ReorderTarget.shift(index: $0, from: 4, to: 1) }, [0, 1, 1, 1, 0])
        XCTAssertEqual((0..<5).map { ReorderTarget.shift(index: $0, from: 2, to: 2) }, [0, 0, 0, 0, 0])
    }

    func testAnUnmeasuredTabStopsTheSearch() {
        XCTAssertEqual(ReorderTarget.index(from: 0, middle: 400, middles: [50, 150, nil, 350]), 1)
    }
}
