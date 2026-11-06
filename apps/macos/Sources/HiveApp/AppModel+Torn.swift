import AppKit
import HiveCore

/// Tabs torn off into their own windows. They belong to the session on screen: another
/// session or host brings them back. The main window keeps its own tab (MainTab), so a
/// torn window taking focus never moves it.
extension AppModel {
    func isTorn(_ id: String) -> Bool { torn.contains(id) }

    /// Whether a tab can go to its own window: one of the session's, and not the last one left in the main window.
    func canTearOff(_ id: String) -> Bool {
        currentSession != nil && windows.contains { $0.id == id } && !isTorn(id) && windows.filter { !isTorn($0.id) }.count > 1
    }

    /// Moves a tab to its own window. frame: where to open it (screen coordinates), else beside the main window.
    func tearOff(_ id: String, frame: NSRect? = nil) {
        guard canTearOff(id) else { return }
        let wasMain = mainWindow == id
        // Its neighbour in the order shown: the next one, else the one before (as when a tab closes).
        let shown = shownWindows.map(\.window.id)
        let at = shown.firstIndex(of: id) ?? 0
        let next = (shown[(at + 1)...].first { !isTorn($0) }) ?? shown[..<at].last { !isTorn($0) }
        torn.append(id)
        tearingNow = id
        if let frame { tornFrames[id] = NSStringFromRect(frame) }
        // The main window moves on to it, and other devices with it.
        if wasMain, let next, let session = currentSession {
            activeWindow = next
            mainWindow = next
            call("session.focus_window", ["session_id": .string(session), "window_id": .string(next)])
        }
        updateMainTab()
        tornChanged()
    }

    /// Back into the main window. select: also show it there and bring the main window forward.
    func bringBack(_ id: String, select: Bool = true) {
        guard isTorn(id) else { return }
        torn.removeAll { $0 == id }
        tornFrames[id] = nil
        if scope == id { scope = nil }
        if select {
            // At once: the server's view follows.
            activeWindow = id
            mainWindow = id
            if let session = currentSession {
                call("session.focus_window", ["session_id": .string(session), "window_id": .string(id)])
            }
            showMainWindow?()
        }
        updateMainTab()
        tornChanged()
    }

    /// Every torn tab back, without moving the main window (another session or host).
    func bringBackAll() {
        guard !torn.isEmpty else { return }
        torn = []
        tornFrames = [:]
        scope = nil
        tornToRestore = []
        tornChanged()
    }

    /// A torn window moved or changed size.
    func tornMoved(_ id: String, frame: NSRect) {
        guard isTorn(id) else { return }
        tornFrames[id] = NSStringFromRect(frame)
        saveTorn()
    }

    /// The session's view arrived: torn tabs that closed go, saved ones come back once, and the
    /// main window picks its tab.
    func tornReloaded(session sid: String) {
        let ids = windows.map(\.id)
        var changed = false
        if !tornToRestore.isEmpty {
            for t in TornTab.restorable(tornToRestore, session: sid, windows: ids) where !isTorn(t.window) {
                torn.append(t.window)
                tornFrames[t.window] = t.frame
                changed = true
            }
            tornToRestore.removeAll { $0.session == sid }
        }
        let live = torn.filter { ids.contains($0) }
        if live != torn {
            for gone in torn where !ids.contains(gone) { tornFrames[gone] = nil }
            torn = live
            if let s = scope, !live.contains(s) { scope = nil }
            changed = true
        }
        // Every tab torn off (another device closed the rest): the first comes back.
        if !torn.isEmpty, torn.count == windows.count, let first = torn.first {
            torn.removeFirst()
            tornFrames[first] = nil
            changed = true
        }
        updateMainTab()
        if changed { tornChanged() }
    }

    func updateMainTab() {
        let next = MainTab.pick(serverActive: activeWindow, last: mainWindow, windows: windows.map(\.id), torn: Set(torn))
        if next != mainWindow { mainWindow = next }
    }

    private func tornChanged() {
        saveTorn()
        // Windows first: a torn tab's panes keep streaming into its new window, no gap.
        onTornChanged?()
        attachVisible()
        onLayout?()
    }

    private func saveTorn() {
        guard let sid = currentSession else { return }
        let list = torn.map { TornTab(session: sid, window: $0, frame: tornFrames[$0]) }
        AppModel.defaults.set(TornTab.encode(list), forKey: "tornTabs")
    }
}
