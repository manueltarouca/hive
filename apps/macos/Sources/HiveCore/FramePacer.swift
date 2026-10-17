import Foundation

/// Builds a pane's frames at most as often as a display shows them (120 Hz,
/// ProMotion). Building one per output chunk took a core at 16 MB/s for a
/// 300x80 pane: up to 5,700 frames a second, 50 times what any screen
/// shows (hivebench-app engine). The first frame after a quiet spell is
/// built at once, so typing feels the same.
public struct FramePacer: Sendable {
    public static let interval: TimeInterval = 1.0 / 120
    private var last: TimeInterval = -.infinity

    public init() {}

    /// How long to wait before building a frame asked for at now.
    public func delay(at now: TimeInterval) -> TimeInterval { max(0, last + Self.interval - now) }

    public mutating func built(at now: TimeInterval) { last = now }
}
