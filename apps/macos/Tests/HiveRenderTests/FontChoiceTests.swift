import CoreText
import Testing
@testable import HiveRender

/// font-family and font-size: a monospaced family installed here, else the
/// bundled JetBrains Mono, with a reason. Nerd Font icons draw either way.
@Suite struct FontChoiceTests {
    func family(_ f: CTFont) -> String { CTFontCopyFamilyName(f) as String }

    @Test func aMonospacedFamilyLoads() {
        let f = FontSet(family: "Menlo", pointSize: 13, scale: 2)
        #expect(family(f.regular) == "Menlo")
        #expect(f.problem == nil)
        #expect(CTFontGetSymbolicTraits(f.bold).contains(.traitBold))
    }

    @Test func otherFamiliesFallBackWithAReason() {
        let proportional = FontSet(family: "Helvetica", pointSize: 13, scale: 2)
        #expect(family(proportional.regular).contains("JetBrains"))
        #expect(proportional.problem?.contains("monospaced") == true)
        let missing = FontSet(family: "No Such Font 4711", pointSize: 13, scale: 2)
        #expect(family(missing.regular).contains("JetBrains"))
        #expect(missing.problem?.contains("isn't installed") == true)
        let bundled = FontSet(family: "", pointSize: 13, scale: 2)
        #expect(family(bundled.regular).contains("JetBrains") && bundled.problem == nil)
    }

    @Test func sizeScalesTheCell() {
        let small = FontSet(family: "", pointSize: 12, scale: 2), big = FontSet(family: "", pointSize: 18, scale: 2)
        #expect(big.cellWidth > small.cellWidth && big.cellHeight > small.cellHeight)
    }

    /// The powerline arrow (U+E0B0) is a Nerd Font glyph: with Menlo, it
    /// comes from the bundled font through the cascade.
    @Test func nerdIconsStillDraw() {
        let f = FontSet(family: "Menlo", pointSize: 13, scale: 2)
        let s = "\u{E0B0}" as CFString
        let used = CTFontCreateForString(f.regular, s, CFRange(location: 0, length: 1))
        #expect(family(used).contains("JetBrains"))
    }

    @Test func rendererSwapsFonts() throws {
        let r = try Renderer(fonts: FontSet(family: "", pointSize: 13, scale: 2))
        let before = r.fonts.cellWidth
        r.setFonts(FontSet(family: "", pointSize: 20, scale: 2))
        #expect(r.fonts.cellWidth > before)
    }
}

/// The Settings picker and FontSet agree (Fable's review): a family the
/// picker offers is one FontSet accepts, whatever its capitalisation.
@Suite struct MonospacedFamilyTests {
    @Test func pickerAndFontSetAgree() {
        let families = FontSet.monospacedFamilies()
        #expect(families.contains("Menlo"))
        #expect(!families.contains("Helvetica"))
        for family in families.prefix(40) {
            #expect(FontSet(family: family, pointSize: 13, scale: 2).problem == nil, "\(family)")
        }
        #expect(CTFontCopyFamilyName(FontSet(family: "menlo", pointSize: 13, scale: 2).regular) as String == "Menlo")
    }
}
