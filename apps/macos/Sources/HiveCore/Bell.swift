import Foundation

/// What a terminal bell does, by the `bell` setting (flash, sound,
/// flash+sound, none). Rate-limited: one pane rings at most once a second,
/// and the Dock bounces at most once every 10 seconds however many panes
/// ring, so a program ringing in a loop can't flood the screen or the Dock.
public struct BellPolicy: Sendable {
    public enum Effect: Equatable, Sendable {
        case flash   // the pane flashes
        case sound   // the system alert sound
        case mark    // a dot on the pane's tab until it's seen
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
        effects.append(.mark)
        if !appActive, lastBounce.map({ now.timeIntervalSince($0) >= 10 }) ?? true {
            lastBounce = now
            effects.append(.bounce)
        }
        return effects
    }
}

/// Panes showing a bell, like Ghostty's bell `title` feature: a pane that
/// rang keeps its bell until it's focused again or typed into. Viewing the
/// window alone doesn't clear it, so a program that rings and then changes
/// its title (Claude Code going idle) still shows it.
public struct BellMarks: Equatable, Sendable {
    public private(set) var blocks: Set<String> = []

    public init() {}

    public func contains(_ block: String) -> Bool { blocks.contains(block) }
    public mutating func rang(_ block: String) { blocks.insert(block) }
    public mutating func typed(into block: String) { blocks.remove(block) }
    public mutating func focused(_ block: String) { blocks.remove(block) }
    /// Coming back to hive re-focuses its focused pane.
    public mutating func appActivated(focused: String?) { if let focused { blocks.remove(focused) } }
    public mutating func prune(keeping live: Set<String>) { blocks.formIntersection(live) }
}
