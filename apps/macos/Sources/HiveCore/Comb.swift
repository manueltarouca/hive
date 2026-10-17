import CoreGraphics

/// Where the cells of a comb go: a tab with several panes is drawn as
/// connected honeycomb cells. Hexagons standing on a point tessellate with
/// neighbours half a cell across and three quarters of a cell down.
public enum Comb {
    public struct Layout: Equatable, Sendable {
        /// Each cell's height; its width is 0.866 of that.
        public var cell: CGFloat
        /// The top-left of each cell's frame.
        public var origins: [CGPoint]
        public var width: CGFloat
    }

    /// One cell fills the height; two join on a slanted edge, the first
    /// above-left; three make a triangle, the first on top.
    public static func layout(count: Int, height: CGFloat) -> Layout {
        switch count {
        case ...1:
            return Layout(cell: height, origins: [.zero], width: height * 0.866)
        case 2:
            let h = height / 1.75, w = h * 0.866
            return Layout(cell: h, origins: [.zero, CGPoint(x: w / 2, y: h * 0.75)], width: w * 1.5)
        default:
            let h = height / 1.75, w = h * 0.866
            return Layout(cell: h, origins: [CGPoint(x: w / 2, y: 0), CGPoint(x: 0, y: h * 0.75), CGPoint(x: w, y: h * 0.75)],
                          width: w * 2)
        }
    }
}
