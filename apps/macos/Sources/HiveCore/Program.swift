import Foundation

/// A logo from Simple Icons: one path on a 24×24 grid, and its brand colour.
/// The logos themselves are generated into Icons.generated.swift.
public struct BrandIcon: Sendable {
    public let title: String
    public let hex: UInt32
    public let path: String

    /// Tiles whose app icon is another colour than the logo: Bun's icon is
    /// cream with a black face, Ollama's white with a black llama.
    static let fills: [String: UInt32] = ["bun": 0xFBF0DF, "ollama": 0xFFFFFF]

    /// The colour of a logo's tile.
    public static func fill(_ slug: String) -> UInt32 { fills[slug] ?? all[slug]?.hex ?? 0x3A3B3F }

    /// The logo as an SVG document, as Simple Icons ships it.
    public var svg: String {
        #"<svg role="img" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="\#(path)"/></svg>"#
    }
}

/// A program's own icon from its vendor's site, as a PNG (base64), and the
/// colour behind it when it's a transparent mark. Generated into
/// VendorIcons.generated.swift by script/vendor-icons.sh.
public struct VendorIcon: Sendable {
    public let title: String
    public let background: UInt32?
    public let png: String
}

/// How a program is drawn without a vendor icon: its logo, or a Mac symbol.
public enum ProgramIcon: Equatable, Sendable {
    case brand(String)
    case symbol(String)
}

/// A program as hive shows it: a name and an icon.
public struct Program: Equatable, Sendable {
    public var name: String
    public var icon: ProgramIcon
    /// The program's own icon (VendorIcon.all), drawn in preference to the
    /// logo or symbol.
    public var vendor: String?

    public init(_ name: String, _ icon: ProgramIcon, vendor: String? = nil) {
        self.name = name; self.icon = icon; self.vendor = vendor
    }
}

public enum Programs {
    /// By the name the server reports (see programOf in internal/block).
    public static let table: [String: Program] = [
        "claude": Program("Claude Code", .brand("claude"), vendor: "claude"),
        "codex": Program("Codex", .symbol("hexagon"), vendor: "codex"),
        "gemini": Program("Gemini", .symbol("sparkles"), vendor: "gemini"),
        "grok": Program("Grok", .symbol("bolt"), vendor: "grok"),
        "opencode": Program("OpenCode", .brand("opencode"), vendor: "opencode"),
        "ollama": Program("Ollama", .brand("ollama"), vendor: "ollama"),

        "node": Program("Node", .brand("nodedotjs")),
        "bun": Program("Bun", .brand("bun"), vendor: "bun"),
        "deno": Program("Deno", .symbol("hexagon.fill")),
        "python": Program("Python", .brand("python"), vendor: "python"),
        "ruby": Program("Ruby", .brand("ruby")),
        "perl": Program("Perl", .brand("perl")),
        "php": Program("PHP", .brand("php")),
        "lua": Program("Lua", .brand("lua")),
        "go": Program("Go", .brand("go"), vendor: "go"),
        "cargo": Program("Cargo", .brand("rust"), vendor: "rust"),
        "rustc": Program("Rust", .brand("rust"), vendor: "rust"),
        "zig": Program("Zig", .brand("zig")),
        "swift": Program("Swift", .brand("swift")),
        "kotlin": Program("Kotlin", .brand("kotlin")),
        "elixir": Program("Elixir", .brand("elixir")),
        "iex": Program("Elixir", .brand("elixir")),
        "mix": Program("Mix", .brand("elixir")),

        "nvim": Program("Neovim", .brand("neovim")),
        "vim": Program("Vim", .brand("vim")),
        "vi": Program("Vim", .brand("vim")),
        "hx": Program("Helix", .brand("helix")),
        "emacs": Program("Emacs", .symbol("chevron.left.forwardslash.chevron.right")),

        "git": Program("Git", .brand("git")),
        "lazygit": Program("Git Changes", .brand("git")),
        "tig": Program("Git History", .brand("git")),

        "docker": Program("Docker", .brand("docker"), vendor: "docker"),
        "podman": Program("Podman", .symbol("shippingbox")),
        "kubectl": Program("Kubernetes", .brand("kubernetes")),
        "k9s": Program("Kubernetes", .brand("kubernetes")),
        "tofu": Program("OpenTofu", .brand("opentofu")),
        "terraform": Program("Terraform", .symbol("square.stack.3d.up")),
        "psql": Program("PostgreSQL", .brand("postgresql")),
        "postgres": Program("PostgreSQL", .brand("postgresql")),
        "mysql": Program("MySQL", .brand("mysql")),
        "sqlite3": Program("SQLite", .brand("sqlite")),
        "redis-cli": Program("Redis", .brand("redis")),
        "redis-server": Program("Redis", .brand("redis")),

        "zsh": Program("Zsh", .brand("zsh")),
        "bash": Program("Bash", .symbol("apple.terminal")),
        "tmux": Program("tmux", .brand("tmux")),
        "htop": Program("htop", .brand("htop")),
        "btop": Program("btop", .symbol("chart.bar.xaxis")),
        "top": Program("top", .symbol("chart.bar.xaxis")),
        "ssh": Program("SSH", .symbol("network")),
        "mosh": Program("Mosh", .symbol("network")),
    ]

    /// The program for a name the server reports, or a terminal under that
    /// name. Names compare without case: macOS calls Python "Python".
    public static func lookup(_ name: String) -> Program {
        if name.isEmpty { return Program("shell", .symbol("apple.terminal")) }
        return table[name.lowercased()] ?? Program(name, .symbol("apple.terminal"))
    }
}

/// Colour contrast as WCAG measures it, for drawing logos on their tiles.
public enum Contrast {
    static func channels(_ c: UInt32) -> (Double, Double, Double) {
        (Double((c >> 16) & 0xFF) / 255, Double((c >> 8) & 0xFF) / 255, Double(c & 0xFF) / 255)
    }

    static func luminance(_ c: UInt32) -> Double {
        func lin(_ v: Double) -> Double { v <= 0.03928 ? v / 12.92 : pow((v + 0.055) / 1.055, 2.4) }
        let (r, g, b) = channels(c)
        return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
    }

    public static func ratio(_ a: UInt32, _ b: UInt32) -> Double {
        let la = luminance(a), lb = luminance(b)
        return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
    }

    /// A colour laid over another at an opacity.
    public static func blend(_ top: UInt32, over bottom: UInt32, alpha: Double) -> UInt32 {
        let (tr, tg, tb) = channels(top), (br, bg, bb) = channels(bottom)
        func mix(_ t: Double, _ b: Double) -> UInt32 { UInt32((t * alpha + b * (1 - alpha)) * 255 + 0.5) }
        return mix(tr, br) << 16 | mix(tg, bg) << 8 | mix(tb, bb)
    }

    /// The glyph colour on a tile filled with a brand colour, like an app
    /// icon: white, unless the colour is too light for white to read (3:1,
    /// WCAG's bar for graphics); then black.
    public static func onFill(_ fill: UInt32) -> UInt32 {
        ratio(0xFFFFFF, fill) >= 3 ? 0xFFFFFF : 0x000000
    }
}
