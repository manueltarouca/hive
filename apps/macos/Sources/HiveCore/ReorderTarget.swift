import CoreGraphics

/// Where a dragged tab would land: it passes another tab once its middle crosses that tab's
/// middle, in the direction it moves. Tabs are given by their middles along the axis, in order.
public enum ReorderTarget {
    /// [from]: the dragged tab's place; [middle]: its middle now; [middles]: every tab's middle at rest.
    public static func index(from: Int, middle: CGFloat, middles: [CGFloat?]) -> Int {
        guard middles.indices.contains(from), let start = middles[from] else { return from }
        var t = from
        if middle > start {
            for i in (from + 1)..<max(from + 1, middles.count) {
                guard let m = middles[i], middle > m else { break }
                t = i
            }
        } else if middle < start {
            for i in stride(from: from - 1, through: 0, by: -1) {
                guard let m = middles[i], middle < m else { break }
                t = i
            }
        }
        return t
    }

    /// How far a tab at [i] moves aside while one is dragged [from] to [to]: a step back, forward, or none.
    public static func shift(index i: Int, from: Int, to: Int) -> Int {
        if from < to, i > from, i <= to { return -1 }
        if from > to, i >= to, i < from { return 1 }
        return 0
    }
}
