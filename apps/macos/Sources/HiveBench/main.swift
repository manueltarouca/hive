// hivebench-app measures the app's side, as bench/cmd/hivebench measures the
// server's: each scenario ramps one dimension against a stated limit and
// records the knee. Build it in release:
//
//	swift run -c release hivebench-app [feed|frame|pipeline|engine|resize|memory|all]
//
// Results go to bench/results/<time>-app-<scenario>.json at the repo root.
import CoreGraphics
import Darwin
import Foundation
import HiveCore
import HiveKit
import HiveRender
import Metal

// MARK: helpers

func now() -> UInt64 { DispatchTime.now().uptimeNanoseconds }
func ms(_ ns: UInt64) -> Double { Double(ns) / 1e6 }

struct Dist: Encodable {
    var n: Int, p50: Double, p99: Double, max: Double
    init(_ samples: [Double]) {
        let s = samples.sorted()
        n = s.count
        func q(_ p: Double) -> Double { s.isEmpty ? 0 : s[min(s.count - 1, Int((p * Double(s.count)).rounded(.up)) - 1)] }
        p50 = q(0.5); p99 = q(0.99); max = s.last ?? 0
    }
}

func sh(_ cmd: String) -> String {
    let p = Process()
    p.executableURL = URL(fileURLWithPath: "/bin/sh")
    p.arguments = ["-c", cmd]
    let pipe = Pipe()
    p.standardOutput = pipe
    p.standardError = FileHandle.nullDevice
    try? p.run()
    p.waitUntilExit()
    return String(decoding: pipe.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
}

let repo = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
    .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()

struct Meta: Encodable {
    var when = ISO8601DateFormatter().string(from: Date())
    var machine = sh("sysctl -n hw.model"), cpu = sh("sysctl -n machdep.cpu.brand_string")
    var os = "macOS " + sh("sw_vers -productVersion")
    var commit = sh("git -C '\(repo.path)' rev-parse --short HEAD") + (sh("git -C '\(repo.path)' status --porcelain").isEmpty ? "" : "+dirty")
    var power = sh("pmset -g batt | head -1")
    var loadBefore = sh("sysctl -n vm.loadavg")
    #if DEBUG
    var build = "debug (numbers are not comparable)"
    #else
    var build = "release"
    #endif
}

func write<T: Encodable>(_ scenario: String, _ v: T) {
    let dir = repo.appendingPathComponent("bench/results")
    try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    let f = DateFormatter()
    f.dateFormat = "yyyyMMdd-HHmmss"
    let url = dir.appendingPathComponent("\(f.string(from: Date()))-app-\(scenario).json")
    let e = JSONEncoder()
    e.outputFormatting = [.prettyPrinted, .sortedKeys]
    e.keyEncodingStrategy = .convertToSnakeCase
    try? e.encode(v).write(to: url)
    print("result:", url.path)
}

/// The process's physical footprint, as Activity Monitor's Memory column.
func footprintMB() -> Double {
    var info = task_vm_info_data_t()
    var count = mach_msg_type_number_t(MemoryLayout<task_vm_info_data_t>.size / MemoryLayout<integer_t>.size)
    let r = withUnsafeMutablePointer(to: &info) {
        $0.withMemoryRebound(to: integer_t.self, capacity: Int(count)) { task_info(mach_task_self_, task_flavor_t(TASK_VM_INFO), $0, &count) }
    }
    return r == KERN_SUCCESS ? Double(info.phys_footprint) / 1048576 : 0
}

// MARK: content

/// Output like the server benchmark's emitter: coloured lines.
func sgrLines(_ bytes: Int) -> Data {
    var s = "", n = 0
    s.reserveCapacity(bytes + 200)
    while s.utf8.count < bytes {
        n += 1
        s += "\u{1b}[3\(n % 7 + 1)m\(String(format: "%08d", n))\u{1b}[0m the quick brown fox jumps over the lazy dog \u{1b}[1mbold\u{1b}[0m \(String(n &* 2654435761, radix: 16))\r\n"
    }
    return Data(s.utf8)
}

func plainLines(_ bytes: Int) -> Data {
    var s = "", n = 0
    while s.utf8.count < bytes { n += 1; s += "line \(n) the quick brown fox jumps over the lazy dog, again and again and again\r\n" }
    return Data(s.utf8)
}

func unicodeLines(_ bytes: Int) -> Data {
    var s = "", n = 0
    while s.utf8.count < bytes { n += 1; s += "\(n) 你好世界 emoji 🙂🚀 accents ãçé box ┌──┐ nerd \u{e0b0}\r\n" }
    return Data(s.utf8)
}

/// A full-screen program redrawing every row in place (an agent's TUI,
/// htop): cursor moves and coloured segments, no scrolling.
func tuiScreen(cols: Int, rows: Int, frame: Int) -> Data {
    var s = "\u{1b}[H"
    for y in 0..<rows {
        s += "\u{1b}[\(y + 1);1H"
        var x = 0, k = 0
        while x < cols {
            let w = min(cols - x, 6 + (y + k + frame) % 9)
            s += "\u{1b}[38;5;\((y * 7 + k * 13 + frame) % 230 + 16)m" + String(repeating: Character(UnicodeScalar(UInt8(97 + (x + frame) % 26))), count: w)
            x += w; k += 1
        }
    }
    return Data((s + "\u{1b}[0m").utf8)
}

func chunks(_ d: Data, _ size: Int) -> [Data] {
    stride(from: 0, to: d.count, by: size).map { d.subdata(in: $0..<min(d.count, $0 + size)) }
}

// MARK: scenarios

/// feed: parsing output, the client's cost per byte (what --client-cost
/// stands in for in the server benchmark). No limit: it's a rate.
struct FeedRow: Encodable { var content: String; var chunk: Int; var mbPerS: Double; var usPerKB: Double }
func feed() -> [FeedRow] {
    var rows: [FeedRow] = []
    let total = 16 << 20
    for (name, data) in [("plain", plainLines(total)), ("sgr", sgrLines(total)), ("unicode", unicodeLines(total)),
                         ("tui 120x40", (0..<200).reduce(into: Data()) { $0 += tuiScreen(cols: 120, rows: 40, frame: $1) })] {
        for size in [1 << 10, 64 << 10] {
            let parts = chunks(data, size)
            let m = try! Mirror(cols: 120, rows: 40)
            m.apply(theme: .ghostty)
            for p in parts.prefix(64) { m.feed(p) }  // warm up
            let t = now()
            for p in parts { m.feed(p) }
            let s = Double(now() - t) / 1e9
            let r = FeedRow(content: name, chunk: size, mbPerS: Double(data.count) / s / 1048576, usPerKB: s / Double(data.count) * 1024 * 1e6)
            rows.append(r)
            print(String(format: "feed  %-11@ chunk %5d B | %7.1f MB/s | %6.2f µs per KB", name as NSString, size, r.mbPerS, r.usPerKB))
        }
    }
    return rows
}

/// frame: one pane's frame (Mirror.frame) and its draw (Renderer.encode on
/// the CPU, then the GPU), ramping the grid. Limit: frame + encode p99 over
/// 8 ms, half of a 60 Hz frame, the rest being the main thread's and the
/// GPU's. Two loads: every row changed (scrolling output, a TUI redraw) and
/// one row (typing).
struct FrameRow: Encodable {
    var cols: Int, rows: Int, load: String
    var frame: Dist, encode: Dist, gpu: Dist
    var degraded: Bool
}
func frames(renderer: Renderer) -> (rows: [FrameRow], knee: String) {
    var out: [FrameRow] = [], knee = "none"
    let sizes = [(80, 24), (120, 40), (200, 60), (300, 80), (400, 120), (500, 150)]
    let device = renderer.device
    for (cols, rows) in sizes {
        var allGood = true
        for load in ["all rows", "one row"] {
            let m = try! Mirror(cols: UInt16(cols), rows: UInt16(rows))
            m.apply(theme: .ghostty)
            m.feed(tuiScreen(cols: cols, rows: rows, frame: 0))
            _ = m.frame()
            let w = renderer.fonts.cellWidth * cols, h = renderer.fonts.cellHeight * rows
            let d = MTLTextureDescriptor.texture2DDescriptor(pixelFormat: renderer.pixelFormat, width: w, height: h, mipmapped: false)
            d.usage = [.renderTarget]
            d.storageMode = .private
            let tex = device.makeTexture(descriptor: d)!
            var fs: [Double] = [], es: [Double] = [], gs: [Double] = []
            for i in 0..<240 {
                m.feed(load == "all rows" ? tuiScreen(cols: cols, rows: rows, frame: i + 1) : Data("\u{1b}[\(rows);\(i % cols + 1)H\(i % 10)".utf8))
                let t0 = now()
                let f = m.frame()
                let t1 = now()
                let cb = renderer.makeCommandBuffer()!
                renderer.encode([PaneDraw(frame: f, origin: .zero, size: SIMD2(Float(w), Float(h)), focused: true)], into: tex, commandBuffer: cb)
                let t2 = now()
                cb.commit()
                cb.waitUntilCompleted()
                if i >= 20 {  // warm-up: atlas fills, caches settle
                    fs.append(ms(t1 - t0)); es.append(ms(t2 - t1)); gs.append((cb.gpuEndTime - cb.gpuStartTime) * 1000)
                }
            }
            let r = FrameRow(cols: cols, rows: rows, load: load, frame: Dist(fs), encode: Dist(es), gpu: Dist(gs),
                             degraded: Dist(fs).p99 + Dist(es).p99 > 8)
            if r.degraded { allGood = false }
            out.append(r)
            print(String(format: "frame %3dx%-3d %-8@ | frame p50 %6.2f p99 %6.2f | encode p50 %6.2f p99 %6.2f | gpu p50 %6.2f p99 %6.2f ms%@",
                         cols, rows, load as NSString, r.frame.p50, r.frame.p99, r.encode.p50, r.encode.p99, r.gpu.p50, r.gpu.p99,
                         (r.degraded ? "  DEGRADED" : "") as NSString))
        }
        if allGood { knee = "\(cols)x\(rows)" }
    }
    return (out, knee)
}

/// pipeline: the vt queue's sustained throughput, feeding 1 KB chunks (a
/// macOS pty read, and the server's frame when the app keeps up) and
/// building a frame after each, as the Engine does when frames keep pace;
/// and with the server's coalesced 16 KB frames. Limit: under the server's
/// own knee (64 MB/s for one pane) means the app is the bottleneck.
struct PipeRow: Encodable { var cols: Int; var rows: Int; var chunk: Int; var mbPerS: Double; var framesPerS: Double }
func pipeline() -> [PipeRow] {
    var out: [PipeRow] = []
    let data = sgrLines(32 << 20)
    for (cols, rows) in [(120, 40), (300, 80)] {
        for size in [1 << 10, 16 << 10] {
            let m = try! Mirror(cols: UInt16(cols), rows: UInt16(rows))
            m.apply(theme: .ghostty)
            let parts = chunks(data, size)
            var n = 0, bytes = 0
            let t = now(), end = t + 3_000_000_000
            var i = 0
            while now() < end {
                let p = parts[i % parts.count]; i += 1
                m.feed(p)
                _ = m.frame()
                n += 1; bytes += p.count
            }
            let s = Double(now() - t) / 1e9
            let r = PipeRow(cols: cols, rows: rows, chunk: size, mbPerS: Double(bytes) / s / 1048576, framesPerS: Double(n) / s)
            out.append(r)
            print(String(format: "pipe  %3dx%-3d chunk %5d B | %7.1f MB/s, %7.0f frames/s%@", cols, rows, size, r.mbPerS, r.framesPerS,
                         (r.mbPerS < 64 ? "  (below the server's 64 MB/s)" : "") as NSString))
        }
    }
    return out
}

/// memory: mirrors with full scrollback (16 MB each, as the server keeps),
/// ramped. The app keeps one per attached pane. Limit: none; it's a slope.
struct MemRow: Encodable { var mirrors: Int; var footprintMB: Double; var perMirrorMB: Double }
func memory() -> [MemRow] {
    var out: [MemRow] = [], mirrors: [Mirror] = []
    let base = footprintMB()
    let data = sgrLines(20 << 20)
    for target in [1, 2, 4, 8, 16] {
        while mirrors.count < target {
            let m = try! Mirror(cols: 120, rows: 40)
            for p in chunks(data, 64 << 10) { m.feed(p) }
            _ = m.frame()
            mirrors.append(m)
        }
        let f = footprintMB()
        let r = MemRow(mirrors: target, footprintMB: f, perMirrorMB: (f - base) / Double(target))
        out.append(r)
        print(String(format: "mem   %2d mirrors (20 MB written each) | footprint %6.0f MB, %5.1f MB per mirror", target, f, r.perMirrorMB))
    }
    return out
}


/// engine: the app's real pipeline (HiveClient and Engine on one serial
/// queue, as the app runs them) attached to a real hive server on its own
/// socket, state and settings, with one pane streaming at a ramped rate.
/// Counts frames built, resyncs and the app side's CPU (the server is
/// another process). Degraded: a resync (the app fell behind and lost its
/// stream), or the vt queue busy over 90% of a core.
struct EngineRow: Encodable {
    var rateMBps: Double, framesPerS: Double, resyncs: Int, cpuCores: Double, degraded: Bool
}

func cpuSeconds() -> Double {
    var ru = rusage()
    getrusage(RUSAGE_SELF, &ru)
    return Double(ru.ru_utime.tv_sec + ru.ru_stime.tv_sec) + Double(ru.ru_utime.tv_usec + ru.ru_stime.tv_usec) / 1e6
}

func engineScenario(cols: Int, rows: Int) -> [EngineRow] {
    let hive = repo.appendingPathComponent("bin/hive").path
    let emitter = "/tmp/hivebench"
    guard FileManager.default.isExecutableFile(atPath: hive), FileManager.default.isExecutableFile(atPath: emitter) else {
        print("engine: needs \(hive) (make build) and \(emitter) (bench/README.md)")
        return []
    }
    var out: [EngineRow] = []
    for rate in [1 << 20, 4 << 20, 16 << 20, 64 << 20] {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("hivebench-app.\(UUID().uuidString.prefix(8))")
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let env = ["HIVE_SERVER": dir.appendingPathComponent("s.sock").path,
                   "HIVE_STATE_DIR": dir.appendingPathComponent("state").path,
                   "HIVE_CONFIG": dir.appendingPathComponent("config").path, "HIVE_SESSION": "", "HIVE_BLOCK": ""]
        func run(_ args: [String]) -> String {
            let p = Process()
            p.executableURL = URL(fileURLWithPath: hive)
            p.arguments = args
            p.environment = ProcessInfo.processInfo.environment.merging(env) { $1 }
            let pipe = Pipe()
            p.standardOutput = pipe
            p.standardError = FileHandle.nullDevice
            try? p.run()
            p.waitUntilExit()
            return String(decoding: pipe.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
        }
        defer {
            _ = run(["server", "stop"])
            try? FileManager.default.removeItem(at: dir)  // the temp dir made above, nothing else
        }
        _ = run(["server", "start"])
        let created = run(["new", "bench", "--json", "--", emitter, "emit", "--rate", "\(rate)"])
        guard let obj = try? JSONSerialization.jsonObject(with: Data(created.utf8)) as? [String: Any],
              let block = (obj["block_ids"] as? [String])?.first else {
            print("engine: couldn't create the pane: \(created)")
            return out
        }
        _ = run(["api", "call", "hive.terminal.resize", "{\"block_id\":\"\(block)\",\"args\":{\"cols\":\(cols),\"rows\":\(rows),\"take\":true}}"])
        let queue = DispatchQueue(label: "vt")
        let client = HiveClient(socketPath: env["HIVE_SERVER"]!, queue: queue)
        let engine = Engine(client: client)
        var frames = 0
        let ready = DispatchSemaphore(value: 0)
        queue.async {
            client.connect(name: "hivebench-app", version: "bench") { _ in
                engine.attach(block: block) { _ in frames += 1 }
                ready.signal()
            }
        }
        _ = ready.wait(timeout: .now() + 5)
        Thread.sleep(forTimeInterval: 2)
        var f0 = 0, r0 = 0
        queue.sync { f0 = frames; r0 = engine.resyncCount }
        let c0 = cpuSeconds(), t0 = now()
        Thread.sleep(forTimeInterval: 6)
        var f1 = 0, r1 = 0
        queue.sync { f1 = frames; r1 = engine.resyncCount }
        let secs = Double(now() - t0) / 1e9, cores = (cpuSeconds() - c0) / secs
        queue.sync { engine.detach(block: block) }
        let r = EngineRow(rateMBps: Double(rate) / 1048576, framesPerS: Double(f1 - f0) / secs, resyncs: r1 - r0, cpuCores: cores,
                          degraded: r1 - r0 > 0 || cores > 0.9)
        out.append(r)
        print(String(format: "engine %3dx%-3d %5.0f MB/s | %6.0f frames/s | resyncs %3d | app cpu %4.2f cores%@", cols, rows,
                     r.rateMBps, r.framesPerS, r.resyncs, r.cpuCores, (r.degraded ? "  DEGRADED" : "") as NSString))
    }
    return out
}


/// resize: the app's mirror reflowing on each resize (a window drag, or the
/// server's size changing), then a frame, with growing history. Limit:
/// resize + frame p99 over 8 ms (half a 60 Hz frame).
struct ResizeRow: Encodable { var historyMB: Int; var resize: Dist; var frame: Dist; var degraded: Bool }
func resizeScenario() -> [ResizeRow] {
    var out: [ResizeRow] = []
    let sizes: [(UInt16, UInt16)] = [(80, 24), (200, 60), (120, 40), (160, 50), (100, 30)]
    for mb in [0, 4, 16] {
        let m = try! Mirror(cols: 120, rows: 40)
        m.apply(theme: .ghostty)
        if mb > 0 { for p in chunks(sgrLines(mb << 20), 64 << 10) { m.feed(p) } }
        var rs: [Double] = [], fs: [Double] = []
        for i in 0..<300 {
            let (c, r) = sizes[i % sizes.count]
            let t0 = now()
            m.resize(cols: c, rows: r, cellWidth: 16, cellHeight: 34)
            let t1 = now()
            _ = m.frame()
            let t2 = now()
            if i >= 20 { rs.append(ms(t1 - t0)); fs.append(ms(t2 - t1)) }
        }
        let row = ResizeRow(historyMB: mb, resize: Dist(rs), frame: Dist(fs), degraded: Dist(rs).p99 + Dist(fs).p99 > 8)
        out.append(row)
        print(String(format: "resize history %2d MB | resize p50 %6.2f p99 %6.2f | frame p50 %6.2f p99 %6.2f ms%@", mb,
                     row.resize.p50, row.resize.p99, row.frame.p50, row.frame.p99, (row.degraded ? "  DEGRADED" : "") as NSString))
    }
    return out
}

// MARK: main

let what = CommandLine.arguments.dropFirst().first ?? "all"
#if DEBUG
print("warning: a debug build; run with -c release")
#endif
let renderer = try Renderer(fonts: FontSet(pointSize: 13, scale: 2))
if what == "feed" || what == "all" {
    struct R: Encodable { var scenario = "app-feed"; var meta = Meta(); var rows: [FeedRow] }
    write("feed", R(rows: feed()))
}
if what == "frame" || what == "all" {
    struct R: Encodable { var scenario = "app-frame"; var meta = Meta(); var limitMs = 8.0; var rows: [FrameRow]; var knee: String }
    let (rows, knee) = frames(renderer: renderer)
    print("knee (frame + encode p99 under 8 ms):", knee)
    write("frame", R(rows: rows, knee: knee))
}
if what == "pipeline" || what == "all" {
    struct R: Encodable { var scenario = "app-pipeline"; var meta = Meta(); var rows: [PipeRow] }
    write("pipeline", R(rows: pipeline()))
}
if what == "engine" || what == "all" {
    struct R: Encodable { var scenario = "app-engine"; var meta = Meta(); var rows: [EngineRow] }
    write("engine", R(rows: engineScenario(cols: 120, rows: 40) + engineScenario(cols: 300, rows: 80)))
}
if what == "resize" || what == "all" {
    struct R: Encodable { var scenario = "app-resize"; var meta = Meta(); var limitMs = 8.0; var rows: [ResizeRow] }
    write("resize", R(rows: resizeScenario()))
}
if what == "memory" || what == "all" {
    struct R: Encodable { var scenario = "app-memory"; var meta = Meta(); var rows: [MemRow] }
    write("memory", R(rows: memory()))
}
