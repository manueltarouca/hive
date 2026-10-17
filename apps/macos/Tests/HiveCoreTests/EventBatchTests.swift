import Foundation
import HiveKit
import Testing
@testable import HiveCore

/// Events reach the main thread in batches: one hop for however many
/// arrived meanwhile, not a task per event (10,000 a second spun up the
/// cooperative pool and saturated the main thread).
@Suite struct EventBatchTests {
    @Test func firstEventAsksForAHopLaterOnesJoinIt() {
        let b = EventBatch()
        #expect(b.add(["n": 1]))     // schedule a hop
        #expect(!b.add(["n": 2]))    // one is coming
        #expect(!b.add(["n": 3]))
        #expect(b.take().map { $0["n"].double } == [1, 2, 3])
        #expect(b.take().isEmpty)
        #expect(b.add(["n": 4]))     // after a take, the next asks again
    }

    @Test func orderIsKeptAcrossThreads() async {
        let b = EventBatch()
        await withTaskGroup(of: Void.self) { g in
            g.addTask { for i in 0..<1000 { _ = b.add(["n": .number(Double(i))]) } }
        }
        #expect(b.take().map { Int($0["n"].double ?? -1) } == Array(0..<1000))
    }

    /// A main thread that can't keep up mustn't grow the app without bound
    /// (clipboard events at 100,000 a second added 447 MB in 6 s): past the
    /// cap the events go and take() says to re-read state, as the server's
    /// events_resync does.
    @Test func overflowDropsAndAsksForAReread() {
        let b = EventBatch(cap: 3)
        for i in 0..<10 { _ = b.add(["n": .number(Double(i))]) }
        let (events, overflowed) = b.takeAll()
        #expect(overflowed)
        #expect(events.count <= 3)
        _ = b.add(["n": 99])
        let (next, again) = b.takeAll()
        #expect(!again && next.count == 1)
    }
}
