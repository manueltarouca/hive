import Foundation
import HiveKit

/// Events on their way to the main thread, in batches: one hop for however
/// many arrived meanwhile, not a task per event. Bounded: a main thread
/// that can't keep up mustn't grow the app (clipboard events at 100,000 a
/// second added 447 MB in 6 s, bench/app-storm.sh). Past the cap the events
/// go, and the taker re-reads state, as on the server's events_resync.
public final class EventBatch: @unchecked Sendable {
    private let lock = NSLock()
    private let cap: Int
    private var items: [JSON] = []
    private var pending = false
    private var overflowed = false

    public init(cap: Int = 4096) { self.cap = cap }

    /// Adds an event; true when the caller should schedule a hop to take.
    public func add(_ e: JSON) -> Bool {
        lock.lock()
        defer { lock.unlock() }
        if items.count >= cap {
            items.removeAll(keepingCapacity: true)
            overflowed = true
        }
        if !overflowed { items.append(e) }
        if pending { return false }
        pending = true
        return true
    }

    /// The events so far, in order, and whether some were dropped (then
    /// re-read state instead of trusting them); the next add asks again.
    public func takeAll() -> (events: [JSON], overflowed: Bool) {
        lock.lock()
        defer { lock.unlock() }
        let out = (items, overflowed)
        items = []
        pending = false
        overflowed = false
        return out
    }

    /// The events so far, in order (tests and callers that ignore overflow).
    public func take() -> [JSON] { takeAll().events }
}
