import Foundation
import Network

/// A WebSocket connection to hive's attach endpoint over its unix socket.
/// Everything happens on `queue`, which the caller owns; callbacks run there.
final class WebSocketConnection {
    let queue: DispatchQueue
    private let conn: NWConnection
    private let path: String
    private var key = WebSocketHandshake.newKey()
    private var upgraded = false
    private var pending = Data()
    private var parser = WebSocketParser()
    private var closed = false

    var onOpen: (() -> Void)?
    var onMessage: ((WebSocketMessage) -> Void)?
    var onClose: ((Error?) -> Void)?

    init(socketPath: String, path: String = "/v1/attach", queue: DispatchQueue) {
        self.queue = queue
        self.path = path
        conn = NWConnection(to: .unix(path: socketPath), using: .tcp)
    }

    func start() {
        conn.stateUpdateHandler = { [weak self] state in
            guard let self else { return }
            switch state {
            case .ready:
                self.conn.send(content: WebSocketHandshake.request(path: self.path, key: self.key), completion: .contentProcessed { _ in })
                self.receive()
            case .failed(let e), .waiting(let e):
                self.finish(e)
            case .cancelled:
                self.finish(nil)
            default: break
            }
        }
        conn.start(queue: queue)
    }

    private func receive() {
        conn.receive(minimumIncompleteLength: 1, maximumLength: 1 << 20) { [weak self] data, _, isComplete, error in
            guard let self, !self.closed else { return }
            if let data, !data.isEmpty { self.ingest(data) }
            if let error { self.finish(error); return }
            if isComplete { self.finish(nil); return }
            self.receive()
        }
    }

    private func ingest(_ data: Data) {
        do {
            var bytes = data
            if !upgraded {
                pending.append(data)
                let (done, rest) = try WebSocketHandshake.parseResponse(pending, key: key)
                guard done else { return }
                upgraded = true
                pending = Data()
                onOpen?()
                bytes = rest
            }
            for msg in try parser.feed(bytes) {
                switch msg {
                case .ping(let p): write(opcode: 0xA, p)
                case .close(let code):
                    write(opcode: 0x8, code.map { Data([UInt8($0 >> 8), UInt8($0 & 0xff)]) } ?? Data())
                    finish(nil)
                    return
                default: onMessage?(msg)
                }
            }
        } catch {
            finish(error)
        }
    }

    private func write(opcode: UInt8, _ payload: Data) {
        guard !closed else { return }
        conn.send(content: WebSocketParser.encode(opcode: opcode, payload: payload), completion: .contentProcessed { _ in })
    }

    func send(text: Data) { write(opcode: 0x1, text) }
    func send(binary: Data) { write(opcode: 0x2, binary) }

    func close() {
        guard !closed else { return }
        write(opcode: 0x8, Data([0x03, 0xe8]))
        finish(nil)
    }

    private func finish(_ error: Error?) {
        guard !closed else { return }
        closed = true
        conn.cancel()
        onClose?(error)
    }
}
