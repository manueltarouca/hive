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
    /// Why the requested family wasn't used, if it wasn't.
    public let problem: String?

    /// The bundled JetBrains Mono.
    public convenience init(pointSize: CGFloat = 13, scale: CGFloat = 2) {
        self.init(family: "", pointSize: pointSize, scale: scale)
    }

    /// A monospaced family installed here (font-family), else the bundled
    /// JetBrains Mono. Either way the bundled font is in each face's cascade
    /// list, so Nerd Font icons agent TUIs draw still have a glyph.
    public init(family: String, pointSize: CGFloat, scale: CGFloat) {
        self.pointSize = pointSize
        self.scale = scale
        let px = pointSize * scale
        func descriptor(_ name: String) -> CTFontDescriptor? {
            guard let url = FontSet.resources?.url(forResource: name, withExtension: "ttf", subdirectory: "fonts"),
                  let descs = CTFontManagerCreateFontDescriptorsFromURL(url as CFURL) as? [CTFontDescriptor] else { return nil }
            return descs.first
        }
        func load(_ name: String) -> CTFont? { descriptor(name).map { CTFontCreateWithFontDescriptor($0, px, nil) } }
        let system = CTFontCreateUIFontForLanguage(.userFixedPitch, px, nil) ?? CTFontCreateWithName("Menlo" as CFString, px, nil)

        var chosen: CTFont?
        var problem: String?
        if !family.isEmpty {
            let attrs = [kCTFontFamilyNameAttribute: family] as CFDictionary
            let f = CTFontCreateWithFontDescriptor(CTFontDescriptorCreateWithAttributes(attrs), px, nil)
            if (CTFontCopyFamilyName(f) as String).caseInsensitiveCompare(family) != .orderedSame {
                problem = "font-family \"\(family)\" isn't installed here; using JetBrains Mono"
            } else if !FontSet.isMonospaced(f) {
                problem = "font-family \"\(family)\" isn't a monospaced font; using JetBrains Mono"
            } else {
                chosen = f
            }
        }
        self.problem = problem
        if let chosen {
            // The bundled font last in the cascade: icons the family lacks.
            let cascade = [descriptor("JetBrainsMonoNerdFont-Regular")].compactMap { $0 } as CFArray
            func withIcons(_ f: CTFont) -> CTFont {
                let d = CTFontDescriptorCreateCopyWithAttributes(CTFontCopyFontDescriptor(f),
                                                                 [kCTFontCascadeListAttribute: cascade] as CFDictionary)
                return CTFontCreateWithFontDescriptor(d, px, nil)
            }
            func styled(_ t: CTFontSymbolicTraits) -> CTFont {
                CTFontCreateCopyWithSymbolicTraits(chosen, px, nil, t, t) ?? chosen
            }
            regular = withIcons(chosen)
            bold = withIcons(styled(.traitBold))
            italic = withIcons(styled(.traitItalic))
            boldItalic = withIcons(styled([.traitBold, .traitItalic]))
        } else {
            regular = load("JetBrainsMonoNerdFont-Regular") ?? system
            bold = load("JetBrainsMonoNerdFont-Bold") ?? CTFontCreateCopyWithSymbolicTraits(regular, px, nil, .traitBold, .traitBold) ?? regular
            italic = load("JetBrainsMonoNerdFont-Italic") ?? CTFontCreateCopyWithSymbolicTraits(regular, px, nil, .traitItalic, .traitItalic) ?? regular
            boldItalic = load("JetBrainsMonoNerdFont-BoldItalic") ?? bold
        }

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

    /// Whether a font is monospaced: the one test the Settings picker and
    /// font-family both use.
    static func isMonospaced(_ f: CTFont) -> Bool { CTFontGetSymbolicTraits(f).contains(.traitMonoSpace) }

    /// The installed families font-family accepts, for the Settings picker.
    public static func monospacedFamilies() -> [String] {
        let names = CTFontManagerCopyAvailableFontFamilyNames() as? [String] ?? []
        return names.filter { family in
            let attrs = [kCTFontFamilyNameAttribute: family] as CFDictionary
            let f = CTFontCreateWithFontDescriptor(CTFontDescriptorCreateWithAttributes(attrs), 13, nil)
            return (CTFontCopyFamilyName(f) as String) == family && isMonospaced(f)
        }.sorted()
    }

    /// The resource bundle, found without Bundle.module, which crashes when
    /// the bundle isn't where SwiftPM expects it (inside an .app).
    static let resources: Bundle? = {
        let name = "Hive_HiveRender.bundle"
        let candidates = [Bundle.main.resourceURL, Bundle.main.bundleURL, Bundle(for: FontSet.self).resourceURL,
                          Bundle(for: FontSet.self).bundleURL.deletingLastPathComponent()]
        for case let dir? in candidates {
            if let b = Bundle(url: dir.appendingPathComponent(name)) { return b }
        }
        return nil
    }()

    func face(bold b: Bool, italic i: Bool) -> CTFont {
        switch (b, i) {
        case (true, true): return boldItalic
        case (true, false): return bold
        case (false, true): return italic
        default: return regular
        }
    }
}
