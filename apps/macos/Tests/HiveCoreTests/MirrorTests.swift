import Foundation
import Testing
@testable import HiveCore

private func bytes(_ s: String) -> Data { Data(s.utf8) }

@Suite struct MirrorTests {
    @Test func frameCellsAndStyles() throws {
        let m = try Mirror(cols: 20, rows: 4)
        m.feed(bytes("ab\u{1b}[1;31mC\u{1b}[0m 你x\r\n"))
        let f = m.frame()
        #expect(f.cols == 20 && f.rows == 4)
        let row = f.cells[0]
        #expect(row[0].text == "a" && row[1].text == "b")
        #expect(row[2].text == "C" && row[2].bold && row[2].fg != nil)
        #expect(row[4].text == "你" && row[4].wide == .wide)
        #expect(row[5].wide == .spacerTail)
        #expect(row[6].text == "x")
        #expect(f.cursor.y == 1 && f.cursor.x == 0)
        #expect(f.rowText(0) == "abC 你x")
    }

    @Test func dirtyRowsOnlyAfterClean() throws {
        let m = try Mirror(cols: 10, rows: 3)
        m.feed(bytes("one\r\ntwo"))
        _ = m.frame()
        m.feed(bytes("\u{1b}[1;1Hxyz"))
        let f = m.frame()
        #expect(f.dirtyRows.contains(0))
        #expect(!f.dirtyRows.contains(2))
        #expect(f.rowText(0) == "xyz" && f.rowText(1) == "two")
    }

    @Test func keyEncodingFollowsModes() throws {
        let m = try Mirror(cols: 10, rows: 3)
        #expect(m.encodeKey(.init(key: .arrowUp)) == bytes("\u{1b}[A"))
        m.feed(bytes("\u{1b}[?1h")) // DECCKM: application cursor keys
        #expect(m.encodeKey(.init(key: .arrowUp)) == bytes("\u{1b}OA"))
        #expect(m.encodeKey(.init(key: .c, mods: [.ctrl], unshifted: "c")) == Data([0x03]))
        m.feed(bytes("\u{1b}[>31u")) // kitty keyboard: everything
        let shiftEnter = m.encodeKey(.init(key: .enter, mods: [.shift]))
        #expect(shiftEnter == bytes("\u{1b}[13;2u"))
    }

    @Test func pasteIsBracketedOnlyWhenAsked() throws {
        let m = try Mirror(cols: 10, rows: 3)
        #expect(m.encodePaste("a\nb") == bytes("a\rb"))
        m.feed(bytes("\u{1b}[?2004h"))
        // Inside a bracketed paste newlines stay LF; the program sees the text as is.
        #expect(m.encodePaste("a\nb") == bytes("\u{1b}[200~a\nb\u{1b}[201~"))
    }

    @Test func renderHoldKeepsLastFrame() throws {
        let m = try Mirror(cols: 10, rows: 2)
        m.feed(bytes("before"))
        #expect(m.frame().rowText(0) == "before")
        m.feed(bytes("\u{1b}[?2026h\u{1b}[2J\u{1b}[Hhalf")) // synchronized output starts
        #expect(m.isHeld)
        #expect(m.frame().rowText(0) == "before")       // still the last complete frame
        m.feed(bytes("-done\u{1b}[?2026l"))
        #expect(!m.isHeld)
        #expect(m.frame().rowText(0) == "half-done")
    }

    @Test func snapshotThenResizeAtPosition() throws {
        let src = try Mirror(cols: 20, rows: 4)
        src.feed(bytes(String(repeating: "0123456789", count: 3)))
        let snap = try #require(src.snapshot())
        let m = try Mirror(snapshot: snap)
        #expect(m.frame().rowText(0) == "01234567890123456789")
        m.resize(cols: 10, rows: 6, cellWidth: 9, cellHeight: 18)
        let f = m.frame()
        #expect(f.cols == 10 && f.rowText(0) == "0123456789" && f.rowText(2) == "0123456789")
    }
}

@Suite struct MacKeyTests {
    @Test func physicalKeysEncodeLikeAKeyboard() throws {
        let m = try Mirror(cols: 10, rows: 2)
        #expect(Key.mac(0x7E) == .arrowUp)
        #expect(m.encodeKey(KeyInput(key: .mac(0x24))) == Data([0x0D]))                    // return
        #expect(m.encodeKey(KeyInput(key: .mac(0x08), mods: [.ctrl], unshifted: "c")) == Data([0x03]))
        #expect(m.encodeKey(KeyInput(key: .mac(0x00), text: "a", unshifted: "a")) == Data("a".utf8))
        #expect(m.encodeKey(KeyInput(key: .mac(0x33))) == Data([0x7F]))                    // backspace
        #expect(Key.mac(0xFF) == .unidentified)
    }
}

@Suite struct SelectionTests {
    @Test func dragSelectsAndCopies() throws {
        let m = try Mirror(cols: 20, rows: 4)
        m.feed(Data("hello world\r\nsecond line".utf8))
        let cw: Double = 10, ch: Double = 20
        m.select(.press, col: 0, row: 0, x: 2, y: 8, cellWidth: cw, cellHeight: ch, timeNs: 1_000_000_000)
        m.select(.drag, col: 4, row: 0, x: 46, y: 8, cellWidth: cw, cellHeight: ch, timeNs: 1_050_000_000)
        m.select(.release, col: 4, row: 0, x: 46, y: 8, cellWidth: cw, cellHeight: ch, timeNs: 1_100_000_000)
        #expect(m.selectedText() == "hello")
        #expect(m.frame().cells[0][0].selected && !m.frame().cells[0][6].selected)
        m.clearSelection()
        #expect(m.selectedText() == nil)
    }

    @Test func doubleClickSelectsAWord() throws {
        let m = try Mirror(cols: 20, rows: 4)
        m.feed(Data("hello world".utf8))
        let t: UInt64 = 5_000_000_000
        for (i, type) in [Mirror.SelectEvent.press, .release, .press, .release].enumerated() {
            m.select(type, col: 7, row: 0, x: 75, y: 8, cellWidth: 10, cellHeight: 20, timeNs: t + UInt64(i) * 40_000_000)
        }
        #expect(m.selectedText() == "world")
    }
}
