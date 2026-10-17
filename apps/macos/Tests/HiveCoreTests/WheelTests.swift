import Foundation
import Testing
@testable import HiveCore

/// Where a scroll-wheel event goes, decided as Ghostty does: to the program
/// when it asked for the mouse, as arrow keys on the alternate screen, and
/// otherwise to hive's own scrollback.
@Suite struct WheelTests {
    // 10×20 px cells; x 25, y 45 is column 3, row 3 (1-based).
    let geo = PointerGeometry(cellWidth: 10, cellHeight: 20, paneWidth: 800, paneHeight: 480)

    func mirror(_ setup: String) throws -> Mirror {
        let m = try Mirror(cols: 80, rows: 24)
        m.feed(Data(setup.utf8))
        return m
    }

    @Test func normalScreenScrollsTheViewport() throws {
        let m = try mirror("hello\r\n")
        #expect(m.wheel(rows: -3, shift: false, x: 25, y: 45, geometry: geo) == .viewport(rows: -3))
    }

    @Test func alternateScreenSendsArrowKeys() throws {
        let m = try mirror("\u{1b}[?1049h")
        #expect(m.wheel(rows: 3, shift: false, x: 25, y: 45, geometry: geo) == .send(Data("\u{1b}[B\u{1b}[B\u{1b}[B".utf8)))
        #expect(m.wheel(rows: -1, shift: false, x: 25, y: 45, geometry: geo) == .send(Data("\u{1b}[A".utf8)))
        // Application cursor keys (DECCKM) change the encoding.
        m.feed(Data("\u{1b}[?1h".utf8))
        #expect(m.wheel(rows: 2, shift: false, x: 25, y: 45, geometry: geo) == .send(Data("\u{1b}OB\u{1b}OB".utf8)))
    }

    @Test func alternateScrollOffScrollsTheViewport() throws {
        let m = try mirror("\u{1b}[?1049h\u{1b}[?1007l")
        #expect(m.wheel(rows: 1, shift: false, x: 25, y: 45, geometry: geo) == .viewport(rows: 1))
    }

    @Test func mouseTrackingSendsWheelReports() throws {
        let m = try mirror("\u{1b}[?1049h\u{1b}[?1000h\u{1b}[?1006h")
        #expect(m.wheel(rows: -1, shift: false, x: 25, y: 45, geometry: geo) == .send(Data("\u{1b}[<64;3;3M".utf8)))
        #expect(m.wheel(rows: 2, shift: false, x: 25, y: 45, geometry: geo) == .send(Data("\u{1b}[<65;3;3M\u{1b}[<65;3;3M".utf8)))
    }

    @Test func shiftAlwaysScrollsTheViewport() throws {
        let m = try mirror("\u{1b}[?1049h\u{1b}[?1000h\u{1b}[?1006h")
        #expect(m.wheel(rows: -2, shift: true, x: 25, y: 45, geometry: geo) == .viewport(rows: -2))
    }

    /// X10 tracking doesn't report wheels: send nothing, and don't scroll.
    @Test func x10TrackingSendsNothing() throws {
        let m = try mirror("\u{1b}[?1049h\u{1b}[?9h")
        #expect(m.wheel(rows: 1, shift: false, x: 25, y: 45, geometry: geo) == .send(Data()))
    }
}
