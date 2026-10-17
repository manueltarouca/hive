/// When a program's copy (OSC 52) may set the Mac clipboard: only from the
/// pane the user is working in, while hive is the active app. Claude Code
/// copies from the pane you drag in, which is the focused one, so nothing real
/// is lost, and a background agent can't overwrite what you copied.
public enum ClipboardPolicy {
    /// The server drops larger copies too (vt.MaxClipboardBytes).
    public static let maxBytes = 1 << 20

    public static func accepts(text: String, from block: String, focusedBlock: String?, appActive: Bool) -> Bool {
        appActive && block == focusedBlock && !text.isEmpty && text.utf8.count <= maxBytes
    }
}
