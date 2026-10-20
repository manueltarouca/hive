import Foundation
import Testing
import HiveKit
@testable import HiveCore

/// A real hive server in a temp directory (bin/hive from the repository).
final class Server {
    let socket = "/tmp/hc-\(UUID().uuidString.prefix(8)).sock"
    let state = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("hc-\(UUID().uuidString.prefix(8))")
    let process = Process()
    init() throws {
        let repo = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        process.executableURL = repo.appendingPathComponent("bin/hive")
        process.arguments = ["server", "run", "--socket", socket, "--state-dir", state.path]
        // Its own socket, state and settings, always together: never the
        // user's settings file (the server reads HIVE_CONFIG).
        process.environment = ProcessInfo.processInfo.environment.merging([
            "HIVE_SERVER": socket, "HIVE_STATE_DIR": state.path,
            "HIVE_CONFIG": state.appendingPathComponent("config").path, "HIVE_SESSION": "", "HIVE_BLOCK": "",
        ]) { $1 }
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        try process.run()
        for _ in 0..<200 where !FileManager.default.fileExists(atPath: socket) { usleep(20_000) }
    }
    func stop() { process.terminate(); process.waitUntilExit() }
}

final class Box<T>: @unchecked Sendable {
    private let lock = NSLock()
    private var v: T
    init(_ v: T) { self.v = v }
    var value: T { get { lock.lock(); defer { lock.unlock() }; return v } set { lock.lock(); v = newValue; lock.unlock() } }
}

final class Rig: @unchecked Sendable {
    let queue = DispatchQueue(label: "hive.vt.test")
    let client: HiveClient
    let engine: Engine
    init(_ s: Server) {
        client = HiveClient(socketPath: s.socket, queue: queue)
        engine = Engine(client: client)
    }
    func connect() async throws {
        _ = try await withCheckedThrowingContinuation { (c: CheckedContinuation<String, Error>) in
            queue.async { self.client.connect(name: "engine-tests", version: "0.0.0-test") { c.resume(with: $0) } }
        }
    }
    /// Waits until the block's screen shows text: a program saying it's ready.
    func waitForScreen(_ block: String, _ text: String) async throws {
        for _ in 0..<250 {
            let screen = try await call("hive.terminal.format", ["block_id": .string(block), "args": ["trim": true]])["content"].string ?? ""
            if screen.contains(text) { return }
            try await Task.sleep(nanoseconds: 20_000_000)
        }
        Issue.record("the screen never showed \(text)")
    }

    func call(_ method: String, _ params: JSON = [:]) async throws -> JSON {
        try await withCheckedThrowingContinuation { c in
            queue.async { self.client.call(method, params) { c.resume(with: $0.mapError { $0 as Error }) } }
        }
    }
    func onQueue<T>(_ f: @escaping () -> T) async -> T {
        await withCheckedContinuation { c in queue.async { c.resume(returning: f()) } }
    }
}

/// A deterministic stream of VT output that exercises the parser: colors,
/// cursor movement, erasing, scroll regions, wide characters, the alternate
/// screen, line wrapping, and sequences split across writes.
func randomVT(seed: UInt64, count: Int) -> Data {
    var rng = seed
    func next(_ n: Int) -> Int { rng = rng &* 6364136223846793005 &+ 1442695040888963407; return Int((rng >> 33) % UInt64(n)) }
    let words = ["hello", "wörld", "你好", "🙂", "box─┼─", "tab\there", String(repeating: "x", count: 97)]
    var s = ""
    for _ in 0..<count {
        switch next(12) {
        case 0: s += "\u{1b}[\(30 + next(8));\(40 + next(8))m"
        case 1: s += "\u{1b}[38;2;\(next(256));\(next(256));\(next(256))m"
        case 2: s += "\u{1b}[\(1 + next(30));\(1 + next(90))H"
        case 3: s += "\u{1b}[\(next(3))K"
        case 4: s += "\u{1b}[\(next(3))J"
        case 5: s += "\u{1b}[\(1 + next(5));\(10 + next(10))r"
        case 6: s += "\u{1b}[r"
        case 7: s += next(2) == 0 ? "\u{1b}[?1049h" : "\u{1b}[?1049l"
        case 8: s += "\r\n"
        case 9: s += "\u{1b}[0m"
        default: s += words[next(words.count)] + " "
        }
    }
    return Data(s.utf8)
}

@Suite(.serialized) struct EngineTests {
    /// The test's server reads a settings file in the test's own dir, never
    /// the user's.
    @Test func serverUsesItsOwnSettings() async throws {
        let s = try Server(); defer { s.stop() }
        let rig = Rig(s)
        try await rig.connect()
        let path = try await rig.call("config.get")["path"].string ?? ""
        #expect(path.hasPrefix(s.state.path + "/"), "settings file: \(path)")
    }

