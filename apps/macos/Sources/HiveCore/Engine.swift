import os
import Foundation
import HiveKit

/// Connects block streams to mirrors. Everything runs on the client's queue
/// (the app's `hive.vt` queue): socket bytes go straight into libghostty,
/// and only immutable frames leave it.
public final class Engine {
    static let log = Logger(subsystem: "xyz.matm.hive", category: "pointer")
    public let client: HiveClient
    public var queue: DispatchQueue { client.queue }

    final class Pane {
        var mirror: Mirror?
        var expected: UInt64 = 0
        let onFrame: (PaneFrame) -> Void
        var framePending = false
        var pacer = FramePacer()
        var resyncs = 0
        /// Find on this pane: the query and the current match, kept across
        /// a new snapshot (a resize or a resync replaces the mirror).
        var findQuery: String?
        var findCurrent: Int?
        var findMatches: [FindMatch] = []
        /** Output arrived while find is open: search again shortly. */
        var findStale = false
        init(onFrame: @escaping (PaneFrame) -> Void) { self.onFrame = onFrame }
    }
    private var panes: [String: Pane] = [:]

    /// Counts resubscriptions caused by gaps or server resyncs; the M2 exit
    /// criterion is zero over a working day.
    public private(set) var resyncCount = 0
    public var log: (String) -> Void = { _ in }

    public var theme: Theme = .ghostty {
        didSet {
            guard theme != oldValue else { return }
            for (_, p) in panes {
                p.mirror?.apply(theme: theme)
                scheduleFrame(p)
            }
        }
    }

    /// cursor-style-blink, for every mirror.
    public var cursorBlink: Bool? {
        didSet {
            guard cursorBlink != oldValue else { return }
            for (_, p) in panes {
                p.mirror?.setDefaultCursorBlink(cursorBlink)
                scheduleFrame(p)
            }
        }
    }

    public init(client: HiveClient) { self.client = client }

    /// Starts mirroring a block. `onFrame` runs on the engine's queue.
    public func attach(block: String, onFrame: @escaping (PaneFrame) -> Void) {
        let pane = Pane(onFrame: onFrame)
        panes[block] = pane
        subscribe(block, pane)
    }

    public func detach(block: String) {
        panes[block] = nil
        client.unsubscribe(block: block)
    }

    private func subscribe(_ block: String, _ pane: Pane) {
        client.subscribe(block: block) { [weak self] update in
            self?.handle(block, pane, update)
        }
    }

    private func resubscribe(_ block: String, _ pane: Pane, _ why: String) {
        resyncCount += 1
        pane.resyncs += 1
        log("resync \(block): \(why)")
        guard panes[block] === pane else { return }
        client.unsubscribe(block: block)
        subscribe(block, pane)
    }

    private func handle(_ block: String, _ pane: Pane, _ update: BlockUpdate) {
        guard panes[block] === pane else { return }
        switch update {
        case .snapshot(let seq, let data):
            do {
                let m = try Mirror(snapshot: data)
                m.apply(theme: theme)
                m.setDefaultCursorBlink(cursorBlink)
                pane.mirror = m
                if pane.findQuery != nil { refind(block, pane) }
                pane.expected = seq
            } catch {
                log("snapshot for \(block) failed: \(error)")
                return
            }
        case .output(let seq, let data):
            guard let m = pane.mirror else { return }
            guard seq == pane.expected else {
                resubscribe(block, pane, "gap: got \(seq), expected \(pane.expected)")
                return
            }
            m.feed(data)
            pane.expected += UInt64(data.count)
            // New output can move or remove what find marked: search again, at most every 150 ms.
            if pane.findQuery != nil && !pane.findStale {
                pane.findStale = true
                queue.asyncAfter(deadline: .now() + 0.15) { [weak self, weak pane] in
                    guard let self, let pane, self.panes[block] === pane, pane.findStale else { return }
                    self.refind(block, pane)
                    self.scheduleFrame(pane)
                }
            }
        case .resize(let seq, let cols, let rows, let cw, let ch):
            guard let m = pane.mirror else { return }
            guard seq == pane.expected else {
                resubscribe(block, pane, "resize gap: \(seq) vs \(pane.expected)")
                return
            }
            m.resize(cols: cols, rows: rows, cellWidth: UInt32(cw), cellHeight: UInt32(ch))
        case .resync:
            resubscribe(block, pane, "server resync")
            return
        case .exited:
            break
        }
        scheduleFrame(pane)
    }

