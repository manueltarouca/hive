import Foundation
import Testing
@testable import HiveCore

@Suite struct SettingsIndexTests {
    @Test func everyEntryIsUniqueAndWritten() {
        let ids = SettingsIndex.entries.map(\.id)
        #expect(Set(ids).count == ids.count)
        for e in SettingsIndex.entries {
            #expect(!e.label.isEmpty && e.help.hasSuffix("."))
            // House style: no interpunct or em dash separators on screen.
            #expect(!e.help.contains("·") && !e.help.contains("—"))
        }
    }

    @Test func searchFindsLabelsFirstThenSentences() {
        let r = SettingsIndex.search("paste")
        #expect(r.first?.id == "confirm-paste")
        #expect(SettingsIndex.search("tailscale").map(\.id).contains("tailnet"))
        #expect(SettingsIndex.search("reduce motion").map(\.id) == ["motion"])
        #expect(SettingsIndex.search("font size").first?.id == "font-size")
        #expect(SettingsIndex.search("cursor blink").first?.id == "cursor-style-blink")
        #expect(SettingsIndex.search("  ").isEmpty)
        #expect(SettingsIndex.search("nothing like this").isEmpty)
    }

    @Test func newerKeysDecodeAndOlderFilesStillDo() throws {
        let old = #"{"path":"/x","values":{"theme":"night","font-family":"","font-size":13,"bell":"flash","notify":"background","confirm-paste":true,"scrollback-limit":16777216,"resume-agents":true},"diagnostics":[]}"#
        let a = try Settings.decode(Data(old.utf8))
        #expect(a.motion == .snappy && a.confirmClose == .running && a.listen.isEmpty && a.scripts)
        let new = #"{"path":"/x","values":{"theme":"night","font-family":"","font-size":13,"bell":"flash","notify":"background","confirm-paste":true,"scrollback-limit":16777216,"resume-agents":true,"motion":"off","confirm-close":"never","listen":["tailnet://:4483"],"scripts":false,"script-remote-run":true},"diagnostics":[]}"#
        let b = try Settings.decode(Data(new.utf8))
        #expect(b.motion == .off && b.confirmClose == .never && b.listen == ["tailnet://:4483"] && !b.scripts && b.scriptRemoteRun)
    }
}
