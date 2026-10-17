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
    public var diagnostics: [Diagnostic] = []

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
        struct Values: Decodable {
            var theme: String
            var fontFamily: String
            var fontSize: Double
            var bell: String
            var notify: String
            var confirmPaste: Bool
            enum CodingKeys: String, CodingKey {
                case theme, bell, notify
                case fontFamily = "font-family", fontSize = "font-size", confirmPaste = "confirm-paste"
            }
        }
    }

    public static func decode(_ data: Data) throws -> Settings {
        let s = try JSONDecoder().decode(Shown.self, from: data)
        return Settings(path: s.path, theme: Appearance(stored: s.values.theme), fontFamily: s.values.fontFamily,
                        fontSize: s.values.fontSize, bell: s.values.bell, notify: s.values.notify,
                        confirmPaste: s.values.confirmPaste, diagnostics: s.diagnostics)
    }
}
