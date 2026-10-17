import Testing
@testable import HiveCore

@Suite struct ShortcutGuideTests {
    /// Modifiers in Apple's order (⌃⌥⇧⌘), then the key as macOS draws it.
    @Test func keysDisplayAsMacOSShowsThem() {
        #expect(ShortcutText.display(key: "t", control: false, option: false, shift: false, command: true) == "⌘T")
        #expect(ShortcutText.display(key: "d", control: false, option: false, shift: true, command: true) == "⇧⌘D")
        #expect(ShortcutText.display(key: "\u{F702}", control: false, option: true, shift: false, command: true) == "⌥⌘←")
        #expect(ShortcutText.display(key: "\u{F701}", control: true, option: false, shift: false, command: true) == "⌃⌘↓")
        #expect(ShortcutText.display(key: "\r", control: false, option: false, shift: true, command: true) == "⇧⌘↩")
        #expect(ShortcutText.display(key: "]", control: false, option: false, shift: true, command: true) == "⇧⌘]")
        #expect(ShortcutText.display(key: "/", control: false, option: false, shift: false, command: true) == "⌘/")
        // Keys whose capital is two letters stay as they are.
        #expect(ShortcutText.display(key: "ß", control: false, option: false, shift: false, command: true) == "⌘ß")
    }

    /// Every editing key the guide lists really is bound, so the guide can't
    /// promise a key that does nothing.
    @Test func editingKeysMatchTheBindings() {
        #expect(!ShortcutGuide.editing.isEmpty)
        for row in ShortcutGuide.editing {
            #expect(MacBindings.text(key: row.key, mods: row.mods) != nil, "\(row.title) isn't bound")
        }
    }

    @Test func mouseGesturesAreListed() {
        let titles = ShortcutGuide.mouse.map(\.title)
        #expect(titles.contains { $0.contains("Select in hive") })
        #expect(ShortcutGuide.mouse.contains { $0.keys == "⇧ Drag" })
    }
}

@Suite struct MenuShortcutTests {
    func item(_ title: String, _ key: String, _ mods: KeyMods = .command, function: Bool = false) -> MenuShortcut {
        MenuShortcut(title: title, key: key, mods: mods, function: function)
    }

    /// Tab 1…9 fold into one row; titles lose their ellipsis.
    @Test func numberedTabsFoldIntoOneRow() {
        let rows = ShortcutGuide.rows((1...9).map { item("Tab \($0)", "\($0)") } + [item("Change Session…", "k")])
        #expect(rows.map(\.title) == ["Change Session", "Go to tab 1–9"])
        #expect(rows.last?.keys == "⌘1 … ⌘9")
    }

    /// AppKit adds items it draws itself: fn shortcuts and keys like the
    /// dictation microphone can't be shown as text, so they're left out.
    @Test func itemsThatCantBeDrawnAreSkipped() {
        let rows = ShortcutGuide.rows([
            item("Start Dictation…", "\u{1F3A4}", []),
            item("Enter Full Screen", "f", [], function: true),
            item("Emoji & Symbols", " ", [.command, .ctrl], function: true),
            item("Copy", "c"),
        ])
        #expect(rows.map(\.title) == ["Copy"])
    }

    /// AppKit localizes keys for the layout: on a Portuguese one ⇧⌘[ is
    /// drawn ⇧⌘Ç. Those letters are real shortcuts and stay.
    @Test func localizedKeysStay() {
        let rows = ShortcutGuide.rows([item("Previous Tab", "Ç", [.command, .shift]), item("Next Tab", "~", [.command, .shift])])
        #expect(rows.map(\.keys) == ["⇧⌘Ç", "⇧⌘~"])
    }

    /// An uppercase key equivalent means Shift, as AppKit draws it.
    @Test func uppercaseKeyMeansShift() {
        #expect(ShortcutGuide.rows([item("Jump", "J")]).first?.keys == "⇧⌘J")
    }

    /// Sections fill the left column, then the right, in order, split where
    /// the taller column is shortest.
    @Test func columnsKeepOrder() {
        // Heights with headings: 5 4 11 3 8 3 6 = 40; best thirds 9 | 14 | 17.
        #expect(ShortcutGuide.columns([3, 2, 9, 1, 6, 1, 4], count: 3) == [0, 0, 1, 1, 2, 2, 2])
        #expect(ShortcutGuide.columns([3, 2, 9, 1, 6, 1, 4], count: 2) == [0, 0, 0, 1, 1, 1, 1])
        #expect(ShortcutGuide.columns([5], count: 3) == [0])
        #expect(ShortcutGuide.columns([], count: 3) == [])
        #expect(ShortcutGuide.columns([2, 3], count: 0) == [0, 0])
    }

    @Test func filterMatchesModifierWords() {
        let row = ShortcutRow(title: "Split Down", keys: "⇧⌘D")
        #expect(ShortcutGuide.matches(row, "split"))
        #expect(ShortcutGuide.matches(row, "shift cmd d"))
        #expect(!ShortcutGuide.matches(row, "option"))
    }
}
