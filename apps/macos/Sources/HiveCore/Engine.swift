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

    public var theme: Theme = .tomorrowNight

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
        queue.async { [weak pane] in
            guard let pane, let m = pane.mirror else { return }
            pane.framePending = false
            pane.onFrame(m.frame())
        }
    }

    // MARK: input

    public func key(block: String, _ k: KeyInput) {
        guard let m = panes[block]?.mirror else { return }
        let bytes = m.encodeKey(k)
        if !bytes.isEmpty {
            m.scrollToBottom()
            client.input(block: block, bytes)
        }
    }

    public func text(block: String, _ s: String) {
        guard let m = panes[block]?.mirror, !s.isEmpty else { return }
        m.scrollToBottom()
        client.input(block: block, Data(s.utf8))
    }

    public func paste(block: String, _ s: String) {
        guard let m = panes[block]?.mirror else { return }
        client.input(block: block, m.encodePaste(s))
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