    @Test func mirrorMatchesServerOnRandomStreams() async throws {
        let server = try Server(); defer { server.stop() }
        let rig = Rig(server)
        try await rig.connect()
        for seed: UInt64 in [1, 2, 3, 7, 42] {
            let file = server.state.path + "-vt-\(seed)"
            try randomVT(seed: seed, count: 1500).write(to: URL(fileURLWithPath: file))
            let created = try await rig.call("session.create", ["command": ["sh", "-c", .string("cat \(file); exec cat")], "cols": 100, "rows": 30])
            let block = try #require(created["block_ids"][0].string)
            let frames = Box(0)
            await rig.onQueue { rig.engine.attach(block: block) { _ in frames.value += 1 } }

            // Mid-stream resize, to check reflow happens at the same point.
            try await Task.sleep(nanoseconds: 50_000_000)
            _ = try await rig.call("hive.terminal.resize", ["block_id": .string(block), "args": ["cols": 73, "rows": 21]])
            _ = try await rig.call("hive.terminal.write", ["block_id": .string(block), "args": ["data": "tail-marker\r"]])

            var mine = "", theirs = ""
            for _ in 0..<150 {
                theirs = try await rig.call("hive.terminal.format", ["block_id": .string(block), "args": ["trim": true]])["content"].string ?? ""
                mine = await rig.onQueue { rig.engine.plainText(block: block) } ?? ""
                if theirs.contains("tail-marker"), mine == theirs { break }
                try await Task.sleep(nanoseconds: 20_000_000)
            }
            #expect(mine == theirs, "seed \(seed): mirror diverged")
            #expect(frames.value > 0)
            await rig.onQueue { rig.engine.detach(block: block) }
        }
        #expect(await rig.onQueue { rig.engine.resyncCount } == 0)
    }

    /// A program that asked for the mouse gets wheel reports through the
    /// server, encoded for the cell under the pointer.
    @Test func wheelReachesAProgramTrackingTheMouse() async throws {
        let server = try Server(); defer { server.stop() }
        let rig = Rig(server)
        try await rig.connect()
        let created = try await rig.call("session.create", ["command": ["sh", "-c", "printf '\\033[?1049h\\033[?1000h\\033[?1006h'; stty raw -echo; printf ready; head -c 10 | od -An -c"], "cols": 80, "rows": 24])
        let block = try #require(created["block_ids"][0].string)
        await rig.onQueue { rig.engine.attach(block: block) { _ in } }
        // Input before stty raw would be echoed: the test would read the echo.
        try await rig.waitForScreen(block, "ready")
        // Wait until the mirror has seen the mode switches.
        let geo = PointerGeometry(cellWidth: 10, cellHeight: 20, paneWidth: 800, paneHeight: 480)
        let report = Data("\u{1b}[<64;3;3M".utf8)
        for _ in 0..<100 {
            let action = await rig.onQueue { rig.engine.mirror(block: block)?.wheel(rows: -1, shift: false, x: 25, y: 45, geometry: geo) }
            if action == .send(report) { break }
            try await Task.sleep(nanoseconds: 20_000_000)
        }
        await rig.onQueue { rig.engine.wheel(block: block, rows: -1, shift: false, x: 25, y: 45, geometry: geo) }
        var out = ""
        for _ in 0..<150 {
            out = try await rig.call("hive.terminal.format", ["block_id": .string(block), "args": ["trim": true]])["content"].string ?? ""
            if out.contains("033") { break }
            try await Task.sleep(nanoseconds: 20_000_000)
        }
        // od -c shows ESC as 033: the SGR report for wheel-up at column 3, row 3.
        #expect(out.contains("033   [   <   6   4   ;   3   ;   3   M"), "got: \(out)")
    }

    /// A click in a program that tracks the mouse reaches it as press and
    /// release reports, instead of starting a local selection.
    @Test func clickReachesAProgramTrackingTheMouse() async throws {
        let server = try Server(); defer { server.stop() }
        let rig = Rig(server)
        try await rig.connect()
        let created = try await rig.call("session.create", ["command": ["sh", "-c", "printf '\\033[?1049h\\033[?1000h\\033[?1006h'; stty raw -echo; printf ready; head -c 18 | od -An -c"], "cols": 80, "rows": 24])
        let block = try #require(created["block_ids"][0].string)
        await rig.onQueue { rig.engine.attach(block: block) { _ in } }
        try await rig.waitForScreen(block, "ready")
        let geo = PointerGeometry(cellWidth: 10, cellHeight: 20, paneWidth: 800, paneHeight: 480)
        for _ in 0..<100 {
            // Stateless probe: the wheel goes to the program once tracking is on.
            let r = await rig.onQueue { rig.engine.mirror(block: block)?.wheel(rows: -1, shift: false, x: 25, y: 45, geometry: geo) }
            if case .send = r { break }
            try await Task.sleep(nanoseconds: 20_000_000)
        }
        for action in [Mirror.PointerAction.press, .release] {
            await rig.onQueue {
                rig.engine.pointer(block: block, action, button: .left, mods: [], x: 25, y: 45, geometry: geo,
                                   col: 2, row: 2, repeatInterval: 0.5, repeatDistance: 8)
            }
        }
        var out = ""
        for _ in 0..<150 {
            out = try await rig.call("hive.terminal.format", ["block_id": .string(block), "args": ["trim": true]])["content"].string ?? ""
            if out.filter({ !$0.isWhitespace }).contains("033[<0;3;3m") { break }
            try await Task.sleep(nanoseconds: 20_000_000)
        }
        // od -c wraps after 16 bytes, so compare without whitespace.
        #expect(out.filter { !$0.isWhitespace }.contains("033[<0;3;3M033[<0;3;3m"), "got: \(out)")
        #expect(await rig.onQueue { rig.engine.selectedText(block: block) } == nil)
    }

