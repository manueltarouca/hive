import CGhosttyVT
import Foundation

/// An RGB color.
public struct RGB: Equatable, Hashable, Sendable {
    public var r, g, b: UInt8
    public init(_ r: UInt8, _ g: UInt8, _ b: UInt8) { self.r = r; self.g = g; self.b = b }
    init(_ c: GhosttyColorRgb) { self.init(c.r, c.g, c.b) }
}

/// One cell of a frame.
public struct Cell: Equatable, Sendable {
    public enum Wide: UInt8, Sendable { case narrow = 0, wide = 1, spacerTail = 2, spacerHead = 3 }
    public var text: String = ""
    public var wide: Wide = .narrow
    public var fg: RGB?
    public var bg: RGB?
    public var bold = false, italic = false, faint = false, inverse = false
    public var invisible = false, strikethrough = false, blink = false
    public var underline: UInt8 = 0
    public var selected = false
}

public struct Cursor: Equatable, Sendable {
    public enum Style: UInt8, Sendable { case bar = 0, block = 1, underline = 2, hollow = 3 }
    public var x: Int = 0, y: Int = 0
    public var visible = true, blinking = false
    public var style: Style = .block
}

/// An immutable picture of a pane, built on the vt queue and handed to the
/// main thread for drawing. Rows that didn't change share storage with the
/// previous frame.
public struct PaneFrame: Sendable {
    public var cols: Int, rows: Int
    public var cells: [[Cell]]
    public var cursor: Cursor
    public var background: RGB, foreground: RGB
    /// Rows that changed since the previous frame (all of them on a full
    /// redraw).
    public var dirtyRows: IndexSet
    public var generation: UInt64

    public var rowsCount: Int { cells.count }
    public func rowText(_ y: Int) -> String {
        guard cells.indices.contains(y) else { return "" }
        var s = ""
        for c in cells[y] where c.wide != .spacerTail && c.wide != .spacerHead {
            s += c.text.isEmpty ? " " : c.text
        }
        while s.hasSuffix(" ") { s.removeLast() }
        return s
    }
    // Back-compat for tests that index rows.
    public var rowsArray: [[Cell]] { cells }
}

extension PaneFrame {
    public subscript(row y: Int) -> [Cell] { cells[y] }
}

/// A key press, in terms of the physical key (W3C code) and modifiers.
public struct KeyInput: Sendable {
    public var key: Key
    public var mods: KeyMods
    /// The text the key produces with the current layout and modifiers.
    public var text: String
    /// The character the key produces without modifiers, for kitty and
    /// ctrl/alt encodings.
    public var unshifted: Character?

    public init(key: Key, mods: KeyMods = [], text: String = "", unshifted: Character? = nil) {
        self.key = key; self.mods = mods; self.text = text; self.unshifted = unshifted
    }
}

public struct KeyMods: OptionSet, Sendable {
    public let rawValue: UInt16
    public init(rawValue: UInt16) { self.rawValue = rawValue }
    public static let shift = KeyMods(rawValue: 1 << 0)
    public static let ctrl = KeyMods(rawValue: 1 << 1)
    public static let alt = KeyMods(rawValue: 1 << 2)
    public static let command = KeyMods(rawValue: 1 << 3)
    public static let capsLock = KeyMods(rawValue: 1 << 4)
}

/// A physical key, as libghostty names it.
public struct Key: Equatable, Hashable, Sendable {
    public let raw: GhosttyKey
    public init(_ raw: GhosttyKey) { self.raw = raw }

    public static let unidentified = Key(GHOSTTY_KEY_UNIDENTIFIED)
    public static let enter = Key(GHOSTTY_KEY_ENTER), tab = Key(GHOSTTY_KEY_TAB), space = Key(GHOSTTY_KEY_SPACE)
    public static let backspace = Key(GHOSTTY_KEY_BACKSPACE), escape = Key(GHOSTTY_KEY_ESCAPE), delete = Key(GHOSTTY_KEY_DELETE)
    public static let arrowUp = Key(GHOSTTY_KEY_ARROW_UP), arrowDown = Key(GHOSTTY_KEY_ARROW_DOWN)
    public static let arrowLeft = Key(GHOSTTY_KEY_ARROW_LEFT), arrowRight = Key(GHOSTTY_KEY_ARROW_RIGHT)
    public static let home = Key(GHOSTTY_KEY_HOME), end = Key(GHOSTTY_KEY_END)
    public static let pageUp = Key(GHOSTTY_KEY_PAGE_UP), pageDown = Key(GHOSTTY_KEY_PAGE_DOWN)
    public static let a = Key(GHOSTTY_KEY_A), c = Key(GHOSTTY_KEY_C)
}

