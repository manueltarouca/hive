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
            queue.async { self.client.connect(name: "engine-tests") { c.resume(with: $0) } }
        }
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

    @Test func keysAndPasteReachTheProgram() async throws {
        let server = try Server(); defer { server.stop() }
        let rig = Rig(server)
        try await rig.connect()
        // The program reads exactly the bytes it was sent and prints them in hex.
        let created = try await rig.call("session.create", ["command": ["sh", "-c", "stty raw -echo; head -c 6 | od -An -tx1; stty sane; head -c 18 | od -An -c"], "cols": 80, "rows": 24])
        let block = try #require(created["block_ids"][0].string)
        await rig.onQueue { rig.engine.attach(block: block) { _ in } }
        try await Task.sleep(nanoseconds: 300_000_000)
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
