/// One agent as a tab row needs it.
public struct AgentSnapshot: Equatable, Sendable {
    public var state: String   // idle, working, blocked, done, ended
    public var task: String
    public var message: String
    public init(state: String, task: String, message: String = "") {
        self.state = state; self.task = task; self.message = message
    }
}

/// What a sidebar row says about a tab. The tab is the agent.
/// The title is the tab's label, else the agent's task, else the program.
/// The second line carries only what matters: the task when the label hides
/// it. A blocked agent shows its reason instead. The state is the row's
/// badge, so the line never repeats it.
public struct TabSummary: Equatable, Sendable {
    public var title: String
    public var detail: String?
    /// working, blocked or done; nil when there's nothing to report.
    public var state: String?
    public var isAgent: Bool

    public init(title: String, detail: String?, state: String?, isAgent: Bool) {
        self.title = title; self.detail = detail; self.state = state; self.isAgent = isAgent
    }

    static let urgency = ["blocked": 3, "done": 2, "working": 1]

    /// `doneSeen`: the user has looked at the tab since its agent finished.
    public static func make(label: String, program: String, dir: String, agents: [AgentSnapshot], doneSeen: Bool = false) -> TabSummary {
        guard let agent = agents.max(by: { (urgency[$0.state] ?? 0) < (urgency[$1.state] ?? 0) }) else {
            if !label.isEmpty { return TabSummary(title: label, detail: nil, state: nil, isAgent: false) }
            return TabSummary(title: program, detail: dir.isEmpty ? nil : dir, state: nil, isAgent: false)
        }
        var state: String? = urgency[agent.state] != nil ? agent.state : nil
        if state == "done", doneSeen { state = nil }
        let title = label.isEmpty ? agent.task : label
        let detail: String?
        if state == "blocked" {
            // Why it waits; else which task, when the label hides it.
            detail = !agent.message.isEmpty ? agent.message : title != agent.task ? "\(agent.task) needs you" : "Needs you"
        } else {
            detail = title != agent.task ? agent.task : nil
        }
        return TabSummary(title: title, detail: detail, state: state, isAgent: true)
    }
}

/// Where an agent is, for counting what needs the user.
public struct AgentPlace: Equatable, Sendable {
    public var session: String
    public var state: String
    public var doneSeen: Bool
    public init(session: String, state: String, doneSeen: Bool) {
        self.session = session; self.state = state; self.doneSeen = doneSeen
    }
}

/// How many agents need the user: blocked, or done and not yet looked at.
public struct Attention: Equatable, Sendable {
    public var blocked = 0, done = 0
    public init(blocked: Int = 0, done: Int = 0) { self.blocked = blocked; self.done = done }
    public var isEmpty: Bool { blocked == 0 && done == 0 }

    static func count(_ agents: [AgentPlace]) -> Attention {
        var a = Attention()
        for x in agents {
            if x.state == "blocked" { a.blocked += 1 }
            if x.state == "done", !x.doneSeen { a.done += 1 }
        }
        return a
    }

    /// In one session.
    public static func of(_ agents: [AgentPlace], session: String) -> Attention {
        count(agents.filter { $0.session == session })
    }

    /// In every session but the current one.
    public static func elsewhere(_ agents: [AgentPlace], current: String?) -> Attention {
        count(agents.filter { $0.session != current })
    }
}
