import Foundation
import Testing
@testable import HiveKit

/// Runs a real hive server (bin/hive from the repository) in a temp directory.
final class TestServer {
    let dir: URL
    let socket: String
    let process = Process()

    init() throws {
        dir = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("hivekit-\(UUID().uuidString.prefix(8))")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        // Unix socket paths are limited to 104 bytes; keep it short.
        socket = "/tmp/hk-\(UUID().uuidString.prefix(8)).sock"
        let repo = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        process.executableURL = repo.appendingPathComponent("bin/hive")
        process.arguments = ["server", "run", "--socket", socket, "--state-dir", dir.appendingPathComponent("state").path]
        // Its own socket, state and settings, always together: never the
        // user's settings file (the server reads HIVE_CONFIG).
        process.environment = ProcessInfo.processInfo.environment.merging([
            "HIVE_SERVER": socket, "HIVE_STATE_DIR": dir.appendingPathComponent("state").path,
            "HIVE_CONFIG": dir.appendingPathComponent("config").path, "HIVE_SESSION": "", "HIVE_BLOCK": "",
        ]) { $1 }
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        try process.run()
        for _ in 0..<200 where !FileManager.default.fileExists(atPath: socket) { usleep(20_000) }
    }

    func stop() {
        process.terminate()
        process.waitUntilExit()
    }
}

/// Bridges the queue-confined client to async tests.
final class Harness: @unchecked Sendable {
    let queue = DispatchQueue(label: "test.vt")
    let client: HiveClient
    init(_ s: TestServer) { client = HiveClient(socketPath: s.socket, queue: queue) }

    func connect() async throws -> String {
        try await withCheckedThrowingContinuation { c in
            queue.async { self.client.connect(name: "tests", version: "0.0.0-test") { c.resume(with: $0) } }
        }
    }

    func call(_ method: String, _ params: JSON = [:]) async throws -> JSON {
        try await withCheckedThrowingContinuation { c in
            queue.async { self.client.call(method, params) { c.resume(with: $0.mapError { $0 as Error }) } }
        }
    }
}

final class Updates: @unchecked Sendable {
    private let lock = NSLock()
    private var items: [BlockUpdate] = []
    func add(_ u: BlockUpdate) { lock.lock(); items.append(u); lock.unlock() }
    var all: [BlockUpdate] { lock.lock(); defer { lock.unlock() }; return items }

    func wait(_ cond: ([BlockUpdate]) -> Bool) async -> Bool {
        for _ in 0..<250 {
            if cond(all) { return true }
            try? await Task.sleep(nanoseconds: 20_000_000)
        }
        return false
    }
}

@Suite(.serialized) struct HiveClientTests {
    /// The test's server reads a settings file in the test's own dir, never
    /// the user's.
    @Test func serverUsesItsOwnSettings() async throws {
        let s = try TestServer(); defer { s.stop() }
        let h = Harness(s)
        _ = try await h.connect()
        let path = try await h.call("config.get")["path"].string ?? ""
        #expect(path.hasPrefix(s.dir.path + "/"), "settings file: \(path)")
    }

    @Test func callsAndErrors() async throws {
        let s = try TestServer(); defer { s.stop() }
        let h = Harness(s)
        let id = try await h.connect()
        #expect(id.hasPrefix("client:"))
        let v = try await h.call("server.version")
        #expect(v["hive_version"].string != nil)
        await #expect(throws: HiveError.self) { _ = try await h.call("session.view", ["session_id": "session:nope"]) }
    }

    @Test func blockStreamSnapshotOutputResize() async throws {
        let s = try TestServer(); defer { s.stop() }
        let h = Harness(s)
        _ = try await h.connect()
        let created = try await h.call("session.create", ["command": ["cat"], "cols": 80, "rows": 24])
        let block = try #require(created["block_ids"][0].string)

        let ups = Updates()
        h.queue.async { h.client.subscribe(block: block) { ups.add($0) } }
        #expect(await ups.wait { $0.contains { if case .snapshot = $0 { return true }; return false } })

        h.queue.async {
            h.client.input(block: block, Data("ping\r".utf8))
            h.client.resize(block: block, cols: 60, rows: 20, cellWidth: 9, cellHeight: 18, take: true)
        }
        #expect(await ups.wait { all in
            let text = all.compactMap { if case .output(_, let d) = $0 { return String(decoding: d, as: UTF8.self) }; return nil }.joined()
            let resized = all.contains { if case .resize(_, 60, 20, 9, 18) = $0 { return true }; return false }
            return text.contains("ping") && resized
        })

        // Seq is continuous: each update starts where the last output ended.
        var next: UInt64?
        for u in ups.all {
            switch u {
            case .snapshot(let seq, _): next = seq
            case .output(let seq, let d): #expect(seq == next); next = seq + UInt64(d.count)
            case .resize(let seq, _, _, _, _): #expect(seq == next)
            default: break
            }
        }
    }

    @Test func eventsAcknowledged() async throws {
        let s = try TestServer(); defer { s.stop() }
        let h = Harness(s)
        _ = try await h.connect()
        let got = Updates()
        h.queue.async {
            h.client.onEventsSubscribed = { _ in got.add(.exited) }
            h.client.subscribeEvents(all: true)
        }
        #expect(await got.wait { !$0.isEmpty })
    }
}