public enum MirrorError: Error { case libghostty(String) }

/// A libghostty terminal mirroring one block. Not thread-safe: use it only
/// from the queue that owns it (the app's `hive.vt` queue). It never writes
/// back to the program: the server's terminal already answers queries, so
/// the mirror has no write-pty callback at all.
public final class Mirror {
    private var term: GhosttyTerminal?
    private var renderState: GhosttyRenderState?
    private var rowIter: GhosttyRenderStateRowIterator?
    private var rowCells: GhosttyRenderStateRowCells?
    private var scratch = [HiveCell](repeating: HiveCell(), count: 512)

    private var last: PaneFrame?
    private var generation: UInt64 = 0
    /// Synchronized output (mode 2026): while held, frame() returns the last
    /// complete frame. Holds older than `holdLimit` are ignored, so a program
    /// can't freeze the display.
    public private(set) var isHeld = false
    private var heldSince: Date?
    public var holdLimit: TimeInterval = 1.0

    public convenience init(cols: UInt16, rows: UInt16) throws {
        var t: GhosttyTerminal?
        guard hive_terminal_new(cols, rows, &t) == GHOSTTY_SUCCESS, let t else { throw MirrorError.libghostty("terminal_new") }
        try self.init(terminal: t)
    }

    public convenience init(snapshot: Data) throws {
        var t: GhosttyTerminal?
        let r = snapshot.withUnsafeBytes { p in
            hive_terminal_from_snapshot(p.bindMemory(to: UInt8.self).baseAddress, p.count, &t)
        }
        guard r == GHOSTTY_SUCCESS, let t else { throw MirrorError.libghostty("snapshot decode (\(r))") }
        try self.init(terminal: t)
        // A snapshot taken during synchronized output arrives already held;
        // the callback only reports transitions.
        if hive_mode(t, 2026, false) { isHeld = true; heldSince = Date() }
    }

    private init(terminal: GhosttyTerminal) throws {
        term = terminal
        guard ghostty_render_state_new(nil, &renderState) == GHOSTTY_SUCCESS,
              ghostty_render_state_row_iterator_new(nil, &rowIter) == GHOSTTY_SUCCESS,
              ghostty_render_state_row_cells_new(nil, &rowCells) == GHOSTTY_SUCCESS
        else { throw MirrorError.libghostty("render state") }
        hive_set_render_hold(terminal, { _, userdata, held in
            guard let userdata else { return }
            let m = Unmanaged<Mirror>.fromOpaque(userdata).takeUnretainedValue()
            m.isHeld = held
            m.heldSince = held ? Date() : nil
        }, Unmanaged.passUnretained(self).toOpaque())
    }

    deinit {
        if let rowCells { ghostty_render_state_row_cells_free(rowCells) }
        if let rowIter { ghostty_render_state_row_iterator_free(rowIter) }
        if let renderState { ghostty_render_state_free(renderState) }
        if let term { ghostty_terminal_free(term) }
    }

    /// Processes program output.
    public func feed(_ data: Data) {
        data.withUnsafeBytes { p in
            if let base = p.bindMemory(to: UInt8.self).baseAddress { ghostty_terminal_vt_write(term, base, p.count) }
        }
    }

    /// Applies default colors. Programs' own colors still win.
    public func apply(theme: Theme) {
        let pal = theme.palette.map { $0.c }
        pal.withUnsafeBufferPointer { hive_set_theme(term, theme.foreground.c, theme.background.c, theme.cursor.c, $0.baseAddress) }
    }

    public func resize(cols: UInt16, rows: UInt16, cellWidth: UInt32, cellHeight: UInt32) {
        ghostty_terminal_resize(term, cols, rows, max(cellWidth, 1), max(cellHeight, 1))
    }

    public func scroll(rows: Int) { hive_scroll_delta(term, rows) }
    public func scrollToBottom() { hive_scroll_bottom(term) }

