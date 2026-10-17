import Testing
@testable import HiveCore

@Suite struct AppearanceTests {
    @Test func settingThenSystem() {
        #expect(Appearance.night.isNight(env: nil, systemDark: false))
        #expect(!Appearance.day.isNight(env: nil, systemDark: true))
        #expect(Appearance.system.isNight(env: nil, systemDark: true))
        #expect(!Appearance.system.isNight(env: nil, systemDark: false))
    }

    /// HIVE_STYLE is for the screenshot loop, and wins over everything.
    @Test func environmentWins() {
        #expect(!Appearance.night.isNight(env: "light", systemDark: true))
        #expect(Appearance.day.isNight(env: "dark", systemDark: false))
        #expect(Appearance.day.isNight(env: "other", systemDark: true) == false)
    }

    @Test func storedValuesRoundTrip() {
        for a in Appearance.allCases { #expect(Appearance(stored: a.rawValue) == a) }
        #expect(Appearance(stored: nil) == .system)
        #expect(Appearance(stored: "bogus") == .system)
    }
}