    /// Without mouse tracking, the same mouse path selects text locally.
    @Test func dragSelectsWhenTheProgramDoesNotTrackTheMouse() async throws {
        let server = try Server(); defer { server.stop() }
        let rig = Rig(server)
        try await rig.connect()
        let created = try await rig.call("session.create", ["command": ["sh", "-c", "printf 'hello world'; exec cat"], "cols": 80, "rows": 24])
        let block = try #require(created["block_ids"][0].string)
        await rig.onQueue { rig.engine.attach(block: block) { _ in } }
        for _ in 0..<100 {
            if await rig.onQueue({ rig.engine.plainText(block: block) })?.contains("hello world") == true { break }
            try await Task.sleep(nanoseconds: 20_000_000)
        }
        let geo = PointerGeometry(cellWidth: 10, cellHeight: 20, paneWidth: 800, paneHeight: 480)
        let steps: [(Mirror.PointerAction, Double, Int)] = [(.press, 2, 0), (.motion, 46, 4), (.release, 46, 4)]
        await rig.onQueue {
            for (action, x, col) in steps {
                rig.engine.pointer(block: block, action, button: .left, mods: [], x: x, y: 10, geometry: geo,
                                   col: col, row: 0, repeatInterval: 0.5, repeatDistance: 8)
            }
        }
        let sel = await rig.onQueue { rig.engine.selectedText(block: block) }
        #expect(sel == "hello", "selected: \(String(describing: sel))")
    }

    @Test func keysAndPasteReachTheProgram() async throws {
        let server = try Server(); defer { server.stop() }
        let rig = Rig(server)
        try await rig.connect()
        // The program reads exactly the bytes it was sent and prints them in hex.
        let created = try await rig.call("session.create", ["command": ["sh", "-c", "stty raw -echo; printf ready; head -c 6 | od -An -tx1; stty sane; head -c 18 | od -An -c"], "cols": 80, "rows": 24])
        let block = try #require(created["block_ids"][0].string)
        await rig.onQueue { rig.engine.attach(block: block) { _ in } }
        // Input before stty raw would be echoed and held for a newline.
        try await rig.waitForScreen(block, "ready")
        await rig.onQueue {
            rig.engine.key(block: block, KeyInput(key: .c, mods: [.ctrl], unshifted: "c"))
            rig.engine.text(block: block, "hi")
            rig.engine.key(block: block, KeyInput(key: .arrowUp))
        }
        var screen = ""
        for _ in 0..<100 {
            screen = try await rig.call("hive.terminal.format", ["block_id": .string(block), "args": ["trim": true]])["content"].string ?? ""
            if screen.contains("41") { break }
            try await Task.sleep(nanoseconds: 20_000_000)
        }
        let hex = screen.split(whereSeparator: \.isWhitespace).joined(separator: " ")
        #expect(hex.contains("03 68 69 1b 5b 41"), "got: \(screen)") // ^C, "hi", up arrow
    }
}

@Suite(.serialized) struct HoldTimeoutTests {
    // B1: a program that starts synchronized output and stalls must still be
    // drawn once the hold limit passes, without waiting for more output.
    @Test func stalledHoldIsRedrawn() async throws {
        let server = try Server(); defer { server.stop() }
        let rig = Rig(server)
        try await rig.connect()
        let created = try await rig.call("session.create", ["command": ["sh", "-c", "printf 'before'; sleep 0.5; printf '\\033[?2026h\\033[2J\\033[Hstalled-frame'; exec sleep 30"]])
        let block = try #require(created["block_ids"][0].string)
        let last = Box("")
        await rig.onQueue { rig.engine.attach(block: block) { f in last.value = f.rowText(0) } }
        var saw = false
        for _ in 0..<60 {
            if last.value.contains("stalled-frame") { saw = true; break }
            try await Task.sleep(nanoseconds: 50_000_000)
        }
        #expect(saw, "last frame: \(last.value)")
    }
}
