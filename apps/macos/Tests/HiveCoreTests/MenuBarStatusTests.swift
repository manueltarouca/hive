import Testing
@testable import HiveCore

/// The menu bar's quick status: what the icon shows, the line under
/// "hive", and the agents, those that need you first.
@Suite struct MenuBarStatusTests {
    typealias H = MenuBarStatus.Host
    typealias A = MenuBarStatus.Agent

    func agent(_ id: String, _ state: String, seen: Bool = false, host: String = "local") -> A {
        A(id: id, state: state, doneSeen: seen, host: host)
    }

    @Test func quietWhenNothingNeedsYou() {
        let s = MenuBarStatus(hosts: [H(id: "local", name: "This Mac", connected: true, local: true)],
                              agents: [agent("a", "idle"), agent("b", "done", seen: true)])
        #expect(s.icon == .quiet)
        #expect(s.subtitle == "Connected")
        #expect(s.needsYou.isEmpty && s.working.isEmpty)
        #expect(s.quiet == 1)
    }

    @Test func blockedFirstThenUnseenDone() {
        let s = MenuBarStatus(hosts: [H(id: "local", name: "This Mac", connected: true, local: true)],
                              agents: [agent("d", "done"), agent("w", "working"), agent("b", "blocked"), agent("s", "done", seen: true)])
        #expect(s.needsYou.map(\.id) == ["b", "d"])
        #expect(s.working.map(\.id) == ["w"])
        #expect(s.quiet == 0)
        #expect(s.icon == .needsYou(2))
    }

    @Test func workingShowsWhenNothingNeedsYou() {
        let s = MenuBarStatus(hosts: [H(id: "local", name: "This Mac", connected: true, local: true)],
                              agents: [agent("w", "working")])
        #expect(s.icon == .working)
    }

    @Test func subtitleNamesHostsAway() {
        let local = H(id: "local", name: "This Mac", connected: true, local: true)
        #expect(MenuBarStatus(hosts: [local, H(id: "lab", name: "lab", connected: true)], agents: []).subtitle == "Connected to 2 hosts")
        let away = MenuBarStatus(hosts: [local, H(id: "lab", name: "lab", connected: false), H(id: "pi", name: "pi", connected: true)], agents: [])
        #expect(away.subtitle == "1 of 3 hosts away")
        #expect(away.icon == .quiet)
        // Saved but not connected on purpose: not away.
        #expect(MenuBarStatus(hosts: [local, H(id: "lab", name: "lab", connected: false, idle: true)], agents: []).subtitle == "Connected")
    }

    @Test func thisMacAwayDimsTheIcon() {
        let s = MenuBarStatus(hosts: [H(id: "local", name: "This Mac", connected: false, local: true)], agents: [agent("b", "blocked")])
        #expect(s.subtitle == "Reconnecting…")
        // What it last knew is out of date: not shown.
        #expect(s.icon == .away)
        #expect(s.needsYou.isEmpty)
    }

    @Test func agentsOfAHostAwayAreNotCounted() {
        let s = MenuBarStatus(hosts: [H(id: "local", name: "This Mac", connected: true, local: true), H(id: "lab", name: "lab", connected: false)],
                              agents: [agent("b", "blocked", host: "lab")])
        #expect(s.needsYou.isEmpty)
        #expect(s.icon == .quiet)
    }
}

@Suite struct MenuBarStatusRulesTests {
    @Test func endedAgentsCountNowhere() {
        let s = MenuBarStatus(hosts: [.init(id: "local", name: "This Mac", connected: true, local: true)],
                              agents: [.init(id: "e", state: "ended", doneSeen: false, host: "local"),
                                       .init(id: "i", state: "idle", doneSeen: false, host: "local")])
        #expect(s.quiet == 1)
    }

    @Test func thisMacsProblemIsTheTitleBarsWords() {
        let s = MenuBarStatus(hosts: [.init(id: "local", name: "This Mac", connected: false, local: true, problem: "No hive server running")], agents: [])
        #expect(s.subtitle == "No hive server running")
    }

    @Test func longTextIsCutForTheMenu() {
        #expect(MenuBarStatus.clip("short", max: 10) == "short")
        #expect(MenuBarStatus.clip("Refactor the attach protocol now", max: 20) == "Refactor the attach…")
        // Whitespace and newlines from an agent's message become one line.
        #expect(MenuBarStatus.clip("May I\n  run   Bash?", max: 40) == "May I run Bash?")
    }
}
