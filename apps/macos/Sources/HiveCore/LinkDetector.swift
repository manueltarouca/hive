import Foundation
import UniformTypeIdentifiers

/// A link in a terminal row: what it points to and the columns it covers.
public struct TerminalLink: Equatable, Sendable {
    public var target: String
    public var columns: Range<Int>
    public init(target: String, columns: Range<Int>) { self.target = target; self.columns = columns }
}

/// Finds URLs and file paths in a terminal row, as Ghostty does for text a
/// program printed without OSC 8 hyperlinks (Claude Code's paths, a build's
/// error locations): http(s), file, ftp and ssh URLs, and absolute, ~/, ./
/// and ../ paths, with a :line:col after a path allowed.
public enum LinkDetector {
    // A URL runs to whitespace, a quote or an angle bracket.
    private static let url = try! NSRegularExpression(pattern: #"(?:https?|ftp|file|ssh)://[^\s"'<>`]+|mailto:[^\s"'<>`]+"#, options: .caseInsensitive)
    // A path starts after a boundary (so 1/2 and 2026/10/19 aren't paths)
    // and has something after its first slash.
    private static let path = try! NSRegularExpression(pattern: #"(?<![\w./~-])(?:~|\.{1,2})?/[^\s"'<>`()\[\]{},;|]+"#)

    /// The link covering a column. `cells` is the row, one string per cell:
    /// a character, " " for an empty cell, "" for a wide character's
    /// second cell.
    public static func link(in cells: [String], at column: Int) -> TerminalLink? {
        links(in: cells).first { $0.columns.contains(column) }
    }

    /// Every link in a row, in order.
    public static func links(in cells: [String]) -> [TerminalLink] {
        // The row as text, each character remembering its column and width.
        var text = ""
        var cols: [Int] = [], widths: [Int] = [], utf16Starts: [Int] = []
        var offset = 0
        for (i, c) in cells.enumerated() where !c.isEmpty {
            let ch = c == "\0" ? " " : c
            utf16Starts.append(offset)
            cols.append(i)
            widths.append(i + 1 < cells.count && cells[i + 1].isEmpty ? 2 : 1)
            text += ch
            offset += ch.utf16.count
        }
        utf16Starts.append(offset)
        let ns = text as NSString
        let all = NSRange(location: 0, length: ns.length)
        let urls = url.matches(in: text, range: all)
        // A path inside a URL (its //host/…) is the URL's.
        let paths = path.matches(in: text, range: all).filter { p in !urls.contains { NSIntersectionRange($0.range, p.range).length > 0 } }
        var found: [TerminalLink] = []
        for m in urls + paths {
            let raw = ns.substring(with: m.range)
            let target = trimmed(raw)
            guard isLink(target) else { continue }
            // Character indexes of the match's first and last characters.
            guard let first = utf16Starts.firstIndex(where: { $0 >= m.range.location }) else { continue }
            let endUTF16 = m.range.location + (target as NSString).length
            guard let afterLast = utf16Starts.firstIndex(where: { $0 >= endUTF16 }), afterLast > first else { continue }
            let last = afterLast - 1
            found.append(TerminalLink(target: target, columns: cols[first]..<(cols[last] + widths[last])))
        }
        return found.sorted { $0.columns.lowerBound < $1.columns.lowerBound }
    }

    /// Sentence punctuation after a link isn't part of it; a closing bracket
    /// is, only when the link opened it.
    static func trimmed(_ s: String) -> String {
        var t = Substring(s)
        while let last = t.last {
            if ".,;:!?'\"".contains(last) {
                t = t.dropLast()
            } else if let open = [")": "(", "]": "[", "}": "{"][String(last)] {
                let opens = t.filter { String($0) == open }.count, closes = t.filter { $0 == last }.count
                if closes > opens { t = t.dropLast() } else { break }
            } else {
                break
            }
        }
        return String(t)
    }

    private static func isLink(_ s: String) -> Bool {
        if s.contains("://") || s.hasPrefix("mailto:") { return s.count > 8 }
        // A path names something after its slash.
        guard let slash = s.firstIndex(of: "/") else { return false }
        return s[s.index(after: slash)...].contains { $0.isLetter || $0.isNumber || $0 == "_" || $0 == "." }
    }

    /// A path with a :line or :line:col after it: the file, and the line.
    public static func fileTarget(_ s: String) -> (path: String, line: Int?) {
        var parts = s.split(separator: ":", omittingEmptySubsequences: false).map(String.init)
        var numbers: [Int] = []
        while parts.count > 1, let n = Int(parts.last!) {
            numbers.insert(n, at: 0)
            parts.removeLast()
        }
        guard !numbers.isEmpty, numbers.count <= 2 else { return (s, nil) }
        return (parts.joined(separator: ":"), numbers.first)
    }
}

/// What ⌘-clicking a link does. Terminal output is untrusted: this follows
/// Ghostty's policy for it (macos/Sources/Helpers/UntrustedURL.swift, MIT,
/// Mitchell Hashimoto and Ghostty contributors). Web and mail links open;
/// any other scheme asks first; a file opens only if opening it can't run
/// code (otherwise it's shown in Finder), judged by its real target.
public enum LinkAction: Equatable, Sendable {
    /// Open: a web or mail link, a file in its app, a folder in Finder.
    case open(URL)
    /// Another scheme, which reaches whatever app claims it: ask first.
    case confirm(URL)
    /// A file that would run if opened (an app, a script, an installer,
    /// anything executable): show it in Finder instead.
    case reveal(URL)
    /// Nothing to open, and why.
    case none(String)

    /// target: what the link points to. cwd: the pane's directory. home:
    /// this Mac's home. local: the pane is on this Mac (a remote host's
    /// paths aren't this Mac's). hostnames: this Mac's names, which a
    /// file:// link may carry (as ls --hyperlink writes them).
    public static func resolve(_ target: String, cwd: String?, home: String, local: Bool, hostnames: [String] = []) -> LinkAction {
        guard !target.isEmpty else { return .none("an empty link") }
        // Checked before parsing: URL(string:) percent-encodes these, and
        // they can make a link look like something else.
        guard !target.unicodeScalars.contains(where: unsafe) else { return .none("hidden characters in the link") }
        // A scheme: a letter, then letters, digits, + . or -, then a colon
        // (more than one letter: C: is a drive, not a scheme).
        if let colon = target.firstIndex(of: ":") {
            let scheme = target[..<colon]
            if scheme.count > 1, scheme.first?.isLetter == true, scheme.allSatisfy({ $0.isLetter || $0.isNumber || "+.-".contains($0) }) {
                return url(target, scheme: scheme.lowercased(), local: local, hostnames: hostnames)
            }
        }
        guard local else { return .none("a path on another machine") }
        let path = LinkDetector.fileTarget(target).path
        var full: String
        if path.hasPrefix("~/") { full = home + path.dropFirst(1) }
        else if path.hasPrefix("/") { full = path }
        else if let cwd, path.hasPrefix("./") || path.hasPrefix("../") { full = cwd + "/" + path }
        else { return .none("a relative path with no directory to start from") }
        return file(URL(fileURLWithPath: full))
    }

    private static func url(_ target: String, scheme: String, local: Bool, hostnames: [String]) -> LinkAction {
        guard let u = URL(string: target) else { return .none("not a link hive opens") }
        switch scheme {
        case "http", "https":
            guard let h = u.host, !h.isEmpty else { return .none("not a web address") }
            return .open(u)
        case "mailto":
            guard let c = URLComponents(url: u, resolvingAgainstBaseURL: false), !c.path.isEmpty else { return .none("not a link hive opens") }
            return .open(u)
        case "file":
            guard local else { return .none("a path on another machine") }
            guard u.query == nil, u.fragment == nil else { return .none("not a file link hive opens") }
            if let h = u.host?.lowercased(), !h.isEmpty, h != "localhost", !hostnames.contains(where: { $0.lowercased() == h }) {
                return .none("a file on another machine")
            }
            return file(u)
        default:
            return .confirm(u)
        }
    }

    /// A file by what it really is: symlinks resolved, dot segments gone.
    private static func file(_ u: URL) -> LinkAction {
        let real = u.standardizedFileURL.resolvingSymlinksInPath()
        guard let v = try? real.resourceValues(forKeys: [.contentTypeKey, .isDirectoryKey, .isExecutableKey, .isRegularFileKey]) else {
            return .none("no such file")
        }
        // Not devices, sockets or other special files.
        guard v.isDirectory == true || v.isRegularFile == true else { return .none("not a file or folder") }
        if unsafeExtensions.contains(real.pathExtension.lowercased()) { return .reveal(real) }
        if let t = v.contentType, [UTType.application, .executable, .script].contains(where: { t.conforms(to: $0) }) { return .reveal(real) }
        if v.isDirectory != true, v.isExecutable == true { return .reveal(real) }
        return .open(real)
    }

    /// Opened, these run code even when they aren't marked executable.
    static let unsafeExtensions: Set<String> = [
        "action", "app", "applescript", "class", "command", "desktop", "inetloc", "jar", "mobileconfig",
        "mpkg", "pkg", "scpt", "terminal", "tool", "url", "webloc", "workflow",
    ]

    /// Controls, line breaks, zero-width and direction-changing characters.
    static func unsafe(_ s: Unicode.Scalar) -> Bool {
        switch s.value {
        case 0x00...0x1F, 0x7F...0x9F, 0x061C, 0x200B...0x200F, 0x202A...0x202E, 0x2066...0x2069,
             0x2028...0x2029, 0x2060, 0xFEFF: return true
        default: return false
        }
    }
}
