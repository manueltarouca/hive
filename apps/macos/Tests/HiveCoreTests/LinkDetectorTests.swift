import Foundation
import Testing
@testable import HiveCore

/// A row as columns: one string per cell ("" for a wide character's second
/// cell), as the terminal holds it.
private func cells(_ s: String) -> [String] { s.map { String($0) } }

@Suite struct LinkDetectorTests {
    @Test func urlUnderTheColumn() {
        let row = cells("see https://github.com/manueltarouca/hive for more")
        let l = LinkDetector.link(in: row, at: 10)
        #expect(l?.target == "https://github.com/manueltarouca/hive")
        #expect(l?.columns == 4..<41)
        #expect(LinkDetector.link(in: row, at: 2) == nil) // "see"
        #expect(LinkDetector.link(in: row, at: 42) == nil) // "for"
    }

    /// Sentence punctuation after a link isn't part of it; balanced
    /// parentheses inside one are.
    @Test func trailingPunctuation() {
        #expect(LinkDetector.link(in: cells("Open https://hive.matm.xyz."), at: 8)?.target == "https://hive.matm.xyz")
        #expect(LinkDetector.link(in: cells("(see https://a.io/x)"), at: 8)?.target == "https://a.io/x")
        #expect(LinkDetector.link(in: cells("https://en.wikipedia.org/wiki/Hive_(game), ok"), at: 3)?.target
            == "https://en.wikipedia.org/wiki/Hive_(game)")
        #expect(LinkDetector.link(in: cells("\"https://a.io/q?x=1\""), at: 4)?.target == "https://a.io/q?x=1")
    }

    @Test func paths() {
        let row = cells("Read(/Users/alex/code/projects/hive/apps/macos/build/shot.png)")
        #expect(LinkDetector.link(in: row, at: 10)?.target == "/Users/alex/code/projects/hive/apps/macos/build/shot.png")
        #expect(LinkDetector.link(in: cells("wrote ~/notes/today.md"), at: 9)?.target == "~/notes/today.md")
        #expect(LinkDetector.link(in: cells("in ./internal/server/attach.go:153:9 here"), at: 6)?.target == "./internal/server/attach.go:153:9")
        #expect(LinkDetector.link(in: cells("see ../README.md"), at: 6)?.target == "../README.md")
        // Not paths: a lone slash, a fraction, a date.
        #expect(LinkDetector.link(in: cells("a / b"), at: 2) == nil)
        #expect(LinkDetector.link(in: cells("1/2 done"), at: 1) == nil)
        #expect(LinkDetector.link(in: cells("on 2026/10/19"), at: 6) == nil)
    }

    /// Wide characters take two cells; columns still line up.
    @Test func wideCharacters() {
        var row = ["日", "", "本", "", " "]
        row += cells("https://x.io")
        let l = LinkDetector.link(in: row, at: 7)
        #expect(l?.target == "https://x.io")
        #expect(l?.columns == 5..<17)
    }

    @Test func fileTarget() {
        // A line and column after a path aren't part of the file.
        #expect(LinkDetector.fileTarget("./a/b.go:153:9") == ("./a/b.go", 153))
        #expect(LinkDetector.fileTarget("/tmp/x.png") == ("/tmp/x.png", nil))
        #expect(LinkDetector.fileTarget("~/a.md:12") == ("~/a.md", 12))
    }
}

/// The mirror finds links in what a program printed: OSC 8 hyperlinks
/// (whatever their text says) and URLs or paths in plain text.
@Suite struct MirrorLinkTests {
    @Test func osc8AndPlain() throws {
        let m = try Mirror(cols: 80, rows: 4)
        m.feed(Data("open \u{1b}]8;;file:///Users/alex/a.png\u{1b}\\a.png\u{1b}]8;;\u{1b}\\ now\r\n".utf8))
        m.feed(Data("see https://hive.matm.xyz/docs.\r\n".utf8))
        let l = m.link(col: 6, row: 0)
        #expect(l?.target == "file:///Users/alex/a.png")
        #expect(l?.columns == 5..<10)
        #expect(m.link(col: 1, row: 0) == nil)
        #expect(m.link(col: 10, row: 1)?.target == "https://hive.matm.xyz/docs")
        #expect(m.link(col: 79, row: 3) == nil)
    }
}

