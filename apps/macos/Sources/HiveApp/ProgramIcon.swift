import AppKit
import HiveCore
import SwiftUI

extension NSColor {
    convenience init(hex: UInt32) {
        self.init(srgbRed: CGFloat((hex >> 16) & 0xFF) / 255, green: CGFloat((hex >> 8) & 0xFF) / 255,
                  blue: CGFloat(hex & 0xFF) / 255, alpha: 1)
    }
}

/// Icons hive draws or borrows, decoded once.
@MainActor
enum ProgramImages {
    private static var logos: [String: NSImage] = [:]
    private static var apps: [String: NSImage?] = [:]

    /// A Simple Icons logo as a template, so it takes any colour.
    static func logo(_ slug: String) -> NSImage? {
        if let i = logos[slug] { return i }
        guard let icon = BrandIcon.all[slug], let i = NSImage(data: Data(icon.svg.utf8)) else { return nil }
        i.isTemplate = true
        logos[slug] = i
        return i
    }

    /// The icon of the first of these Mac apps that's installed. Read from
    /// the Mac at run time, never shipped with hive.
    static func app(_ ids: [String]) -> NSImage? {
        for id in ids {
            if let cached = apps[id] { if let i = cached { return i } else { continue } }
            let icon = NSWorkspace.shared.urlForApplication(withBundleIdentifier: id).map { NSWorkspace.shared.icon(forFile: $0.path) }
            apps[id] = icon
            if let icon { return icon }
        }
        return nil
    }
}

/// A program as a small app icon, in the tab bar and sidebar: the installed
/// Mac app's icon if it has one; else its logo on a squircle in its brand
/// colour, drawn like a Mac app icon; else a symbol on a graphite one.
struct ProgramTile: View {
    let program: Program
    var size: CGFloat = 22

    var body: some View {
        Group {
            if let app = ProgramImages.app(program.apps) {
                // Mac icons leave a margin around their squircle: crop to it.
                Image(nsImage: app).resizable().interpolation(.high).scaledToFill()
                    .frame(width: size * 1.22, height: size * 1.22)
                    .frame(width: size, height: size)
            } else {
                let (fill, glyph) = colors
                let shape = RoundedRectangle(cornerRadius: size * 0.235, style: .continuous)
                ZStack {
                    shape.fill(LinearGradient(colors: [Color(nsColor: fill.blended(withFraction: 0.18, of: .white) ?? fill),
                                                       Color(nsColor: fill.blended(withFraction: 0.12, of: .black) ?? fill)],
                                              startPoint: .top, endPoint: .bottom))
                    shape.strokeBorder(.white.opacity(0.14), lineWidth: 0.5)
                    ProgramGlyph(program: program, color: Color(nsColor: glyph), size: size * 0.6)
                }
                .frame(width: size, height: size)
                .shadow(color: .black.opacity(0.18), radius: 0.5, y: 0.5)
            }
        }
        .accessibilityLabel(program.name)
    }

    private var colors: (fill: NSColor, glyph: NSColor) {
        if case .brand(let slug) = program.icon, let hex = BrandIcon.all[slug]?.hex {
            return (NSColor(hex: hex), NSColor(hex: Contrast.onFill(hex)))
        }
        return (NSColor(hex: 0x3A3B3F), .white)
    }
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
