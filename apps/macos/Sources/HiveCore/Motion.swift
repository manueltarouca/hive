import Foundation

/// How tabs, panes and the sidebar move (the motion setting). Reduce Motion
/// in macOS always wins: everything moves at once.
public enum Motion: String, Sendable, CaseIterable {
    case snappy, natural, off

    /// The spring to use, or nil to move at once.
    public func spring(reduceMotion: Bool) -> (response: Double, damping: Double)? {
        if reduceMotion { return nil }
        switch self {
        case .snappy: return (0.26, 1.0)    // settles quickly, no overshoot
        case .natural: return (0.34, 0.78)  // a slight overshoot
        case .off: return nil
        }
    }
}
