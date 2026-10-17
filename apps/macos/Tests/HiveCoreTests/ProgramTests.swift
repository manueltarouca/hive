import AppKit
import Testing
@testable import HiveCore

@Suite struct ProgramTests {
    /// What the server reports maps to one program, with its name and icon.
    @Test func programsResolveByAlias() {
        #expect(Programs.lookup("nvim").icon == .brand("neovim"))
        #expect(Programs.lookup("nvim").name == "Neovim")
        #expect(Programs.lookup("node").icon == .brand("nodedotjs"))
        #expect(Programs.lookup("lazygit").icon == .brand("git"))
        #expect(Programs.lookup("lazygit").name == "Git Changes")
        #expect(Programs.lookup("psql").icon == .brand("postgresql"))
        #expect(Programs.lookup("Python").icon == .brand("python"))
    }

    /// Claude Code is its logo; agents without one in the set are symbols.
    @Test func agentIcons() {
        #expect(Programs.lookup("claude").icon == .brand("claude"))
        #expect(Programs.lookup("claude").name == "Claude Code")
        #expect(Programs.lookup("gemini").icon == .symbol("sparkles"))
        #expect(Programs.lookup("codex").name == "Codex")
    }

    /// Programs with a vendor icon show it: Claude Code its docs icon,
    /// not Claude Desktop's.
    @Test func vendorIcons() {
        #expect(Programs.lookup("claude").vendor == "claude")
        #expect(Programs.lookup("codex").vendor == "codex")
        #expect(Programs.lookup("python").vendor == "python")
        #expect(Programs.lookup("zsh").vendor == nil)
        let used = Set(Programs.table.values.compactMap(\.vendor))
        #expect(used == Set(VendorIcon.all.keys))
    }

    /// The vendor icons decode, at most 96 px, and transparent marks have a
    /// background to sit on.
    @Test func vendorIconsDecode() throws {
        for (name, icon) in VendorIcon.all {
            let data = try #require(Data(base64Encoded: icon.png), "\(name)")
            let rep = try #require(NSBitmapImageRep(data: data), "\(name)")
            #expect(rep.pixelsWide <= 96 && rep.pixelsWide >= 48, "\(name)")
        }
        #expect(VendorIcon.all["python"]?.background == 0xFFFFFF)
        #expect(VendorIcon.all["claude"]?.background == nil)
    }

    /// Anything else is a terminal, under its own name.
    @Test func unknownProgramsAreTerminals() {
        let p = Programs.lookup("frobnicate")
        #expect(p.icon == .symbol("apple.terminal"))
        #expect(p.name == "frobnicate")
        #expect(Programs.lookup("").name == "shell")
    }

    /// Every logo the table names is bundled, and every bundled logo is used.
    @Test func brandIconsAndTableMatch() {
        let named = Set(Programs.table.values.compactMap { if case .brand(let s) = $0.icon { s } else { nil } })
        #expect(named == Set(BrandIcon.all.keys))
    }

    /// The bundled SVGs decode as 24-point images.
    @Test func brandIconsDecode() throws {
        for (slug, icon) in BrandIcon.all {
            let image = try #require(NSImage(data: Data(icon.svg.utf8)), "\(slug)")
            #expect(image.size == NSSize(width: 24, height: 24), "\(slug)")
        }
    }
}

@Suite struct ContrastTests {
    @Test func ratioMatchesWCAG() {
        #expect(abs(Contrast.ratio(0x000000, 0xFFFFFF) - 21) < 0.01)
        #expect(abs(Contrast.ratio(0x777777, 0x777777) - 1) < 0.01)
    }

    /// Logos sit on a tile filled with their brand colour, like an app icon:
    /// white on it, as Claude's coral tile has, unless the colour is too
    /// light for white to read.
    @Test func glyphOnBrandFill() {
        #expect(Contrast.onFill(0xD97757) == 0xFFFFFF)   // Claude
        #expect(Contrast.onFill(0x000000) == 0xFFFFFF)   // Bun
        #expect(Contrast.onFill(0xFFDA18) == 0x000000)   // OpenTofu
        for (slug, icon) in BrandIcon.all {
            let fill = BrandIcon.fill(slug)
            #expect(Contrast.ratio(Contrast.onFill(fill), fill) >= 3, "\(icon.title)")
        }
    }

