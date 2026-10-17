import Foundation

/// An error the hive server returned.
public struct HiveError: Error, Equatable, CustomStringConvertible {
    public let status: Int
    public let code: String
    public let message: String
    public var description: String { "\(message) (\(code))" }

    init(_ j: JSON) {
        status = j["status"].int ?? 0
        code = j["code"].string ?? "unknown"
        message = j["message"].string ?? "\(j)"
    }
    public init(code: String, message: String) { status = 0; self.code = code; self.message = message }
}

/// What a block subscription delivers, in stream order.
public enum BlockUpdate: Equatable, Sendable {
    /// A full terminal state at `seq`; everything after continues from it.
    case snapshot(seq: UInt64, data: Data)
    case output(seq: UInt64, data: Data)
    /// The terminal was resized at `seq`, before any later output.
    case resize(seq: UInt64, cols: UInt16, rows: UInt16, cellWidth: UInt16, cellHeight: UInt16)
    /// The subscriber fell behind; subscribe again for a new snapshot.
    case resync
    /// The program exited; no more output will come.
    case exited
}

/// The hive attach protocol, one connection per client. All methods must be
/// called on `queue`, and every callback runs on it. The Mac app uses one
/// serial queue for this client and every libghostty mirror, so frames go
/// straight from the socket into the terminals without a hop.
public final class HiveClient {
    public let queue: DispatchQueue
    public private(set) var clientID: String?
    private let ws: WebSocketConnection
    private var nextID: Int64 = 1
    private var calls: [Int64: (Result<JSON, HiveError>) -> Void] = [:]
    private var blockHandlers: [String: (BlockUpdate) -> Void] = [:]
    private var awaitingSnapshot: Set<String> = []

    public var onEvent: ((JSON) -> Void)?
    /// Events stream (re)started at seq, or must be re-read after a gap.
    public var onEventsSubscribed: ((UInt64) -> Void)?
    public var onEventsResync: (() -> Void)?
    /// An error not tied to a call: (request type, block ID, error).
    public var onError: ((String?, String?, HiveError) -> Void)?
    public var onClose: ((Error?) -> Void)?

    public init(socketPath: String, queue: DispatchQueue) {
        self.queue = queue
        ws = WebSocketConnection(socketPath: socketPath, queue: queue)
    }

    /// Connects and says hello. `ready` runs once the server has welcomed us.
    public func connect(name: String, version: String = "0.1", ready: @escaping (Result<String, Error>) -> Void) {
        var greeted = false
        ws.onOpen = { [weak self] in
            self?.sendJSON(["type": "hello", "name": .string(name), "kind": "app", "platform": "darwin", "version": .string(version)])
        }
        ws.onMessage = { [weak self] msg in
            guard let self else { return }
            switch msg {
            case .text(let d):
                guard let j = try? JSON.parse(d) else { return }
                if !greeted, j["type"].string == "welcome" {
                    greeted = true
                    self.clientID = j["client_id"].string
                    ready(.success(self.clientID ?? ""))
                    return
                }
                self.handleText(j)
            case .binary(let d): self.handleBinary(d)
            default: break
            }
        }
        ws.onClose = { [weak self] err in
            guard let self else { return }
            if !greeted { ready(.failure(err ?? HiveError(code: "closed", message: "connection closed before welcome"))) }
            for (_, cb) in self.calls { cb(.failure(HiveError(code: "closed", message: "connection closed"))) }
            self.calls.removeAll()
            self.onClose?(err)
        }
        ws.start()
    }

    public func close() { ws.close() }

    private func sendJSON(_ j: JSON) { ws.send(text: j.data()) }

    // MARK: calls

    /// Calls a server method.
    public func call(_ method: String, _ params: JSON = [:], completion: @escaping (Result<JSON, HiveError>) -> Void) {
        let id = nextID
        nextID += 1
        calls[id] = completion
        sendJSON(["type": "call", "id": .number(Double(id)), "method": .string(method), "params": params])
    }

    // MARK: events