/// Files for the policy tests, in a temp dir: what a ⌘-click may open and
/// what it must never run.
private final class Fixtures {
    let dir: String
    init() throws {
        dir = NSTemporaryDirectory() + "hive-links-" + UUID().uuidString.prefix(8)
        let fm = FileManager.default
        try fm.createDirectory(atPath: dir + "/docs", withIntermediateDirectories: true)
        try fm.createDirectory(atPath: dir + "/Evil.app/Contents", withIntermediateDirectories: true)
        fm.createFile(atPath: dir + "/shot.png", contents: Data([0x89, 0x50, 0x4E, 0x47]))
        fm.createFile(atPath: dir + "/notes.md", contents: Data("# notes".utf8))
        fm.createFile(atPath: dir + "/run.command", contents: Data("#!/bin/sh\necho hi".utf8))
        fm.createFile(atPath: dir + "/tool", contents: Data("#!/bin/sh\necho hi".utf8), attributes: [.posixPermissions: 0o755])
        // A harmless-looking name for something that runs.
        try fm.createSymbolicLink(atPath: dir + "/picture.png", withDestinationPath: dir + "/run.command")
    }
    deinit { try? FileManager.default.removeItem(atPath: dir) }
}

@Suite struct LinkActionTests {
    let home = "/Users/alex"
    func resolve(_ t: String, cwd: String? = nil, local: Bool = true) -> LinkAction {
        LinkAction.resolve(t, cwd: cwd, home: home, local: local)
    }

    @Test func web() {
        #expect(resolve("https://hive.matm.xyz") == .open(URL(string: "https://hive.matm.xyz")!))
        #expect(resolve("HTTPS://hive.matm.xyz/x") == .open(URL(string: "HTTPS://hive.matm.xyz/x")!))
        // A web link opens from a remote pane too.
        #expect(resolve("https://hive.matm.xyz", local: false) == .open(URL(string: "https://hive.matm.xyz")!))
        #expect(resolve("mailto:mt@example.com") == .open(URL(string: "mailto:mt@example.com")!))
        // No host, no address: refused.
        #expect(resolve("https:relative") == .none("not a web address"))
        #expect(resolve("mailto:") == .none("not a link hive opens"))
    }

    /// Any other scheme reaches whatever app claims it: ask first.
    @Test func otherSchemesAsk() {
        #expect(resolve("ssh://user@box") == .confirm(URL(string: "ssh://user@box")!))
        #expect(resolve("ftp://files.example.com/x") == .confirm(URL(string: "ftp://files.example.com/x")!))
    }

    /// Invisible, control or text-reversing characters could make a link
    /// look like something else.
    @Test func unsafeCharacters() {
        #expect(resolve("https://a.io/x\u{1}y") == .none("hidden characters in the link"))
        #expect(resolve("https://a.io/\u{202E}gpj.exe") == .none("hidden characters in the link"))
        #expect(resolve("https://a\u{200B}pple.com") == .none("hidden characters in the link"))
    }

    @Test func files() throws {
        let f = try Fixtures()
        let real = { (p: String) in URL(fileURLWithPath: p).resolvingSymlinksInPath() }
        #expect(resolve(f.dir + "/shot.png") == .open(real(f.dir + "/shot.png")))
        #expect(resolve("./notes.md:12:3", cwd: f.dir) == .open(real(f.dir + "/notes.md")))
        #expect(resolve("../shot.png", cwd: f.dir + "/docs") == .open(real(f.dir + "/shot.png")))
        #expect(resolve("file://" + f.dir + "/shot.png") == .open(real(f.dir + "/shot.png")))
        #expect(resolve("file://localhost" + f.dir + "/shot.png") == .open(real(f.dir + "/shot.png")))
        // A folder opens in Finder.
        #expect(resolve(f.dir + "/docs") == .open(real(f.dir + "/docs")))
        #expect(resolve("~/x", local: true) == .none("no such file"))
        #expect(resolve(f.dir + "/missing.png") == .none("no such file"))
    }

    /// Opening these would run code: they're shown in Finder instead.
    @Test func neverRun() throws {
        let f = try Fixtures()
        let real = { (p: String) in URL(fileURLWithPath: p).resolvingSymlinksInPath() }
        #expect(resolve(f.dir + "/run.command") == .reveal(real(f.dir + "/run.command")))
        #expect(resolve(f.dir + "/tool") == .reveal(real(f.dir + "/tool")))
        #expect(resolve(f.dir + "/Evil.app") == .reveal(real(f.dir + "/Evil.app")))
        // Through a symlink with an innocent name.
        #expect(resolve(f.dir + "/picture.png") == .reveal(real(f.dir + "/run.command")))
    }

    /// A remote pane's paths are on that machine: nothing on this Mac to open.
    @Test func remoteAndForeignFiles() {
        #expect(resolve("/home/ubuntu/a.png", local: false) == .none("a path on another machine"))
        #expect(resolve("file:///etc/hosts", local: false) == .none("a path on another machine"))
        #expect(resolve("file://hive/home/ubuntu/a.png") == .none("a file on another machine"))
        #expect(resolve("file:///tmp/x.png?y=1") == .none("not a file link hive opens"))
        #expect(resolve("./a.go") == .none("a relative path with no directory to start from"))
    }
}