    /// Some tiles use the app icon's colour rather than the logo's: Bun's
    /// icon is cream, not black.
    @Test func tileFillsFollowAppIcons() {
        #expect(BrandIcon.fill("bun") == 0xFBF0DF)
        #expect(Contrast.onFill(BrandIcon.fill("bun")) == 0x000000)
        #expect(BrandIcon.fill("python") == 0x3776AB)
    }
}

/// What a pane shows: the agent that reported from it, unless the program
/// running there says otherwise.
@Suite struct ShownProgramTests {
    /// Release binaries carry a version and platform (older servers send them).
    @Test func versionedAgentBinaries() {
        #expect(Programs.lookup("grok-1.0.41-macos-aarch64").name == "Grok")
        #expect(Programs.canonical("grok-1.0.62-macos-aarch64") == "grok")
        #expect(Programs.canonical("Python") == "python")
        #expect(Programs.canonical("-zsh") == "zsh")
    }

    @Test func theReportedAgent() {
        #expect(Programs.shown(harness: "claude", foreground: "claude").name == "Claude Code")
        // Older servers name Claude by its binary, a version number.
        #expect(Programs.shown(harness: "claude", foreground: "2.1.302").name == "Claude Code")
        #expect(Programs.shown(harness: "gemini", foreground: "node").name == "Gemini")
        #expect(Programs.shown(harness: nil, foreground: "nvim").name == "Neovim")
    }

    /// Grok runs Claude Code's hooks, so a "claude" agent can be grok.
    @Test func anotherAgentRunningWins() {
        #expect(Programs.shown(harness: "claude", foreground: "grok-1.0.41-macos-aarch64").name == "Grok")
        #expect(Programs.shown(harness: "claude", foreground: "codex").name == "Codex")
    }

    /// The shell in front means the pane's agent is gone: interactive
    /// shells only (servers name a shell running a script by the script).
    @Test func atTheShell() {
        #expect(Programs.atShell("zsh"))
        #expect(Programs.atShell("-zsh"))
        #expect(!Programs.atShell("2.1.302"))
        #expect(!Programs.atShell("claude"))
        #expect(!Programs.atShell(nil))
        #expect(!Programs.atShell(""))
    }

    /// An agent that ended without saying so (killed) leaves the shell.
    @Test func backAtTheShell() {
        #expect(Programs.shown(harness: "claude", foreground: "zsh").name == "Zsh")
        #expect(Programs.shown(harness: "claude", foreground: "-bash").name == "Bash")
        #expect(Programs.shown(harness: "claude", foreground: nil).name == "Claude Code")
    }
}

/// Claude Code reports its own state (OSC 7501) at every change, also after
/// an interrupt, when no hook runs; it decides the state shown.
@Suite struct AgentStatusTests {
    @Test func statusMapsOntoAgentStates() {
        #expect(AgentStatus.state(harness: "claude", app: "claude-code", status: "idle") == "idle")
        #expect(AgentStatus.state(harness: "claude", app: "claude-code", status: "blocked") == "blocked")
        // error: failed and stopped, such as Claude Code at the usage limit.
        #expect(AgentStatus.state(harness: "claude", app: "claude-code", status: "error") == "done")
        #expect(AgentStatus.state(harness: "grok", app: "grok", status: "working") == "working")
        #expect(AgentStatus.state(harness: "claude", app: "cargo", status: "working") == nil)
        #expect(AgentStatus.state(harness: "claude", app: "claude-code", status: "clear") == nil)
    }

    /// The interrupted pane: hooks said working, Claude says idle.
    @Test func ownStatusWinsOverTheLastHook() {
        let records = [(app: "cargo", status: "working"), (app: "claude-code", status: "idle")]
        #expect(AgentStatus.effective(hook: "working", harness: "claude", records: records) == "idle")
        #expect(AgentStatus.effective(hook: "blocked", harness: "claude", records: []) == "blocked")
        #expect(AgentStatus.effective(hook: "ended", harness: "claude", records: records) == "ended")
    }
}
