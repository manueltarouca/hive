package xyz.matm.hive.agents

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.BeforeClass
import org.junit.Test
import java.io.File

/** The Mac's ProgramTests, on the phone: the same table, generated from the Mac's sources. */
class ProgramsTest {
    companion object {
        @BeforeClass @JvmStatic fun load() = Programs.load(File(requireNotNull(System.getProperty("hive.programs")) {
            "hive.programs: run through Gradle, which generates programs.json from the Mac's sources" }).readText())
    }

    @Test fun programsResolveByAlias() {
        assertEquals(ProgramIcon.Brand("neovim"), Programs.lookup("nvim").icon)
        assertEquals("Neovim", Programs.lookup("nvim").name)
        assertEquals(ProgramIcon.Brand("git"), Programs.lookup("lazygit").icon)
        assertEquals("Git Changes", Programs.lookup("lazygit").name)
        assertEquals(ProgramIcon.Brand("python"), Programs.lookup("Python").icon)
    }

    @Test fun agentIcons() {
        assertEquals(ProgramIcon.Brand("claude"), Programs.lookup("claude").icon)
        assertEquals("Claude Code", Programs.lookup("claude").name)
        assertEquals(ProgramIcon.Symbol("sparkles"), Programs.lookup("gemini").icon)
        assertEquals("claude", Programs.lookup("claude").vendor)
        assertNull(Programs.lookup("zsh").vendor)
    }

    @Test fun iconsArePresent() {
        assertTrue(Programs.vendorPng("claude")!!.size > 100)
        assertEquals(0xFFFFFF, Programs.vendorBackground("python"))
        assertNull(Programs.vendorBackground("claude"))
        assertTrue(Programs.brandPath("git")!!.startsWith("M"))
        assertEquals(0xFBF0DF, Programs.fill("bun"))
    }

    @Test fun unknownProgramsAreTerminals() {
        assertEquals(ProgramIcon.Symbol("apple.terminal"), Programs.lookup("frobnicate").icon)
        assertEquals("frobnicate", Programs.lookup("frobnicate").name)
        assertEquals("shell", Programs.lookup("").name)
    }

    @Test fun versionedAgentBinaries() {
        assertEquals("grok", Programs.canonical("grok-1.0.62-macos-aarch64"))
        assertEquals("zsh", Programs.canonical("-zsh"))
        assertEquals("node-gyp", Programs.canonical("node-gyp"))
    }

    @Test fun whatAPaneShows() {
        assertEquals("Claude Code", Programs.shown("claude", "node").name) // the agent that reported
        assertEquals("Grok", Programs.shown("claude", "grok").name)       // another agent runs: it wins
        assertEquals("Zsh", Programs.shown("claude", "zsh").name)         // back at the shell
        assertEquals("Python", Programs.shown(null, "python").name)
        assertEquals("python3.12", Programs.shown(null, "python3.12").name) // not in the table: a terminal under its name
        assertTrue(Programs.atShell("-zsh")); assertFalse(Programs.atShell("claude")); assertFalse(Programs.atShell(null))
    }

    @Test fun glyphOnBrandFill() {
        assertEquals(0xFFFFFF, Programs.onFill(0x000000))
        assertEquals(0x000000, Programs.onFill(0xFBF0DF))
    }

    @Test fun tasksFromTitles() {
        assertEquals("Arch review", Programs.taskName("✳ Arch review", "claude"))
        assertNull(Programs.taskName("✳ Claude Code", "claude"))
        assertNull(Programs.taskName("", "claude"))
        assertEquals("Fix login", Programs.taskName("‮Fix login", "claude"))
    }
}
