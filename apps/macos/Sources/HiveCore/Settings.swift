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
    /// How tabs, panes and the sidebar move.
    public var motion = Motion.snappy
    /// When closing a tab, or its last pane, asks first.
    public var confirmClose = CloseGuard.Policy.running
    /// How much the panes you're not typing in step back: light, more, off.
    public var dimPanes = "light"
    /// Even out a tab's panes when the app opens or closes one.
    public var balanceSplits = true
    /// Focus a pane when the pointer moves over it.
    public var focusFollowsPointer = false
    /// cursor-style-blink: nil blinks unless a program says otherwise.
    public var cursorStyleBlink: Bool?
    /// Read by the server; shown and set in the Settings window.
    public var scrollbackLimit: UInt64 = 16 << 20
    public var resumeAgents = true
    /// Listeners the server adds when it starts (tailnet://:4483).
    public var listen: [String] = []
    /// Scripts run on the server; others may run them over Tailscale.
    public var scripts = true
    public var scriptRemoteRun = false
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
            var cursorStyleBlink: Bool?
            var scrollbackLimit: UInt64
            var resumeAgents: Bool
            // Newer keys: optional, so a settings file read by an older hive still decodes.
            var motion: String?
            var confirmClose: String?
            var dimPanes: String?
            var balanceSplits: Bool?
            var focusFollowsPointer: Bool?
            var listen: [String]?
            var scripts: Bool?
            var scriptRemoteRun: Bool?
            enum CodingKeys: String, CodingKey {
                case theme, bell, notify
                case fontFamily = "font-family", fontSize = "font-size", confirmPaste = "confirm-paste", cursorStyleBlink = "cursor-style-blink"
                case scrollbackLimit = "scrollback-limit", resumeAgents = "resume-agents"
                case dimPanes = "dim-panes", balanceSplits = "balance-splits", focusFollowsPointer = "focus-follows-pointer"
                case motion, confirmClose = "confirm-close", listen, scripts, scriptRemoteRun = "script-remote-run"
            }
        }
    }

    public static func decode(_ data: Data) throws -> Settings {
        let s = try JSONDecoder().decode(Shown.self, from: data)
        return Settings(path: s.path, theme: Appearance(stored: s.values.theme), fontFamily: s.values.fontFamily,
                        fontSize: s.values.fontSize, bell: s.values.bell, notify: s.values.notify,
                        confirmPaste: s.values.confirmPaste,
                        motion: s.values.motion.flatMap(Motion.init(rawValue:)) ?? .snappy,
                        confirmClose: s.values.confirmClose.flatMap(CloseGuard.Policy.init(rawValue:)) ?? .running,
                        dimPanes: s.values.dimPanes ?? "light", balanceSplits: s.values.balanceSplits ?? true,
                        focusFollowsPointer: s.values.focusFollowsPointer ?? false,
                        cursorStyleBlink: s.values.cursorStyleBlink, scrollbackLimit: s.values.scrollbackLimit,
                        resumeAgents: s.values.resumeAgents, listen: s.values.listen ?? [],
                        scripts: s.values.scripts ?? true, scriptRemoteRun: s.values.scriptRemoteRun ?? false, diagnostics: s.diagnostics, keys: s.keys ?? [])
    }
}
