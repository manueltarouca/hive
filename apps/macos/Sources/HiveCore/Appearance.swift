/// The user's choice of theme: Hive Day, Hive Night, or follow macOS.
public enum Appearance: String, CaseIterable, Sendable {
    case system, day, night

    /// A stored value, or System when it is missing or unknown.
    public init(stored: String?) { self = stored.flatMap(Appearance.init(rawValue:)) ?? .system }

    public var title: String {
        switch self {
        case .system: return "Match System"
        case .day: return "Hive Day"
        case .night: return "Hive Night"
        }
    }

    /// Whether to draw Hive Night. HIVE_STYLE (light or dark) wins, for the
    /// screenshot loop; then this setting; then the system appearance.
    public func isNight(env: String?, systemDark: Bool) -> Bool {
        switch env {
        case "dark": return true
        case "light": return false
        default: break
        }
        switch self {
        case .night: return true
        case .day: return false
        case .system: return systemDark
        }
    }
}
