import CoreGraphics
import CoreText
import Foundation
import Metal

/// Where a glyph lives in an atlas.
struct GlyphSlot {
    var x: Int, y: Int, w: Int, h: Int
    var color: Bool // in the BGRA atlas (emoji), drawn as is
}

struct GlyphKey: Hashable {
    var text: String
    var bold: Bool, italic: Bool
    var wide: Bool
}

/// Two atlases: grayscale coverage for ordinary glyphs, tinted by the cell's
/// color, and BGRA for color glyphs such as emoji. Every glyph is rendered
/// into a tile of exactly its cell size (two cells for wide), so positions
/// are whole pixels and nothing needs bearings at draw time.
final class Atlas {
    let device: MTLDevice
    let fonts: FontSet
    let size: Int
    private(set) var gray: MTLTexture
    private(set) var color: MTLTexture
    private var slots: [GlyphKey: GlyphSlot] = [:]
    private var fallback: [String: CTFont] = [:]

    // Shelf packers, one per atlas.
    private var shelves = [Shelf(), Shelf()]
    struct Shelf { var x = 0, y = 0, rowHeight = 0 }

    init(device: MTLDevice, fonts: FontSet, size: Int = 2048) {
        self.device = device
        self.fonts = fonts
        self.size = size
        func tex(_ format: MTLPixelFormat) -> MTLTexture {
            let d = MTLTextureDescriptor.texture2DDescriptor(pixelFormat: format, width: size, height: size, mipmapped: false)
            d.usage = [.shaderRead]
            d.storageMode = .shared
            return device.makeTexture(descriptor: d)!
        }
        gray = tex(.r8Unorm)
        color = tex(.bgra8Unorm)
    }

    /// The slot for a cell's text, rasterizing it on first use.
    func slot(_ key: GlyphKey) -> GlyphSlot? {
        if let s = slots[key] { return s }
        guard let s = rasterize(key) else { return nil }
        slots[key] = s
        return s
    }

    private func rasterize(_ key: GlyphKey) -> GlyphSlot? {
        let w = fonts.cellWidth * (key.wide ? 2 : 1), h = fonts.cellHeight
        let scalars = key.text.unicodeScalars
        if scalars.count == 1, let u = scalars.first, BoxDrawing.handles(u) {
            return place(w: w, h: h, color: false) { ctx in
                BoxDrawing.draw(u, in: ctx, width: CGFloat(w), height: CGFloat(h), thickness: max(1, (CGFloat(fonts.cellWidth) / 8).rounded()))
            }
        }
        let base = fonts.face(bold: key.bold, italic: key.italic)
        let font = fontFor(key.text, base: base)
        let isColor = CTFontGetSymbolicTraits(font).contains(.traitColorGlyphs)
        let attr = NSAttributedString(string: key.text, attributes: [
            NSAttributedString.Key(kCTFontAttributeName as String): font,
            NSAttributedString.Key(kCTForegroundColorFromContextAttributeName as String): true,
        ])
        let line = CTLineCreateWithAttributedString(attr)
        // Center glyphs narrower than their cells (fallback fonts, emoji).
        let lineWidth = CGFloat(CTLineGetTypographicBounds(line, nil, nil, nil))
        var dx: CGFloat = 0
        if lineWidth > 0, lineWidth < CGFloat(w) - 1 || font != base { dx = ((CGFloat(w) - lineWidth) / 2).rounded() }
        if lineWidth > CGFloat(w) { dx = 0 }
        return place(w: w, h: h, color: isColor) { ctx in
            ctx.setFillColor(CGColor(gray: 1, alpha: 1))
            ctx.textPosition = CGPoint(x: dx, y: fonts.baseline)
            if isColor, lineWidth > CGFloat(w) {
                // Scale color glyphs that are wider than their cells.
                let s = CGFloat(w) / lineWidth
                ctx.scaleBy(x: s, y: s)
                ctx.textPosition = CGPoint(x: 0, y: fonts.baseline / s)
            }
            CTLineDraw(line, ctx)
        }
    }

    private func fontFor(_ text: String, base: CTFont) -> CTFont {
        // The base font covers almost everything; only ask CoreText for a
        // fallback when it lacks a glyph, and remember the answer.
        var chars = Array(text.utf16)
        var glyphs = [CGGlyph](repeating: 0, count: chars.count)
        if CTFontGetGlyphsForCharacters(base, &chars, &glyphs, chars.count) { return base }
        let k = text + (base == fonts.regular ? "" : "\u{0}\(CTFontCopyPostScriptName(base))")
        if let f = fallback[k] { return f }
        let f = CTFontCreateForString(base, text as CFString, CFRange(location: 0, length: text.utf16.count))
        fallback[k] = f
        return f
    }

    private func place(w: Int, h: Int, color: Bool, draw: (CGContext) -> Void) -> GlyphSlot? {
        let which = color ? 1 : 0
        var s = shelves[which]
        let pw = w + 1, ph = h + 1 // 1 px padding against bleeding
        if s.x + pw > size { s.x = 0; s.y += s.rowHeight; s.rowHeight = 0 }
        guard s.y + ph <= size else { return nil } // full; TODO: evict
        let slot = GlyphSlot(x: s.x, y: s.y, w: w, h: h, color: color)
        s.x += pw
        s.rowHeight = max(s.rowHeight, ph)
        shelves[which] = s

        let bytesPerPixel = color ? 4 : 1
        var pixels = [UInt8](repeating: 0, count: w * h * bytesPerPixel)
        let ok: Bool = pixels.withUnsafeMutableBytes { buf in
            let ctx: CGContext?
            if color {
                ctx = CGContext(data: buf.baseAddress, width: w, height: h, bitsPerComponent: 8, bytesPerRow: w * 4,
                                space: CGColorSpace(name: CGColorSpace.sRGB)!,
                                bitmapInfo: CGImageAlphaInfo.premultipliedFirst.rawValue | CGBitmapInfo.byteOrder32Little.rawValue)
            } else {
                ctx = CGContext(data: buf.baseAddress, width: w, height: h, bitsPerComponent: 8, bytesPerRow: w,
                                space: CGColorSpaceCreateDeviceGray(), bitmapInfo: CGImageAlphaInfo.none.rawValue)
            }
            guard let ctx else { return false }
            ctx.setAllowsFontSmoothing(false)
            ctx.setShouldAntialias(true)
            ctx.setAllowsFontSubpixelPositioning(false)
            ctx.setShouldSubpixelPositionFonts(false)
            draw(ctx)
            return true
        }
        guard ok else { return nil }
        // CoreGraphics draws bottom-up into memory top-down, which matches
        // texture rows: row 0 is the top.
        let tex = color ? self.color : gray
        tex.replace(region: MTLRegionMake2D(slot.x, slot.y, w, h), mipmapLevel: 0,
                    withBytes: pixels, bytesPerRow: w * bytesPerPixel)
        return slot
    }
}
