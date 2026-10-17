import Foundation
import Testing
@testable import HiveCore

/// Clicks and drags go to a program that tracks the mouse (Claude Code's
/// click-to-move and drag-to-copy need them), else to hive's own selection.
/// Shift always selects locally, as in Ghostty.
@Suite struct PointerTests {
    let geo = PointerGeometry(cellWidth: 10, cellHeight: 20, paneWidth: 800, paneHeight: 480)

    func mirror(_ setup: String) throws -> Mirror {
        let m = try Mirror(cols: 80, rows: 24)
        m.feed(Data(setup.utf8))
        return m
    }

    @Test func withoutTrackingSelectionIsLocal() throws {
        let m = try mirror("hello")
        #expect(m.pointer(.press, button: .left, mods: [], x: 25, y: 45, geometry: geo) == .local)
    }

    @Test func pressDragReleaseReachTheProgram() throws {
        let m = try mirror("\u{1b}[?1049h\u{1b}[?1002h\u{1b}[?1006h")
        #expect(m.pointer(.press, button: .left, mods: [], x: 25, y: 45, geometry: geo) == .send(Data("\u{1b}[<0;3;3M".utf8)))
        // Button-event tracking reports motion with the button held (32 + button).
        #expect(m.pointer(.motion, button: .left, mods: [], x: 35, y: 45, geometry: geo) == .send(Data("\u{1b}[<32;4;3M".utf8)))
        #expect(m.pointer(.release, button: .left, mods: [], x: 35, y: 45, geometry: geo) == .send(Data("\u{1b}[<0;4;3m".utf8)))
    }

    @Test func shiftSelectsLocally() throws {
        let m = try mirror("\u{1b}[?1049h\u{1b}[?1002h\u{1b}[?1006h")
        #expect(m.pointer(.press, button: .left, mods: .shift, x: 25, y: 45, geometry: geo) == .local)
    }

    /// Normal tracking (1000) reports presses but not motion: the program
    /// still owns the mouse, so nothing is sent and nothing is selected.
    @Test func motionWithoutMotionTrackingSendsNothing() throws {
        let m = try mirror("\u{1b}[?1049h\u{1b}[?1000h\u{1b}[?1006h")
        #expect(m.pointer(.motion, button: .left, mods: [], x: 35, y: 45, geometry: geo) == .send(Data()))
    }

    @Test func anyEventTrackingReportsHover() throws {
        let m = try mirror("\u{1b}[?1049h\u{1b}[?1003h\u{1b}[?1006h")
        // Motion with no button: 35 = 32 + 3 (no button).
        #expect(m.pointer(.motion, button: nil, mods: [], x: 45, y: 65, geometry: geo) == .send(Data("\u{1b}[<35;5;4M".utf8)))
    }

    /// A drag keeps the route its press chose: Shift pressed or released
    /// mid-drag can't leave the program's button stuck down.
    @Test func dragKeepsTheRouteOfItsPress() throws {
        let m = try mirror("\u{1b}[?1049h\u{1b}[?1002h\u{1b}[?1006h")
        #expect(m.pointer(.press, button: .left, mods: [], x: 25, y: 45, geometry: geo) != .local)
        #expect(m.pointer(.motion, button: .left, mods: .shift, x: 35, y: 45, geometry: geo) != .local)
        // Still sent to the program, with Shift reported (4).
        #expect(m.pointer(.release, button: .left, mods: .shift, x: 35, y: 45, geometry: geo) == .send(Data("\u{1b}[<4;4;3m".utf8)))
        // And the other way: a Shift-drag stays local after Shift is let go.
        #expect(m.pointer(.press, button: .left, mods: .shift, x: 25, y: 45, geometry: geo) == .local)
        #expect(m.pointer(.motion, button: .left, mods: [], x: 35, y: 45, geometry: geo) == .local)
        #expect(m.pointer(.release, button: .left, mods: [], x: 35, y: 45, geometry: geo) == .local)
    }

    /// A release outside the pane is clamped to its edge.
    @Test func positionsOutsideThePaneAreClamped() throws {
        let m = try mirror("\u{1b}[?1049h\u{1b}[?1000h\u{1b}[?1006h")
        #expect(m.pointer(.press, button: .left, mods: [], x: -5, y: 10_000, geometry: geo) == .send(Data("\u{1b}[<0;1;24M".utf8)))
        #expect(m.wheel(rows: -1, shift: false, x: 99_999, y: -3, geometry: geo) == .send(Data("\u{1b}[<64;80;1M".utf8)))
    }
}
