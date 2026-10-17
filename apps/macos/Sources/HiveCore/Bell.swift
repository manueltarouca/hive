import Foundation

/// What a terminal bell does, by the `bell` setting (flash, sound,
/// flash+sound, none). Rate-limited: one pane rings at most once a second,
/// and the Dock bounces at most once every 10 seconds however many panes
/// ring, so a program ringing in a loop can't flood the screen or the Dock.
public struct BellPolicy: Sendable {
    public enum Effect: Equatable, Sendable {
        case flash   // the pane flashes
        case sound   // the system alert sound
        case bounce  // the Dock icon, when hive isn't in front
    }

    private var lastRing: [String: Date] = [:]
    private var lastBounce: Date?

    public init() {}

    /// The panes it remembers a ring for.
    public var remembered: Set<String> { Set(lastRing.keys) }

    /// Forgets closed panes, so its memory doesn't grow forever.
    public mutating func prune(keeping live: Set<String>) {
        lastRing = lastRing.filter { live.contains($0.key) }
    }

    public mutating func ring(block: String, setting: String, appActive: Bool, now: Date = Date()) -> [Effect] {
        guard setting != "none" else { return [] }
        if let last = lastRing[block], now.timeIntervalSince(last) < 1 { return [] }
        lastRing[block] = now
        var effects: [Effect] = []
        if setting.contains("flash") { effects.append(.flash) }
        if setting.contains("sound") { effects.append(.sound) }
        if !appActive, lastBounce.map({ now.timeIntervalSince($0) >= 10 }) ?? true {
            lastBounce = now
            effects.append(.bounce)
        }
        return effects
    }
}
