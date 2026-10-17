import Foundation
import Testing
@testable import HiveCore

/// The app reads settings by running its bundled `hive config show --json`:
/// one parser for the server, the CLI and the app.
@Suite struct SettingsTests {
    /// Real output of `hive config show --json` (a file with theme = night
    /// and font-size = 99).
    static let shown = #"{"diagnostics":[{"line":2,"key":"font-size","level":"error","message":"font-size must be a number from 8 to 32 (got \"99\")"}],"path":"/var/folders/4b/_vcbpkm54pg429ytnhjg6lt40000gn/T/tmp.l3YlGHJXhb/config","values":{"bell":"flash","confirm-paste":true,"font-family":"","font-size":13,"notify":"background","resume-agents":true,"scrollback-limit":16777216,"theme":"night"}}"#

    @Test func decodesTheCLIsOutput() throws {
        let s = try Settings.decode(Data(Self.shown.utf8))
        #expect(s.theme == .night)
        #expect(s.fontSize == 13)
        #expect(s.bell == "flash")
        #expect(s.notify == "background")
        #expect(s.confirmPaste)
        #expect(s.diagnostics.count == 1)
        #expect(s.diagnostics.first?.line == 2)
        #expect(s.banner == "Settings, line 2: font-size must be a number from 8 to 32 (got \"99\")")
    }

    /// Without hive's binary or a readable answer, the defaults.
    @Test func defaultsWhenUnreadable() {
        #expect((try? Settings.decode(Data("nonsense".utf8))) == nil)
        let d = Settings.defaults
        #expect(d.theme == .system && d.bell == "flash" && d.confirmPaste && d.banner == nil)
    }
}
