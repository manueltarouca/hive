import CoreText
import Foundation

/// The terminal font in four faces, at one pixel size, with cell metrics
/// rounded to whole pixels so glyphs sit on the grid.
public final class FontSet {
    public let regular, bold, italic, boldItalic: CTFont
    public let pointSize: CGFloat
    public let scale: CGFloat
    /// Cell size in pixels.
    public let cellWidth: Int, cellHeight: Int
    /// Distance from the cell's bottom to the baseline, in pixels.
    public let baseline: CGFloat
    public let underlinePosition: CGFloat, underlineThickness: CGFloat

    /// Loads the bundled JetBrains Mono, falling back to the system's
    /// monospaced font if it can't be found.
    public init(pointSize: CGFloat = 13, scale: CGFloat = 2) {
        self.pointSize = pointSize
        self.scale = scale
        let px = pointSize * scale
        func load(_ name: String) -> CTFont? {
            guard let url = Bundle.module.url(forResource: name, withExtension: "ttf", subdirectory: "fonts"),
                  let descs = CTFontManagerCreateFontDescriptorsFromURL(url as CFURL) as? [CTFontDescriptor],
                  let d = descs.first else { return nil }
            return CTFontCreateWithFontDescriptor(d, px, nil)
        }
        let system = CTFontCreateUIFontForLanguage(.userFixedPitch, px, nil) ?? CTFontCreateWithName("Menlo" as CFString, px, nil)
        regular = load("JetBrainsMonoNerdFont-Regular") ?? system
        bold = load("JetBrainsMonoNerdFont-Bold") ?? CTFontCreateCopyWithSymbolicTraits(regular, px, nil, .traitBold, .traitBold) ?? regular
        italic = load("JetBrainsMonoNerdFont-Italic") ?? CTFontCreateCopyWithSymbolicTraits(regular, px, nil, .traitItalic, .traitItalic) ?? regular
        boldItalic = load("JetBrainsMonoNerdFont-BoldItalic") ?? bold

        var glyph = CGGlyph()
        var ch: UniChar = 0x4D // "M"
        CTFontGetGlyphsForCharacters(regular, &ch, &glyph, 1)
        var advance = CGSize.zero
        CTFontGetAdvancesForGlyphs(regular, .horizontal, &glyph, &advance, 1)
        let ascent = CTFontGetAscent(regular), descent = CTFontGetDescent(regular), leading = CTFontGetLeading(regular)
        cellWidth = Int(advance.width.rounded())
        cellHeight = Int((ascent + descent + leading).rounded(.up))
        baseline = (descent + leading / 2).rounded()
        underlineThickness = max(1, CTFontGetUnderlineThickness(regular).rounded())
        underlinePosition = max(1, (baseline + CTFontGetUnderlinePosition(regular)).rounded())
    }

    func face(bold b: Bool, italic i: Bool) -> CTFont {
        switch (b, i) {
        case (true, true): return boldItalic
        case (true, false): return bold
        case (false, true): return italic
        default: return regular
        }
    }
}
