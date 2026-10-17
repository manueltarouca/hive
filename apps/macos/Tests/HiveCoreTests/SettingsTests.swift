import Foundation
import Testing
@testable import HiveCore

/// The app reads settings by running its bundled `hive config show --json`:
/// one parser for the server, the CLI and the app.
@Suite struct SettingsTests {
    /// Real output of `hive config show --json` (a file with theme = night
    /// and font-size = 99).
    static let shown = #"{"diagnostics":[{"line":2,"key":"font-size","level":"error","message":"font-size must be a number from 8 to 32 (got \"99\")"}],"keys":[{"name":"theme","default":"system","server":false,"help":"system, day or night","choices":["system","day","night"]},{"name":"font-family","default":"","server":false,"help":"a monospaced font; empty is hive's bundled JetBrains Mono"},{"name":"font-size","default":"13","server":false,"help":"points, 8 to 32"},{"name":"bell","default":"flash","server":false,"help":"when a program rings the bell: flash, sound, flash+sound or none (none also skips the Dock bounce)","choices":["flash","sound","flash+sound","none"]},{"name":"notify","default":"background","server":false,"help":"agent notifications: background (when hive isn't in front), always or never","choices":["background","always","never"]},{"name":"confirm-paste","default":"true","server":false,"help":"ask before pasting lines into a program that would run each one"},{"name":"scrollback-limit","default":"16m","server":true,"help":"history kept per terminal, in bytes (k, m, g); at least 64k. Lowering it drops old lines at once"},{"name":"resume-agents","default":"true","server":true,"help":"resume agents' sessions when the server starts again"}],"path":"/var/folders/4b/_vcbpkm54pg429ytnhjg6lt40000gn/T/tmp.OvJALxeIWb/config","values":{"bell":"flash","confirm-paste":true,"font-family":"","font-size":13,"notify":"background","resume-agents":true,"scrollback-limit":16777216,"theme":"night"}}"#

    @Test func decodesTheCLIsOutput() throws {
        let s = try Settings.decode(Data(Self.shown.utf8))
        #expect(s.theme == .night)
        #expect(s.fontSize == 13)
        #expect(s.bell == "flash")
        #expect(s.notify == "background")
        #expect(s.confirmPaste)
        #expect(s.scrollbackLimit == 16 << 20)
        #expect(s.resumeAgents)
        #expect(s.diagnostics.count == 1)
        #expect(s.diagnostics.first?.line == 2)
        #expect(s.keys.count == 8)
        #expect(s.keys.first { $0.name == "scrollback-limit" }?.server == true)
        #expect(s.keys.first { $0.name == "bell" }?.choices == ["flash", "sound", "flash+sound", "none"])
        #expect(s.banner == "Settings, line 2: font-size must be a number from 8 to 32 (got \"99\")")
    }

    /// Without hive's binary or a readable answer, the defaults.
    @Test func defaultsWhenUnreadable() {
        #expect((try? Settings.decode(Data("nonsense".utf8))) == nil)
        let d = Settings.defaults
        #expect(d.theme == .system && d.bell == "flash" && d.confirmPaste && d.banner == nil)
    }
}
