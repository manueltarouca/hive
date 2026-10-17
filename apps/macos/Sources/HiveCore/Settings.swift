import Foundation

/// The settings the app uses, as `hive config show --json` reports them:
/// the app runs its bundled hive rather than parsing the file itself, so the
/// server, the CLI and the app read it one way.
public struct Settings: Equatable, Sendable {
    public var path = ""
    public var theme = Appearance.system
    public var fontFamily = ""
    public var fontSize = 13.0
    public var bell = "flash"
    public var notify = "background"
    public var confirmPaste = true
    /// Read by the server; shown and set in the Settings window.
    public var scrollbackLimit: UInt64 = 16 << 20
    public var resumeAgents = true
    public var diagnostics: [Diagnostic] = []
    /// Every setting: its name, default, help, and whether the server reads it.
    public var keys: [Key] = []

    public struct Key: Equatable, Sendable, Decodable {
        public var name: String
        public var `default`: String
        public var server: Bool
        public var help: String
        public var choices: [String]?
    }

    public struct Diagnostic: Equatable, Sendable, Decodable {
        public var line: Int
        public var key: String?
        public var level: String
        public var message: String
    }

    public static let defaults = Settings()

    /// What the window says about a problem in the file, or nil.
    public var banner: String? {
        guard let d = diagnostics.first(where: { $0.level == "error" }) ?? diagnostics.first else { return nil }
        let more = diagnostics.count > 1 ? " (and \(diagnostics.count - 1) more: hive config check)" : ""
        return (d.line > 0 ? "Settings, line \(d.line): " : "Settings: ") + d.message + more
    }

    private struct Shown: Decodable {
        var path: String
        var values: Values
        var diagnostics: [Diagnostic]
        var keys: [Key]?
        struct Values: Decodable {
            var theme: String
            var fontFamily: String
            var fontSize: Double
            var bell: String
            var notify: String
            var confirmPaste: Bool
            var scrollbackLimit: UInt64
            var resumeAgents: Bool
            enum CodingKeys: String, CodingKey {
                case theme, bell, notify
                case fontFamily = "font-family", fontSize = "font-size", confirmPaste = "confirm-paste"
                case scrollbackLimit = "scrollback-limit", resumeAgents = "resume-agents"
            }
        }
    }

    public static func decode(_ data: Data) throws -> Settings {
        let s = try JSONDecoder().decode(Shown.self, from: data)
        return Settings(path: s.path, theme: Appearance(stored: s.values.theme), fontFamily: s.values.fontFamily,
                        fontSize: s.values.fontSize, bell: s.values.bell, notify: s.values.notify,
                        confirmPaste: s.values.confirmPaste, scrollbackLimit: s.values.scrollbackLimit,
                        resumeAgents: s.values.resumeAgents, diagnostics: s.diagnostics, keys: s.keys ?? [])
    }
}