    /// Encodes a key press for the program's current modes.
    public func encodeKey(_ k: KeyInput) -> Data {
        var out = [UInt8](repeating: 0, count: 128)
        let unshifted = k.unshifted?.unicodeScalars.first.map { $0.value } ?? 0
        let n = k.text.withCString { s in
            hive_encode_key(term, k.key.raw, k.mods.rawValue, s, k.text.utf8.count, unshifted, &out, out.count)
        }
        return Data(out.prefix(n))
    }

    /// Encodes pasted text: newlines become CR, and it is bracketed if the
    /// program asked for bracketed paste.
    public func encodePaste(_ text: String) -> Data {
        var data = Array(text.utf8).map { CChar(bitPattern: $0) }
        var out = [UInt8](repeating: 0, count: data.count * 2 + 32)
        var written = 0
        let r = hive_encode_paste(term, &data, data.count, &out, out.count, &written)
        return r == GHOSTTY_SUCCESS ? Data(out.prefix(written)) : Data(text.utf8)
    }

    public func snapshot() -> Data? {
        var ptr: UnsafeMutablePointer<UInt8>?
        var len = 0
        guard hive_snapshot(term, &ptr, &len) == GHOSTTY_SUCCESS, let ptr else { return nil }
        defer { hive_free(ptr, len) }
        return Data(bytes: ptr, count: len)
    }

    /// The whole terminal (scrollback and screen) as plain text, trimmed.
    public func plainText() -> String {
        var ptr: UnsafeMutablePointer<UInt8>?
        var len = 0
        guard hive_format_plain(term, &ptr, &len) == GHOSTTY_SUCCESS, let ptr else { return "" }
        defer { hive_free(ptr, len) }
        return String(decoding: UnsafeBufferPointer(start: ptr, count: len), as: UTF8.self)
    }

    /// Builds the current frame. While output is held (mode 2026) it returns
    /// the last complete frame.
    public func frame() -> PaneFrame {
        if isHeld, let heldSince, Date().timeIntervalSince(heldSince) < holdLimit, let last {
            return last
        }
        ghostty_render_state_update(renderState, term)
        var meta = HiveFrameMeta()
        hive_frame_meta(renderState, &meta)
        let cols = Int(meta.cols), rows = Int(meta.rows)
        var cells = last?.cells ?? []
        let full = meta.dirty == UInt8(GHOSTTY_RENDER_STATE_DIRTY_FULL.rawValue) || cells.count != rows
            || (cells.first?.count ?? -1) != cols
        if full { cells = Array(repeating: [], count: rows) }
        var dirty = IndexSet()

        if scratch.count < cols { scratch = [HiveCell](repeating: HiveCell(), count: cols) }
        ghostty_render_state_get(renderState, GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR, &rowIter)
        var y = 0
        while y < rows, ghostty_render_state_row_iterator_next(rowIter) {
            if full || hive_row_dirty(rowIter) {
                let n = scratch.withUnsafeMutableBufferPointer { hive_row_cells(rowIter, rowCells, $0.baseAddress, cols) }
                var row = [Cell](repeating: Cell(), count: cols)
                for x in 0..<min(n, cols) { row[x] = Cell(scratch[x]) }
                cells[y] = row
                dirty.insert(y)
            }
            y += 1
        }
        ghostty_render_state_clean(renderState)
        generation += 1
        let f = PaneFrame(
            cols: cols, rows: rows, cells: cells,
            cursor: Cursor(x: Int(meta.cursor_x), y: Int(meta.cursor_y),
                           visible: meta.cursor_visible && meta.cursor_in_viewport, blinking: meta.cursor_blinking,
                           style: Cursor.Style(rawValue: meta.cursor_style) ?? .block),
            background: RGB(meta.background), foreground: RGB(meta.foreground),
            dirtyRows: dirty, generation: generation)
        last = f
        return f
    }
}

extension Cell {
    init(_ c: HiveCell) {
        var t = c
        text = withUnsafeBytes(of: &t.text) { raw in
            String(decoding: raw.prefix(Int(c.text_len)), as: UTF8.self)
        }
        wide = Wide(rawValue: c.wide) ?? .narrow
        fg = c.has_fg != 0 ? RGB(c.fg) : nil
        bg = c.has_bg != 0 ? RGB(c.bg) : nil
        bold = c.bold; italic = c.italic; faint = c.faint; inverse = c.inverse
        invisible = c.invisible; strikethrough = c.strikethrough; blink = c.blink
        underline = c.underline; selected = c.selected
    }
}
