/// The order tabs are shown in, on the Mac and the phone: pinned tabs
/// first, then the rest, each keeping the session's order.
public enum TabOrder {
    /// The session indices of the tabs, in the order shown.
    public static func shown(pinned: [Bool]) -> [Int] {
        let all = Array(pinned.indices)
        return all.filter { pinned[$0] } + all.filter { !pinned[$0] }
    }

    /// The place `by` steps from `from`, wrapping around `count`.
    public static func step(from: Int, by: Int, count: Int) -> Int {
        ((from + by) % count + count) % count
    }
}