    /// Builds at most one frame per burst of messages.
    private func scheduleFrame(_ pane: Pane) {
        guard !pane.framePending else { return }
        pane.framePending = true
        let build = { [weak self, weak pane] in
            guard let self, let pane, let m = pane.mirror else { return }
            pane.framePending = false
            pane.pacer.built(at: ProcessInfo.processInfo.systemUptime)
            pane.onFrame(m.frame())
            // A program can start synchronized output and stall. Draw again
            // once the hold limit passes, even with no more output.
            if m.isHeld {
                self.queue.asyncAfter(deadline: .now() + m.holdLimit + 0.05) { [weak self, weak pane] in
                    if let self, let pane { self.scheduleFrame(pane) }
                }
            }
        }
        // Output meanwhile goes into the mirror; the frame shows all of it.
        let wait = pane.pacer.delay(at: ProcessInfo.processInfo.systemUptime)
        if wait > 0 { queue.asyncAfter(deadline: .now() + wait, execute: build) } else { queue.async(execute: build) }
    }

    // MARK: input

    public func select(block: String, _ type: Mirror.SelectEvent, col: Int, row: Int, x: Double, y: Double,
                       cellWidth: Double, cellHeight: Double, repeatInterval: TimeInterval = 0.5, repeatDistance: Double = 8) {
        guard let pane = panes[block], let m = pane.mirror else { return }
        m.select(type, col: col, row: row, x: x, y: y, cellWidth: cellWidth, cellHeight: cellHeight,
                 timeNs: DispatchTime.now().uptimeNanoseconds, repeatInterval: repeatInterval, repeatDistance: repeatDistance)
        scheduleFrame(pane)
    }

    /// A click, release or motion: to the program when it tracks the mouse,
    /// else hive's own selection (press, drag with a button held, release).
    public func pointer(block: String, _ action: Mirror.PointerAction, button: Mirror.PointerButton?, mods: KeyMods,
                        x: Double, y: Double, geometry: PointerGeometry, col: Int, row: Int,
                        repeatInterval: TimeInterval, repeatDistance: Double) {
        guard let pane = panes[block], let m = pane.mirror else { return }
        let result = m.pointer(action, button: button, mods: mods, x: x, y: y, geometry: geometry)
        // `log stream --level debug --predicate 'subsystem == "xyz.matm.hive"'`
        Engine.log.debug("pointer \(String(describing: action), privacy: .public) button \(button?.rawValue ?? 0) at \(Int(x)),\(Int(y)) -> \(String(describing: result), privacy: .public)")
        switch result {
        case .send(let bytes):
            if action == .press { clearSelection(block) }
            if !bytes.isEmpty { client.input(block: block, bytes) }
        case .local:
            guard button == .left else { return }
            let type: Mirror.SelectEvent = action == .press ? .press : action == .release ? .release : .drag
            select(block: block, type, col: col, row: row, x: x, y: y,
                   cellWidth: Double(geometry.cellWidth), cellHeight: Double(geometry.cellHeight),
                   repeatInterval: repeatInterval, repeatDistance: repeatDistance)
        }
    }

    public func selectedText(block: String) -> String? { panes[block]?.mirror?.selectedText() }

    private func clearSelection(_ block: String) {
        guard let pane = panes[block], let m = pane.mirror, m.selectedText() != nil else { return }
        m.clearSelection()
        scheduleFrame(pane)
    }

    public func key(block: String, _ k: KeyInput) {
        clearSelection(block)
        guard let m = panes[block]?.mirror else { return }
        let bytes = m.encodeKey(k)
        if !bytes.isEmpty {
            m.scrollToBottom()
            client.input(block: block, bytes)
        }
    }

    public func text(block: String, _ s: String) {
        clearSelection(block)
        guard let m = panes[block]?.mirror, !s.isEmpty else { return }
        m.scrollToBottom()
        client.input(block: block, Data(s.utf8))
    }

