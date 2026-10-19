package xyz.matm.hive.agents

import org.json.JSONObject
import java.util.Base64
import kotlin.math.max
import kotlin.math.min
import kotlin.math.pow

/** How a program is drawn without a vendor icon: its logo (Simple Icons), or a symbol. */
sealed class ProgramIcon {
    data class Brand(val slug: String) : ProgramIcon()
    data class Symbol(val name: String) : ProgramIcon()
}

/** A program as hive shows it: a name and an icon, and its vendor's own icon if hive has one. */
data class Program(val name: String, val icon: ProgramIcon, val vendor: String? = null)

/**
 * The Mac app's Programs (HiveCore/Program.swift), from the same table:
 * programs.json is generated from the Mac's sources at build time.
 */
object Programs {
    private class Brand(val path: String, val hex: Int, val fill: Int?)
    private class Vendor(val png: ByteArray, val background: Int?)

    private var table: Map<String, Program> = emptyMap()
    private var brands: Map<String, Brand> = emptyMap()
    private var vendors: Map<String, Vendor> = emptyMap()
    private var agents: Set<String> = setOf("claude", "codex", "gemini", "grok", "opencode")
    private var shells: Set<String> = setOf("zsh", "bash", "sh", "fish", "dash", "ksh", "tcsh", "nu")

    fun load(json: String) {
        val j = JSONObject(json)
        val p = j.getJSONObject("programs")
        table = p.keys().asSequence().associateWith { k ->
            val o = p.getJSONObject(k)
            val (kind, icon) = o.getString("icon").split(":", limit = 2)
            Program(o.getString("name"), if (kind == "brand") ProgramIcon.Brand(icon) else ProgramIcon.Symbol(icon), o.optString("vendor").ifEmpty { null })
        }
        val b = j.getJSONObject("brands")
        brands = b.keys().asSequence().associateWith { k ->
            val o = b.getJSONObject(k)
            Brand(o.getString("path"), o.getInt("hex"), if (o.has("fill")) o.getInt("fill") else null)
        }
        val v = j.getJSONObject("vendors")
        vendors = v.keys().asSequence().associateWith { k ->
            val o = v.getJSONObject(k)
            Vendor(Base64.getDecoder().decode(o.getString("png")), if (o.has("background")) o.getInt("background") else null)
        }
        agents = j.getJSONArray("agents").let { a -> (0 until a.length()).map(a::getString).toSet() }
        shells = j.getJSONArray("shells").let { a -> (0 until a.length()).map(a::getString).toSet() }
    }

    private val terminal = ProgramIcon.Symbol("apple.terminal")

    /** The program for a name the server reports, or a terminal under that name. Names compare without case. */
    fun lookup(name: String): Program {
        if (name.isEmpty()) return Program("shell", terminal)
        return table[canonical(name)] ?: Program(name, terminal)
    }

    /** As the table keys it: lowercase, a login shell's dash dropped, a versioned agent binary by its agent. */
    fun canonical(name: String): String {
        var n = name.lowercase()
        if (n.startsWith("-")) n = n.drop(1)
        val dash = n.indexOf('-')
        if (dash > 0 && n.substring(0, dash) in agents && n.getOrNull(dash + 1)?.isDigit() == true) return n.substring(0, dash)
        return n
    }

    /** Whether an interactive shell is in front: the pane's agent, if it had one, has exited. */
    fun atShell(foreground: String?): Boolean = !foreground.isNullOrEmpty() && canonical(foreground) in shells

    /** What a pane shows: the agent that reported, unless another agent or a shell is in front. */
    fun shown(harness: String?, foreground: String?): Program {
        val fg = foreground?.let(::canonical) ?: ""
        if (harness == null) return lookup(foreground ?: "")
        if (fg in agents && fg != harness) return lookup(fg)
        if (atShell(fg)) return lookup(fg)
        return lookup(harness)
    }

    fun brandPath(slug: String): String? = brands[slug]?.path
    /** A logo's tile colour: its app icon's when that differs (Bun, Ollama), else the brand's. */
    fun fill(slug: String): Int = brands[slug]?.let { it.fill ?: it.hex } ?: 0x3A3B3F
    fun vendorPng(name: String): ByteArray? = vendors[name]?.png
    fun vendorBackground(name: String): Int? = vendors[name]?.background

    /** The glyph colour on a brand fill: white unless white reads below 3:1 (WCAG for graphics), then black. */
    fun onFill(fill: Int): Int = if (ratio(0xFFFFFF, fill) >= 3) 0xFFFFFF else 0x000000

    private fun luminance(c: Int): Double {
        fun lin(v: Double) = if (v <= 0.03928) v / 12.92 else ((v + 0.055) / 1.055).pow(2.4)
        return 0.2126 * lin((c shr 16 and 0xFF) / 255.0) + 0.7152 * lin((c shr 8 and 0xFF) / 255.0) + 0.0722 * lin((c and 0xFF) / 255.0)
    }

    private fun ratio(a: Int, b: Int): Double {
        val la = luminance(a)
        val lb = luminance(b)
        return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
    }

    /** An agent's task from the title it sets ("✳ Arch review" is "Arch review"); none when it's just the agent's name. */
    fun taskName(title: String, harness: String): String? {
        val clean = title.filterNot { it.code in 0x202A..0x202E || it.code in 0x2066..0x2069 || it.code == 0x200E || it.code == 0x200F }
        val t = clean.dropWhile { !it.isLetterOrDigit() }.trim()
        return if (t.isEmpty() || t == lookup(harness).name) null else t
    }
}
