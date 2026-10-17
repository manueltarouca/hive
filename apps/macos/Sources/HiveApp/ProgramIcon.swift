import AppKit
import HiveCore
import SwiftUI

extension NSColor {
    convenience init(hex: UInt32) {
        self.init(srgbRed: CGFloat((hex >> 16) & 0xFF) / 255, green: CGFloat((hex >> 8) & 0xFF) / 255,
                  blue: CGFloat(hex & 0xFF) / 255, alpha: 1)
    }
}

/// Icons hive draws, decoded once.
@MainActor
enum ProgramImages {
    private static var logos: [String: NSImage] = [:]
    private static var vendors: [String: NSImage] = [:]

    /// A Simple Icons logo as a template, so it takes any colour.
    static func logo(_ slug: String) -> NSImage? {
        if let i = logos[slug] { return i }
        guard let icon = BrandIcon.all[slug], let i = NSImage(data: Data(icon.svg.utf8)) else { return nil }
        i.isTemplate = true
        logos[slug] = i
        return i
    }

    /// A program's own icon from its vendor.
    static func vendor(_ name: String) -> NSImage? {
        if let i = vendors[name] { return i }
        guard let icon = VendorIcon.all[name], let data = Data(base64Encoded: icon.png), let i = NSImage(data: data) else { return nil }
        vendors[name] = i
        return i
    }
}

/// A honeycomb cell: a hexagon standing on a point, with rounded corners.
struct HexCell: Shape {
    /// Corner radius, as a share of the cell's height.
    var rounding: CGFloat = 0.12

    func path(in rect: CGRect) -> Path {
        let h = rect.height, w = min(rect.width, h * 0.866)
        let cx = rect.midX, cy = rect.midY
        let corners = (0..<6).map { i -> CGPoint in
            let a = CGFloat(i) * .pi / 3 - .pi / 2
            // The side corners sit at ±30°, at the rect's left and right edges.
            return CGPoint(x: cx + cos(a) * (w / 2) / 0.866, y: cy + sin(a) * h / 2)
        }
        let r = h * rounding
        var p = Path()
        for i in 0..<6 {
            let prev = corners[(i + 5) % 6], c = corners[i], next = corners[(i + 1) % 6]
            func toward(_ a: CGPoint, _ b: CGPoint, _ d: CGFloat) -> CGPoint {
                let len = hypot(b.x - a.x, b.y - a.y)
                return CGPoint(x: a.x + (b.x - a.x) * d / len, y: a.y + (b.y - a.y) * d / len)
            }
            let start = toward(c, prev, r), end = toward(c, next, r)
            if i == 0 { p.move(to: start) } else { p.addLine(to: start) }
            p.addQuadCurve(to: end, control: c)
        }
        p.closeSubpath()
        return p
    }
}

/// A program as a honeycomb cell, in the tab bar and sidebar: its vendor's
/// icon if hive has one, else its logo on its brand colour, else a symbol on
/// graphite. Other panes' cells sit behind it, to the right.
struct ProgramTile: View {
    let program: Program
    var size: CGFloat = 22
    /// The tab's other panes.
    var behind: [Program] = []

    var body: some View {
        // Each pane behind peeks out by a sliver, a little smaller each time.
        let peek = size * 0.2
        ZStack(alignment: .leading) {
            ForEach(Array(behind.enumerated().reversed()), id: \.offset) { i, p in
                let k = CGFloat(i + 1)
                CellFace(program: p, size: size * (1 - 0.12 * k))
                    .offset(x: peek * k + size * 0.866 * 0.06 * k)
                    .brightness(-0.12)
            }
            CellFace(program: program, size: size)
        }
        .frame(width: size * 0.866 + peek * CGFloat(behind.count) * 1.05, height: size, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(program.name)
    }
}

/// One cell, with the depth of an app icon: rim, gloss, outline, shadow.
private struct CellFace: View {
    let program: Program
    let size: CGFloat

    var body: some View {
        let cell = HexCell()
        let w = size * 0.866
        ZStack {
            if let name = program.vendor, let image = ProgramImages.vendor(name) {
                if let bg = VendorIcon.all[name]?.background {
                    cell.fill(Color(nsColor: NSColor(hex: bg)))
                    Image(nsImage: image).resizable().interpolation(.high).scaledToFit()
                        .frame(width: w * 0.62, height: w * 0.62)
                } else {
                    Image(nsImage: image).resizable().interpolation(.high).scaledToFill()
                        .frame(width: size, height: size)
                }
            } else {
                let (fill, glyph) = colors
                cell.fill(Color(nsColor: fill))
                ProgramGlyph(program: program, color: Color(nsColor: glyph), size: w * 0.56)
            }
            // Gloss: light from above, fading by the middle.
            cell.fill(LinearGradient(stops: [.init(color: .white.opacity(0.22), location: 0),
                                              .init(color: .white.opacity(0.0), location: 0.5),
                                              .init(color: .black.opacity(0.10), location: 1)],
                                      startPoint: .top, endPoint: .bottom))
        }
        .frame(width: w, height: size)
        .clipShape(cell)
        .overlay(cell.stroke(.black.opacity(0.35), lineWidth: 0.75))
        .overlay(cell.inset(size: 0.9).stroke(.white.opacity(0.18), lineWidth: 0.5))
        .shadow(color: .black.opacity(0.28), radius: 1, y: 0.75)
    }

    private var colors: (fill: NSColor, glyph: NSColor) {
        if case .brand(let slug) = program.icon, BrandIcon.all[slug] != nil {
            let fill = BrandIcon.fill(slug)
            return (NSColor(hex: fill), NSColor(hex: Contrast.onFill(fill)))
        }
        return (NSColor(hex: 0x2C2D31), .white)
    }
}

extension HexCell {
    /// The same cell, smaller by a margin in points, for the inner rim.
    func inset(size margin: CGFloat) -> some Shape {
        InsetHex(base: self, margin: margin)
    }
}

private struct InsetHex: Shape {
    let base: HexCell
    let margin: CGFloat
    func path(in rect: CGRect) -> Path { base.path(in: rect.insetBy(dx: margin * 0.866, dy: margin)) }
}

/// A program's mark alone, in one colour: pane headers draw it in the text
/// colour, as Mac apps draw symbols in toolbars.
struct ProgramGlyph: View {
    let program: Program
    let color: Color
    var size: CGFloat = 13

    var body: some View {
        switch program.icon {
        case .brand(let slug):
            if let logo = ProgramImages.logo(slug) {
                Image(nsImage: logo).renderingMode(.template).resizable().interpolation(.high)
                    .frame(width: size, height: size).foregroundStyle(color)
            }
        case .symbol(let name):
            Image(systemName: name).font(.system(size: size * 0.85, weight: .semibold)).foregroundStyle(color)
                .frame(width: size, height: size)
        }
    }
}
