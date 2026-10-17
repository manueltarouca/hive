import CGhosttyVT

/// Default colors for mirrors. The default is Ghostty's built-in theme,
/// which is what the maintainer's Ghostty and tmux use.
public struct Theme: Sendable, Equatable {
    public var foreground: RGB
    public var background: RGB
    public var cursor: RGB
    public var selection: RGB
    public var palette: [RGB] // 16

    public init(foreground: RGB, background: RGB, cursor: RGB, selection: RGB, palette: [RGB]) {
        self.foreground = foreground; self.background = background; self.cursor = cursor
        self.selection = selection; self.palette = palette
    }

    /// Ghostty's built-in theme (`ghostty +show-config --default`): Tomorrow
    /// Night colors on a #282c34 background. Ghostty draws the cursor in the
    /// foreground color and inverts selections; the selection here is One
    /// Dark's, which sits well on this background.
    public static let ghostty = Theme(
        foreground: RGB(0xff, 0xff, 0xff), background: RGB(0x28, 0x2c, 0x34), cursor: RGB(0xff, 0xff, 0xff),
        selection: RGB(0x3e, 0x44, 0x51),
        palette: [
            RGB(0x1d, 0x1f, 0x21), RGB(0xcc, 0x66, 0x66), RGB(0xb5, 0xbd, 0x68), RGB(0xf0, 0xc6, 0x74),
            RGB(0x81, 0xa2, 0xbe), RGB(0xb2, 0x94, 0xbb), RGB(0x8a, 0xbe, 0xb7), RGB(0xc5, 0xc8, 0xc6),
            RGB(0x66, 0x66, 0x66), RGB(0xd5, 0x4e, 0x53), RGB(0xb9, 0xca, 0x4a), RGB(0xe7, 0xc5, 0x47),
            RGB(0x7a, 0xa6, 0xda), RGB(0xc3, 0x97, 0xd8), RGB(0x70, 0xc0, 0xb1), RGB(0xea, 0xea, 0xea),
        ])
}

extension RGB {
    var c: GhosttyColorRgb { GhosttyColorRgb(r: r, g: g, b: b) }
}
