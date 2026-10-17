import Foundation
import HiveKit

/// Connects block streams to mirrors. Everything runs on the client's queue
/// (the app's `hive.vt` queue): socket bytes go straight into libghostty,
/// and only immutable frames leave it.
public final class Engine {
    public let client: HiveClient
    public var queue: DispatchQueue { client.queue }

    final class Pane {
        var mirror: Mirror?
        var expected: UInt64 = 0
        let onFrame: (PaneFrame) -> Void
        var framePending = false
        var resyncs = 0
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
                pane.mirror = m
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
        queue.async { [weak self, weak pane] in
            guard let self, let pane, let m = pane.mirror else { return }
            pane.framePending = false
            pane.onFrame(m.frame())
            // A program can start synchronized output and stall. Draw again
            // once the hold limit passes, even with no more output.
            if m.isHeld {
                self.queue.asyncAfter(deadline: .now() + m.holdLimit + 0.05) { [weak self, weak pane] in
                    if let self, let pane { self.scheduleFrame(pane) }
                }
            }
        }
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
        switch m.pointer(action, button: button, mods: mods, x: x, y: y, geometry: geometry) {
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

    /// The mirror's text, for tests and debugging.
    public func plainText(block: String) -> String? { panes[block]?.mirror?.plainText() }
    public func mirror(block: String) -> Mirror? { panes[block]?.mirror }
}
