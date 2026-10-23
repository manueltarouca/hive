/// The order tabs are shown in, on the Mac and the phone: pinned tabs
/// first, then the rest, each keeping the session's order.
public enum TabOrder {
    /// The session indices of the tabs, in the order shown.
    public static func shown(pinned: [Bool]) -> [Int] {
        let all = Array(pinned.indices)
        return all.filter { pinned[$0] } + all.filter { !pinned[$0] }
    }

    /// Moving a tab to place `to` of the shown order (pins first): whether it ends up pinned (it takes
    /// the pinned state of the tab whose place it takes), and the session index to move it to so the
    /// shown order comes out as dropped. `pinned` is in session order; `id` a session index.
    public static func drop(_ id: Int, to: Int, pinned: [Bool]) -> (pinned: Bool, sessionIndex: Int) {
        var order = shown(pinned: pinned)
        guard let from = order.firstIndex(of: id), order.indices.contains(to) else { return (pinned[id], id) }
        let nowPinned = pinned[order[to]]
        order.remove(at: from)
        order.insert(id, at: to)
        // Its neighbours in its own group, in the new shown order: it goes after the one before it,
        // or before the one after it, in the session's order (without it).
        let group = order.filter { $0 == id || pinned[$0] == nowPinned }
        let at = group.firstIndex(of: id)!
        var session = Array(pinned.indices)
        session.remove(at: id)
        if at > 0, let i = session.firstIndex(of: group[at - 1]) { return (nowPinned, i + 1) }
        if at + 1 < group.count, let i = session.firstIndex(of: group[at + 1]) { return (nowPinned, i) }
        return (nowPinned, nowPinned ? 0 : session.count)
    }

    /// The place `by` steps from `from`, wrapping around `count`.
    public static func step(from: Int, by: Int, count: Int) -> Int {
        ((from + by) % count + count) % count
    }
}
