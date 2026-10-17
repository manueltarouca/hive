import Testing
@testable import HiveCore

@Suite struct TabSummaryTests {
    @Test func labelledAgentTabShowsItsTask() {
        let s = TabSummary.make(label: "retirement", program: "Claude Code", dir: "~/code/projects/retirement",
                                agents: [AgentSnapshot(state: "idle", task: "Degiro CLI and API")])
        #expect(s == TabSummary(title: "retirement", detail: "Degiro CLI and API", state: nil, isAgent: true))
    }

    @Test func stateIsShownWhenItMatters() {
        let working = TabSummary.make(label: "oracle", program: "Claude Code", dir: "",
                                      agents: [AgentSnapshot(state: "working", task: "Backtest")])
        #expect(working.detail == "Backtest · Working")
        #expect(working.state == "working")

        let blocked = TabSummary.make(label: "", program: "Claude Code", dir: "",
                                      agents: [AgentSnapshot(state: "blocked", task: "Migrate rate limiter", message: "Claude needs your permission to use Bash")])
        // Without a label the task is the title, so the line shows the reason.
        #expect(blocked == TabSummary(title: "Migrate rate limiter", detail: "Claude needs your permission to use Bash", state: "blocked", isAgent: true))
    }

    /// "Done" says so until you've looked at the tab.
    @Test func doneGoesQuietOnceSeen() {
        let a = [AgentSnapshot(state: "done", task: "Write changelog")]
        #expect(TabSummary.make(label: "", program: "Claude Code", dir: "", agents: a).state == "done")
        let seen = TabSummary.make(label: "", program: "Claude Code", dir: "", agents: a, doneSeen: true)
        #expect(seen.state == nil && seen.detail == nil)
    }

    /// With several agents in one tab, the one that needs you most wins.
    @Test func mostUrgentAgentWins() {
        let s = TabSummary.make(label: "x", program: "", dir: "", agents: [
            AgentSnapshot(state: "working", task: "A"), AgentSnapshot(state: "blocked", task: "B", message: ""),
            AgentSnapshot(state: "done", task: "C"),
        ])
        #expect(s.state == "blocked")
        #expect(s.detail == "B · Needs you")
    }

    @Test func plainTabsAreOneLine() {
        #expect(TabSummary.make(label: "bitmagnet", program: "zsh", dir: "~/code", agents: [])
            == TabSummary(title: "bitmagnet", detail: nil, state: nil, isAgent: false))
        #expect(TabSummary.make(label: "", program: "Node", dir: "~/sites/app", agents: [])
            == TabSummary(title: "Node", detail: "~/sites/app", state: nil, isAgent: false))
    }

    @Test func attentionCountsOtherSessionsOnly() {
        let agents = [
            AgentPlace(session: "s1", state: "blocked", doneSeen: false),
            AgentPlace(session: "s2", state: "blocked", doneSeen: false),
            AgentPlace(session: "s2", state: "done", doneSeen: false),
            AgentPlace(session: "s2", state: "done", doneSeen: true),
            AgentPlace(session: "s3", state: "working", doneSeen: false),
        ]
        #expect(Attention.elsewhere(agents, current: "s1") == Attention(blocked: 1, done: 1))
        #expect(Attention.of(agents, session: "s2") == Attention(blocked: 1, done: 1))
        #expect(Attention.of(agents, session: "s3").isEmpty)
    }
}
