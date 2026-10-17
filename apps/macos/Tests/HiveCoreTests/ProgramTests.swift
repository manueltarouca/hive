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

    /// Programs with a Mac app show that app's icon when it's installed.
    @Test func macAppsByBundleID() {
        #expect(Programs.lookup("claude").apps.isEmpty)   // its coral tile, as in the demos
        #expect(Programs.lookup("gemini").apps == ["com.google.GeminiMacOS"])
        #expect(Programs.lookup("codex").apps == ["com.openai.codex"])
        #expect(Programs.lookup("docker").apps.contains("io.podmandesktop.PodmanDesktop"))
        #expect(Programs.lookup("zsh").apps.isEmpty)
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
        for icon in BrandIcon.all.values {
            #expect(Contrast.ratio(Contrast.onFill(icon.hex), icon.hex) >= 3, "\(icon.title)")
        }
    }
}
