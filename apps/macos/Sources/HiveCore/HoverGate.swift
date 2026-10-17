import CoreGraphics

/// Lets hover move a list's selection only when the pointer itself moved,
/// so rows scrolling under a resting pointer (while arrow keys move the
/// selection) don't take it back.
public struct HoverGate: Sendable {
    private var last: CGPoint?

    public init() {}

    /// Whether a hover at this screen position should select.
    public mutating func accept(pointer p: CGPoint) -> Bool {
        defer { last = p }
        guard let last else { return true }
        return abs(p.x - last.x) >= 1 || abs(p.y - last.y) >= 1
    }
}
