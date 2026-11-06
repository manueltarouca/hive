import AppKit
import HiveCore
import SwiftUI

/// Dragging a tab to a new place, in the tab bar or the sidebar: the tab follows the pointer
/// and the others slide aside to open the gap where it will land, so you see the place before
/// you let go. Letting go where it started leaves the order as it was.
final class Reorder: ObservableObject {
    /// Where each reorderable list's tabs are, in its hosting view (top left origin), by the list's
    /// space name: the host moves the window only from elsewhere, and takes a first click on a tab.
    static var tabRects: [String: [CGRect]] = [:]

    /// Along x (the tab bar) or y (the sidebar).
    let horizontal: Bool
    let spacing: CGFloat
    @Published private(set) var dragging: String?
    /// Let go (moved or not): it springs into its place, still above the others.
    @Published private(set) var settling: String?
    /// How far the dragged tab has moved from where it started.
    @Published private(set) var offset: CGFloat = 0
    /// The place it would land, in the shown order.
    @Published private(set) var target: Int?
    /// Pulled off the row: it's being torn into its own window (TearDrag shows it under the pointer).
    @Published private(set) var tearing: String?
    private var from = 0
    /// Each tab's extent along the axis, from the last layout at rest: start and length.
    var frames: [String: (start: CGFloat, length: CGFloat)] = [:]
    private(set) var order: [String] = []
    /// Escape let go of the tab: the rest of that gesture does nothing.
    private(set) var cancelled = false
    private var escape: Any?
    private static let spring = Animation.spring(response: 0.3, dampingFraction: 0.86)

    init(horizontal: Bool, spacing: CGFloat) {
        self.horizontal = horizontal
        self.spacing = spacing
    }

    #if DEBUG
    /// The lists on screen, by space name, for the screenshot loop's drag (HIVE_OPEN=reorder).
    static var live: [String: Reorder] = [:]
    #endif

    var atRest: Bool { dragging == nil && settling == nil && tearing == nil }

    /// The dragged tab left its row: it springs back into its place and the drag goes on as a tear.
    func startTear(_ id: String) {
        guard dragging == id else { return }
        tearing = id
        withAnimation(Reorder.spring) {
            target = nil
            dragging = nil
            settling = id
            offset = 0
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.35) { [weak self] in
            if self?.settling == id { self?.settling = nil }
        }
    }

    /// The tear is over (dropped or cancelled).
    func endTear() {
        tearing = nil
        if let m = escape { NSEvent.removeMonitor(m); escape = nil }
    }

    func begin(_ id: String, order: [String]) {
        guard dragging == nil, !cancelled, let i = order.firstIndex(of: id) else { return }
        self.order = order
        from = i
        settling = nil
        dragging = id
        target = i
        // Escape puts it back, as in Finder and Safari.
        escape = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] e in
            guard e.keyCode == 53, let self, self.dragging != nil || self.tearing != nil else { return e }
            self.cancelled = true
            if self.tearing != nil {
                self.endTear()
                TearDrag.shared.drop(commit: false)
            } else {
                self.cancel()
            }
            return nil
        }
    }

    func move(by delta: CGFloat) {
        guard let id = dragging, let f = frames[id] else { return }
        // Kept within the row: from the first tab's start to the last one's end.
        let all = order.compactMap { frames[$0] }
        let lo = (all.map(\.start).min() ?? f.start) - f.start
        let hi = (all.map { $0.start + $0.length }.max() ?? f.start + f.length) - (f.start + f.length)
        let d = min(max(delta, lo), hi)
        offset = d
        // Where its middle is now: past the middles of the tabs it has crossed, toward where it moves.
        let t = ReorderTarget.index(from: from, middle: f.start + d + f.length / 2,
                                    middles: order.map { id in frames[id].map { $0.start + $0.length / 2 } })
        if t != target { withAnimation(Reorder.spring) { target = t } }
    }

    /// Let go: [commit] moves it to where it landed. Its new place takes effect at once, and its
    /// offset is set from there, so nothing jumps; then it springs into the place.
    func finish(commit: (String, Int) -> Void) {
        guard let id = dragging else { return }
        guard let t = target, t != from else { cancel(); return }
        var travel: CGFloat = 0
        for i in min(from, t)...max(from, t) where i != from { travel += (frames[order[i]]?.length ?? 0) + spacing }
        if t < from { travel = -travel }
        var tx = Transaction()
        tx.disablesAnimations = true
        withTransaction(tx) {
            commit(id, t)
            offset -= travel
            target = nil
            dragging = nil
            settling = id
        }
        settle(id)
    }

    /// Back where it started (Escape, a drop where it began, the system taking the gesture).
    func cancel() {
        guard let id = dragging else { return }
        withAnimation(Reorder.spring) {
            target = nil
            dragging = nil
            settling = id
        }
        settle(id)
    }

    /// The gesture is over, however it ended: the next one may start.
    func gestureEnded() {
        cancelled = false
        if dragging != nil { cancel() }
        // The system took the gesture mid-tear: nothing tears off.
        if tearing != nil {
            endTear()
            MainActor.assumeIsolated { TearDrag.shared.drop(commit: false) }
        }
    }

    private func settle(_ id: String) {
        if let m = escape { NSEvent.removeMonitor(m); escape = nil }
        withAnimation(Reorder.spring) { offset = 0 }
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.35) { [weak self] in
            if self?.settling == id { self?.settling = nil }
        }
    }

    /// How far a tab that isn't dragged moves aside: the dragged one's length and a gap, toward where it left.
    func shift(of id: String) -> CGFloat {
        guard let d = dragging, d != id, let t = target, let i = order.firstIndex(of: id), let f = frames[d] else { return 0 }
        return CGFloat(ReorderTarget.shift(index: i, from: from, to: t)) * (f.length + spacing)
    }
}

