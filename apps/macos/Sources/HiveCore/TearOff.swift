import CoreGraphics
import Foundation

/// Tearing a tab out of the tab bar or the sidebar into its own window.
public enum TearOff {
    /// How far off its row (across it) the pointer goes before a drag becomes a tear.
    public static let threshold: CGFloat = 32

    /// Whether a drag with this translation tears the tab off: far enough across the
    /// row (down from the tab bar; sideways out of the sidebar).
    public static func tears(across: CGFloat, horizontalRow: Bool) -> Bool {
        // From the bar, only downward: above it is the menu bar and the screen's edge.
        horizontalRow ? across > threshold : abs(across) > threshold
    }

    /// The new window's frame for a drop at `point` (screen coordinates, origin bottom
    /// left): its title bar under the pointer, as if the window was carried there, and
    /// the whole window on `screen` (its visible frame).
    public static func windowFrame(dropAt point: CGPoint, size: CGSize, screen: CGRect) -> CGRect {
        let w = min(size.width, screen.width), h = min(size.height, screen.height)
        // The pointer holds the title bar a little in from the left, as it held the tab.
        var x = point.x - min(120, w / 3)
        var y = point.y - h + 19
        x = min(max(x, screen.minX), screen.maxX - w)
        y = min(max(y, screen.minY), screen.maxY - h)
        return CGRect(x: x, y: y, width: w, height: h)
    }
}

/// A tab in its own window, as kept between launches.
public struct TornTab: Codable, Equatable {
    public var session: String
    public var window: String
    /// The window's frame on screen, as NSStringFromRect writes it; nil until placed.
    public var frame: String?

    public init(session: String, window: String, frame: String? = nil) {
        self.session = session
        self.window = window
        self.frame = frame
    }

    /// Reads the saved list; anything unreadable is no torn tabs.
    public static func decode(_ data: Data?) -> [TornTab] {
        guard let data, let list = try? JSONDecoder().decode([TornTab].self, from: data) else { return [] }
        return list
    }

    public static func encode(_ list: [TornTab]) -> Data? { try? JSONEncoder().encode(list) }

    /// The tabs to reopen for a session's view: those saved for it whose window still exists, in order.
    public static func restorable(_ saved: [TornTab], session: String, windows: [String]) -> [TornTab] {
        let live = Set(windows)
        return saved.filter { $0.session == session && live.contains($0.window) }
    }
}

/// Which tab the main window shows when some tabs are in their own windows.
public enum MainTab {
    /// The server's active tab when it is not torn off; else the one the main window showed
    /// last, if still there and not torn; else the first that isn't torn; nil when every tab is.
    public static func pick(serverActive: String?, last: String?, windows: [String], torn: Set<String>) -> String? {
        let here = windows.filter { !torn.contains($0) }
        if let a = serverActive, here.contains(a) { return a }
        if let l = last, here.contains(l) { return l }
        return here.first
    }
}
