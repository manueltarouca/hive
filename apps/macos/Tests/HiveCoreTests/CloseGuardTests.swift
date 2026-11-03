import Testing
@testable import HiveCore

@Suite struct CloseGuardTests {
    let shell = CloseGuard.Pane(foreground: "zsh", exited: false, name: "zsh")
    let loginShell = CloseGuard.Pane(foreground: "-zsh", exited: false, name: "zsh")
    let claude = CloseGuard.Pane(foreground: "claude", exited: false, name: "Claude Code")
    let vim = CloseGuard.Pane(foreground: "nvim", exited: false, name: "Neovim")
    let ended = CloseGuard.Pane(foreground: "make", exited: true, name: "make")

    @Test func shellsAtTheirPromptCloseAtOnce() {
        #expect(CloseGuard.question(closing: [shell, loginShell, ended], what: "zsh", policy: .running) == nil)
    }

    @Test func aRunningProgramAsks() {
        let q = CloseGuard.question(closing: [shell, claude], what: "claude", policy: .running)
        #expect(q?.title == "Close “claude”?")
        #expect(q?.detail == "Claude Code is still running in it. Closing ends it.")
    }

    @Test func severalProgramsAreNamedOnce() {
        let q = CloseGuard.question(closing: [claude, vim, claude], what: "work", policy: .running)
        #expect(q?.detail == "Claude Code and Neovim are still running in it. Closing ends them.")
    }

    @Test func policiesOverride() {
        #expect(CloseGuard.question(closing: [claude], what: "x", policy: .never) == nil)
        #expect(CloseGuard.question(closing: [shell], what: "x", policy: .always)?.detail == "Nothing else is running in it.")
        #expect(CloseGuard.question(closing: [shell, shell], what: "x", policy: .always)?.detail == "It has 2 panes.")
    }

    @Test func motionSprings() {
        #expect(Motion.snappy.spring(reduceMotion: false)?.damping == 1.0)
        #expect(Motion.natural.spring(reduceMotion: false)!.damping < 1)
        #expect(Motion.off.spring(reduceMotion: false) == nil)
        #expect(Motion.natural.spring(reduceMotion: true) == nil)
    }
}
