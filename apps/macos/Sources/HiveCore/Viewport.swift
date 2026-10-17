/// Where a pane shows its grid when the grid is taller than the pane: when
/// another window owns the size, this one shows the part with the cursor.
public enum Viewport {
    /// Rows hidden above the pane: none until the cursor would fall below
    /// the last visible row, then enough to keep it on that row.
    public static func rowOffset(rows: Int, visible: Int, cursorRow: Int) -> Int {
        guard visible > 0, rows > visible else { return 0 }
        return max(0, min(rows - visible, cursorRow - visible + 1))
    }
}
