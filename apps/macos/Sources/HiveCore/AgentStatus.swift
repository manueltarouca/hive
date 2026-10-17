/// An agent's own report of its state (OSC 7501, the Program Status
/// protocol), as the server's agents.StateFromStatus maps it. Claude Code
/// sends one at every change, also after an interrupt, when no hook runs.
public enum AgentStatus {
    /// The app each agent reports under; others use their own name.
    static let apps = ["claude": "claude-code"]
    static let states = ["idle": "idle", "working": "working", "waiting": "blocked", "done": "done", "failed": "done"]

    public static func state(harness: String, app: String, status: String) -> String? {
        guard app == (apps[harness] ?? harness) else { return nil }
        return states[status]
    }

    /// The state to show: the agent's own status when it reported one, else
    /// the last hook's. An ended agent stays ended.
    public static func effective(hook: String?, harness: String?, records: [(app: String, status: String)]) -> String? {
        guard let harness, hook != "ended" else { return hook }
        return records.compactMap { state(harness: harness, app: $0.app, status: $0.status) }.last ?? hook
    }
}
