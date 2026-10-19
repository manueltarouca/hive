/// Who sizes a pane: the device you're using. Taking the size from another
/// device is for your own input, focus or window changes; a reload (which
/// another device's resize itself causes) never takes it, or two devices
/// showing one pane would take it from each other forever.
public enum SizeClaim {
    /// owner: the pane's size_owner ("" or nil: nobody; "cli": a one-off resize).
    public static func should(owner: String?, me: String?, interactive: Bool, sameSize: Bool) -> Bool {
        let mine = owner != nil && owner == me
        if mine && sameSize { return false }
        let free = owner == nil || owner!.isEmpty || owner == "cli" || mine
        return free || interactive
    }
}
