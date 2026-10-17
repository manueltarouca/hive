import AppKit
import HiveCore
import SwiftUI

/// hive's look: the reference app's affordances, our own identity. Warm neutrals, a honey
/// accent for focus, and hexagons for agent status. Dark first.
struct Style: Equatable {
    var dark: Bool
    var windowTop: NSColor, windowBottom: NSColor
    var card: NSColor, cardUnfocused: NSColor
    var text: NSColor, secondary: NSColor
    var border: NSColor
    var tabActive: NSColor
    /// Honey: focus, active tab, working agents.
    var accent: NSColor
    var blocked: NSColor, done: NSColor
    var sidebar: NSColor
    var terminal: Theme

    /// "Hive Night": warm charcoal, Tomorrow Night terminal colors.
    static let dark = Style(
        dark: true,
        windowTop: hex(0x141317), windowBottom: hex(0x0F0E12),
        card: hex(0x1B1A1F), cardUnfocused: hex(0x17161A),
        text: hex(0xECE8E1), secondary: hex(0x8D877E), border: hex(0x2B2930), tabActive: hex(0x26242B),
        accent: hex(0xF2A93B), blocked: hex(0xE5645B), done: hex(0x7DBE8F), sidebar: hex(0x111014),
        terminal: Theme(
            foreground: RGB(0xE3, 0xDF, 0xD8), background: RGB(0x1B, 0x1A, 0x1F), cursor: RGB(0xF2, 0xA9, 0x3B),
            selection: RGB(0x4D, 0x41, 0x2A),
            palette: Theme.tomorrowNight.palette))

    /// "Hive Day": warm paper with a hint of honey.
    static let light = Style(
        dark: false,
        windowTop: hex(0xF4EFE6), windowBottom: hex(0xEDE6D9),
        card: hex(0xFDFBF7), cardUnfocused: hex(0xF6F2EA),
        text: hex(0x231F1A), secondary: hex(0x8C8478), border: hex(0xE4DCCD), tabActive: hex(0xFFFFFF),
        accent: hex(0xD98A12), blocked: hex(0xD24E45), done: hex(0x4E9A64), sidebar: hex(0xEFE8DC),
        terminal: Theme(
            foreground: RGB(0x23, 0x1F, 0x1A), background: RGB(0xFD, 0xFB, 0xF7), cursor: RGB(0xD9, 0x8A, 0x12),
            selection: RGB(0xF6, 0xE3, 0xBF),
            palette: [
                RGB(0x23, 0x1F, 0x1A), RGB(0xC2, 0x45, 0x3D), RGB(0x3F, 0x8A, 0x55), RGB(0xA8, 0x74, 0x0B),
                RGB(0x35, 0x6C, 0xB0), RGB(0x8A, 0x55, 0xA8), RGB(0x2A, 0x86, 0x8C), RGB(0x8C, 0x84, 0x78),
                RGB(0x5E, 0x57, 0x4D), RGB(0xD4, 0x58, 0x4F), RGB(0x4C, 0x9C, 0x64), RGB(0xC0, 0x88, 0x14),
                RGB(0x46, 0x7E, 0xC4), RGB(0x9C, 0x67, 0xBA), RGB(0x37, 0x99, 0x9F), RGB(0xB5, 0xAD, 0xA0),
            ]))

    /// How strongly unfocused panes are tinted toward cardUnfocused.
    var unfocusedTint: CGFloat { dark ? 0.35 : 0.30 }

    /// The color an unfocused pane ends up: the terminal background tinted
    /// exactly as the renderer does it, so card padding and terminal match.
    var unfocusedPane: NSColor {
        let bg = terminal.background, a = unfocusedTint
        let c = cardUnfocused.usingColorSpace(.sRGB) ?? cardUnfocused
        return NSColor(srgbRed: CGFloat(bg.r) / 255 * (1 - a) + c.redComponent * a,
                       green: CGFloat(bg.g) / 255 * (1 - a) + c.greenComponent * a,
                       blue: CGFloat(bg.b) / 255 * (1 - a) + c.blueComponent * a, alpha: 1)
    }

    /// The focused pane's color: exactly the terminal background.
    var focusedPane: NSColor {
        let bg = terminal.background
        return NSColor(srgbRed: CGFloat(bg.r) / 255, green: CGFloat(bg.g) / 255, blue: CGFloat(bg.b) / 255, alpha: 1)
    }

    static func hex(_ v: UInt32) -> NSColor {
        NSColor(srgbRed: CGFloat((v >> 16) & 0xff) / 255, green: CGFloat((v >> 8) & 0xff) / 255, blue: CGFloat(v & 0xff) / 255, alpha: 1)
    }
}

extension NSColor {
    var swiftUI: Color { Color(nsColor: self) }
}
