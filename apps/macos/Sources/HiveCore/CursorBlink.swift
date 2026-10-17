import Foundation

/// Ghostty's cursor blink: shown for 600 ms, hidden for 600 ms, and shown
/// again at once after typing or output, so a cursor only blinks while the
/// program waits.
public struct CursorBlink: Equatable, Sendable {
    public static let interval: TimeInterval = 0.6
    public private(set) var since: TimeInterval

    public init(at now: TimeInterval) { since = now }

    public mutating func reset(at now: TimeInterval) { since = now }

    private func phase(_ now: TimeInterval) -> Int {
        // Rounded to microseconds so 0.6 + 0.6 lands on the boundary.
        Int(((now - since) * 1e6).rounded() / (Self.interval * 1e6))
    }

    public func visible(at now: TimeInterval) -> Bool { now < since || phase(now) % 2 == 0 }

    /// When the cursor next appears or disappears.
    public func nextChange(after now: TimeInterval) -> TimeInterval {
        guard now >= since else { return since + Self.interval }
        return since + Double(phase(now) + 1) * Self.interval
    }
}
