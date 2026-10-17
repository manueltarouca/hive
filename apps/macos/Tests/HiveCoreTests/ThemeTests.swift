import Testing
@testable import HiveCore

@Suite struct ThemeTests {
    /// The default theme is Ghostty's built-in one (`ghostty +show-config
    /// --default`), so hive looks like the user's terminal.
    @Test func defaultMatchesGhostty() {
        let t = Theme.ghostty
        #expect(t.background == RGB(0x28, 0x2c, 0x34))
        #expect(t.foreground == RGB(0xff, 0xff, 0xff))
        let hex: [UInt32] = [
            0x1d1f21, 0xcc6666, 0xb5bd68, 0xf0c674, 0x81a2be, 0xb294bb, 0x8abeb7, 0xc5c8c6,
            0x666666, 0xd54e53, 0xb9ca4a, 0xe7c547, 0x7aa6da, 0xc397d8, 0x70c0b1, 0xeaeaea,
        ]
        #expect(t.palette == hex.map { RGB(UInt8($0 >> 16), UInt8(($0 >> 8) & 0xff), UInt8($0 & 0xff)) })
    }
}