    public func paste(block: String, _ s: String) {
        guard let m = panes[block]?.mirror else { return }
        client.input(block: block, m.encodePaste(s))
    }

    /// A scroll-wheel movement: to the program, as arrow keys, or scrolling
    /// hive's scrollback (see Mirror.wheel). Sending doesn't jump to the
    /// bottom, unlike typing.
    public func wheel(block: String, rows: Int, shift: Bool, x: Double, y: Double, geometry: PointerGeometry) {
        guard let pane = panes[block], let m = pane.mirror else { return }
        switch m.wheel(rows: rows, shift: shift, x: x, y: y, geometry: geometry) {
        case .viewport(let r):
            m.scroll(rows: r)
            scheduleFrame(pane)
        case .send(let bytes):
            clearSelection(block)
            if !bytes.isEmpty { client.input(block: block, bytes) }
        }
    }

    public func scroll(block: String, rows: Int) {
        guard let pane = panes[block], let m = pane.mirror else { return }
        m.scroll(rows: rows)
        scheduleFrame(pane)
    }

    /// Find's count and current match changed without the user (new output,
    /// a new snapshot): block, count, current. On the engine's queue.
    public var onFindChanged: ((String, Int, Int?) -> Void)?

    /// Searches a block's scrollback and screen, marks the matches, makes the
    /// one nearest the bottom current and scrolls it into view. An empty query
    /// ends find. Returns the count and the current match.
    @discardableResult
    public func find(block: String, query: String) -> (count: Int, current: Int?) {
        guard let pane = panes[block], let m = pane.mirror else { return (0, nil) }
        pane.findQuery = query.isEmpty ? nil : query
        pane.findStale = false
        pane.findMatches = query.isEmpty ? [] : m.find(query)
        pane.findCurrent = pane.findMatches.isEmpty ? nil : pane.findMatches.count - 1
        show(pane, m, reveal: true)
        return (pane.findMatches.count, pane.findCurrent)
    }

    /// The next (1) or previous (-1) match, wrapping around.
    @discardableResult
    public func stepFind(block: String, by: Int) -> Int? {
        guard let pane = panes[block], let m = pane.mirror, !pane.findMatches.isEmpty else { return nil }
        let n = pane.findMatches.count
        pane.findCurrent = ((pane.findCurrent ?? (by > 0 ? -1 : n)) + by + n) % n
        show(pane, m, reveal: true)
        return pane.findCurrent
    }

    public func endFind(block: String) {
        guard let pane = panes[block] else { return }
        pane.findQuery = nil
        pane.findMatches = []
        pane.findCurrent = nil
        pane.findStale = false
        if let m = pane.mirror { show(pane, m, reveal: false) }
    }

    private func show(_ pane: Pane, _ m: Mirror, reveal: Bool) {
        m.setFindMarks(pane.findMatches, current: pane.findCurrent)
        if reveal, let c = pane.findCurrent, pane.findMatches.indices.contains(c) { m.reveal(pane.findMatches[c]) }
        scheduleFrame(pane)
    }

    /// Searches again for the open query, keeping the current match on its
    /// row when it's still there, and tells the app.
    private func refind(_ block: String, _ pane: Pane) {
        guard let q = pane.findQuery, let m = pane.mirror else { return }
        pane.findStale = false
        let was = pane.findCurrent.flatMap { pane.findMatches.indices.contains($0) ? pane.findMatches[$0] : nil }
        pane.findMatches = m.find(q)
        pane.findCurrent = was.flatMap { w in pane.findMatches.firstIndex { $0.row == w.row && $0.col == w.col } }
            ?? (pane.findMatches.isEmpty ? nil : pane.findMatches.count - 1)
        m.setFindMarks(pane.findMatches, current: pane.findCurrent)
        onFindChanged?(block, pane.findMatches.count, pane.findCurrent)
    }

    /// The mirror's text, for tests and debugging.
    public func plainText(block: String) -> String? { panes[block]?.mirror?.plainText() }
    public func mirror(block: String) -> Mirror? { panes[block]?.mirror }

    /// Whether the program in a block has turned on mouse reporting.
    public func tracksMouse(block: String) -> Bool { panes[block]?.mirror?.tracksMouse ?? false }
}