private struct HostFrameKey: PreferenceKey {
    static var defaultValue: [String: CGRect] = [:]
    static func reduce(value: inout [String: CGRect], nextValue: () -> [String: CGRect]) { value.merge(nextValue(), uniquingKeysWith: { $1 }) }
}

private struct FrameKey: PreferenceKey {
    static var defaultValue: [String: CGRect] = [:]
    static func reduce(value: inout [String: CGRect], nextValue: () -> [String: CGRect]) { value.merge(nextValue(), uniquingKeysWith: { $1 }) }
}

extension View {
    /// One tab in a reorderable row or column: reports where it is, moves aside for the dragged one,
    /// and follows the pointer while it is the one dragged. [commit]: where it landed, in the shown order.
    /// [canTear]: whether pulling it off the row tears it into its own window.
    func reorderable(_ id: String, in r: Reorder, order: @escaping () -> [String], space: String,
                     canTear: @escaping () -> Bool = { false }, commit: @escaping (String, Int) -> Void) -> some View {
        modifier(ReorderItem(id: id, r: r, order: order, space: space, canTear: canTear, commit: commit))
    }

    /// Collects the tabs' places for a Reorder (on the row or column that holds them). [order]: the
    /// tabs' ids as shown; a change while dragging (a tab closed, the server's view) puts it back.
    func reorderSpace(_ r: Reorder, name: String, order: [String]) -> some View {
        #if DEBUG
        Reorder.live[name] = r
        #endif
        return coordinateSpace(name: name)
            .onPreferenceChange(HostFrameKey.self) { frames in Reorder.tabRects[name] = Array(frames.values) }
            .onPreferenceChange(FrameKey.self) { frames in
                // Only at rest: while dragging, the tabs stand aside and their frames move with them.
                guard r.atRest else { return }
                r.frames = frames.mapValues { r.horizontal ? ($0.minX, $0.width) : ($0.minY, $0.height) }
            }
            .onChange(of: order) { _, now in if r.dragging != nil, now != r.order { r.cancel() } }
            // Gone (the other tab layout chosen): its tabs mustn't keep the window from moving there.
            .onDisappear { Reorder.tabRects[name] = nil }
    }
}

private struct ReorderItem: ViewModifier {
    let id: String
    @ObservedObject var r: Reorder
    let order: () -> [String]
    let space: String
    let canTear: () -> Bool
    let commit: (String, Int) -> Void
    /// Pressed: false again however the gesture ends, the system cancelling it included (onEnded isn't called then).
    @GestureState private var pressed = false

    func body(content: Content) -> some View {
        let dragged = r.dragging == id
        let lifted = dragged || r.settling == id
        let move = lifted ? r.offset : r.shift(of: id)
        content
            .background(GeometryReader { g in
                Color.clear.preference(key: FrameKey.self, value: [id: g.frame(in: .named(space))])
                    .preference(key: HostFrameKey.self, value: [id: g.frame(in: .global)])
            })
            .offset(x: r.horizontal ? move : 0, y: r.horizontal ? 0 : move)
            .scaleEffect(dragged ? 1.03 : 1)
            .shadow(color: .black.opacity(dragged ? 0.22 : 0), radius: dragged ? 8 : 0, y: dragged ? 3 : 0)
            .zIndex(lifted ? 1 : 0)
            // Following the pointer, no lag; let go, it springs home (the settle's own animation).
            .animation(dragged ? nil : .spring(response: 0.28, dampingFraction: 0.86), value: move)
            .gesture(
                DragGesture(minimumDistance: 5, coordinateSpace: .named(space))
                    .updating($pressed) { _, s, _ in s = true }
                    .onChanged { v in
                        if r.tearing == id { TearDrag.shared.move(); return }
                        if r.dragging == nil, r.tearing == nil { r.begin(id, order: order()) }
                        guard r.dragging == id else { return }
                        let across = r.horizontal ? v.translation.height : v.translation.width
                        if TearOff.tears(across: across, horizontalRow: r.horizontal), canTear() {
                            r.startTear(id)
                            TearDrag.shared.begin(id)
                            return
                        }
                        r.move(by: r.horizontal ? v.translation.width : v.translation.height)
                    }
                    .onEnded { v in
                        if r.tearing == id {
                            // Let go back over the row: it stays where it is.
                            let across = r.horizontal ? v.translation.height : v.translation.width
                            r.endTear()
                            TearDrag.shared.drop(commit: TearOff.tears(across: across, horizontalRow: r.horizontal))
                        } else if r.dragging == id {
                            r.finish(commit: commit)
                        }
                    }
            )
            .onChange(of: pressed) { _, now in if !now { r.gestureEnded() } }
    }
}
