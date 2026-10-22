/// An agent's own report of its state (OSC 7501, the Program Status
/// protocol), as the server's agents.StateFromStatus maps it. Claude Code
/// sends one at every change, also after an interrupt, when no hook runs.
public enum AgentStatus {
    /// The app each agent reports under; others use their own name.
    static let apps = ["claude": "claude-code"]
    /// The protocol's states; error is "failed and stopped" (Claude Code at
    /// its usage limit): the turn is over and the message says why. The app
    /// shows it as stopped, apart from done: it needs you.
    static let states = ["idle": "idle", "working": "working", "blocked": "blocked", "done": "done", "error": "stopped"]

    public static func state(harness: String, app: String, status: String) -> String? {
        guard app == (apps[harness] ?? harness) else { return nil }
        return states[status]
    }

    /// The state to show: the agent's own status when it reported one, else
    /// the last hook's. An ended agent stays ended.
    /// The server marks such an agent done with kind error.
    public static func effective(hook: String?, kind: String? = nil, harness: String?, records: [(app: String, status: String)]) -> String? {
        guard let harness, hook != "ended" else { return hook }
        let own = records.compactMap { state(harness: harness, app: $0.app, status: $0.status) }.last
        if hook == "done", kind == "error", own == nil || own == "done" { return "stopped" }
        return own ?? hook
    }
}