    /// Starts the event stream. `since` nil: live only.
    public func subscribeEvents(sessionID: String? = nil, all: Bool = false, since: UInt64? = nil) {
        var m: [String: JSON] = ["type": "events", "all": .bool(all)]
        if let sessionID { m["session_id"] = .string(sessionID) }
        if let since { m["since"] = .number(Double(since)) }
        sendJSON(.object(m))
    }

    // MARK: blocks

    /// Subscribes to a block. `handler` gets a snapshot, then output and
    /// resizes in order, until resync or exit.
    public func subscribe(block: String, handler: @escaping (BlockUpdate) -> Void) {
        blockHandlers[block] = handler
        awaitingSnapshot.insert(block)
        sendJSON(["type": "subscribe", "block_id": .string(block)])
    }

    public func unsubscribe(block: String) {
        blockHandlers[block] = nil
        sendJSON(["type": "unsubscribe", "block_id": .string(block)])
    }

    public func input(block: String, _ bytes: Data) {
        sendJSON(["type": "input", "block_id": .string(block), "data_base64": .string(bytes.base64EncodedString())])
    }

    public func resize(block: String, cols: UInt16, rows: UInt16, cellWidth: UInt16, cellHeight: UInt16, take: Bool) {
        sendJSON(["type": "resize", "block_id": .string(block), "cols": .number(Double(cols)), "rows": .number(Double(rows)),
                  "cell_width_px": .number(Double(cellWidth)), "cell_height_px": .number(Double(cellHeight)), "take": .bool(take)])
    }

    // MARK: incoming

    private func handleText(_ j: JSON) {
        switch j["type"].string {
        case "result":
            guard let id = j["id"].int.map(Int64.init), let cb = calls.removeValue(forKey: id) else { return }
            if !j["error"].isNull { cb(.failure(HiveError(j["error"]))) } else { cb(.success(j["result"])) }
        case "event":
            onEvent?(j["event"])
        case "events_subscribed":
            onEventsSubscribed?(UInt64(j["seq"].double ?? 0))
        case "events_resync":
            onEventsResync?()
        case "resync":
            if let b = j["block_id"].string, let h = blockHandlers.removeValue(forKey: b) { h(.resync) }
        case "exited":
            if let b = j["block_id"].string { blockHandlers[b]?(.exited) }
        case "error":
            let block = j["block_id"].string
            if j["for"].string == "subscribe", let block { blockHandlers[block] = nil }
            onError?(j["for"].string, block, HiveError(j["error"]))
        default:
            break // subscribed: the snapshot frame that follows carries the seq
        }
    }

    private func handleBinary(_ d: Data) {
        guard let f = Frame(d), let handler = blockHandlers[f.blockID] else { return }
        switch f.kind {
        case 2:
            awaitingSnapshot.remove(f.blockID)
            handler(.snapshot(seq: f.seq, data: f.payload))
        case 1 where !awaitingSnapshot.contains(f.blockID):
            handler(.output(seq: f.seq, data: f.payload))
        case 3 where !awaitingSnapshot.contains(f.blockID) && f.payload.count == 8:
            let p = [UInt8](f.payload)
            func u16(_ i: Int) -> UInt16 { UInt16(p[i]) << 8 | UInt16(p[i + 1]) }
            handler(.resize(seq: f.seq, cols: u16(0), rows: u16(2), cellWidth: u16(4), cellHeight: u16(6)))
        default: break
        }
    }
}

/// A binary attach frame: [kind][u16 id length][id][u64 seq][payload].
struct Frame {
    let kind: UInt8
    let blockID: String
    let seq: UInt64
    let payload: Data

    init?(_ d: Data) {
        let b = [UInt8](d.prefix(3))
        guard b.count == 3 else { return nil }
        let n = Int(UInt16(b[1]) << 8 | UInt16(b[2]))
        guard d.count >= 3 + n + 8 else { return nil }
        let s = d.startIndex
        kind = b[0]
        blockID = String(decoding: d[(s + 3)..<(s + 3 + n)], as: UTF8.self)
        seq = d[(s + 3 + n)..<(s + 11 + n)].reduce(0) { $0 << 8 | UInt64($1) }
        payload = Data(d[(s + 11 + n)...])
    }
}
