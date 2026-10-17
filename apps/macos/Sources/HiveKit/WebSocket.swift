import CryptoKit
import Foundation

/// A complete WebSocket message, after reassembling fragments.
public enum WebSocketMessage: Equatable, Sendable {
    case text(Data)
    case binary(Data)
    case ping(Data)
    case pong(Data)
    case close(UInt16?)
}

public enum WebSocketError: Error, Equatable {
    case handshake(String)
    case protocolViolation(String)
}

/// Parses server-to-client frames (RFC 6455): fragments, control frames
/// interleaved with them, and 7/16/64-bit lengths, from bytes that may arrive
/// split anywhere.
struct WebSocketParser {
    private var buffer = Data()
    private var fragments = Data()
    private var fragmentOpcode: UInt8?
    /// Largest message accepted. Snapshots of big scrollbacks run to tens of MB.
    var maxMessage = 256 << 20

    mutating func feed(_ data: Data) throws -> [WebSocketMessage] {
        buffer.append(data)
        var out: [WebSocketMessage] = []
        while let msg = try next() {
            out.append(msg)
        }
        return out
    }

    private mutating func next() throws -> WebSocketMessage? {
        while true {
            let b = [UInt8](buffer.prefix(14))
            guard b.count >= 2 else { return nil }
            let fin = b[0] & 0x80 != 0
            let opcode = b[0] & 0x0f
            let masked = b[1] & 0x80 != 0
            var len = UInt64(b[1] & 0x7f)
            var header = 2
            if len == 126 {
                guard b.count >= 4 else { return nil }
                len = UInt64(b[2]) << 8 | UInt64(b[3])
                header = 4
            } else if len == 127 {
                guard b.count >= 10 else { return nil }
                len = b[2..<10].reduce(0) { $0 << 8 | UInt64($1) }
                header = 10
            }
            if masked { header += 4 }
            guard len <= UInt64(maxMessage) else { throw WebSocketError.protocolViolation("frame of \(len) bytes") }
            guard buffer.count >= header + Int(len) else { return nil }
            let start = buffer.startIndex
            var payload = Data(buffer[(start + header)..<(start + header + Int(len))])
            if masked {
                let mask = [UInt8](buffer[(start + header - 4)..<(start + header)])
                payload = Data(payload.enumerated().map { $0.element ^ mask[$0.offset % 4] })
            }
            buffer.removeFirst(header + Int(len))
            buffer = Data(buffer) // reset indices so prefix() starts at 0

            switch opcode {
            case 0x8:
                let code = payload.count >= 2 ? UInt16(payload[payload.startIndex]) << 8 | UInt16(payload[payload.startIndex + 1]) : nil
                return .close(code)
            case 0x9: return .ping(payload)
            case 0xA: return .pong(payload)
            case 0x1, 0x2:
                guard fragmentOpcode == nil else { throw WebSocketError.protocolViolation("new message inside a fragmented one") }
                if fin { return opcode == 1 ? .text(payload) : .binary(payload) }
                fragmentOpcode = opcode
                fragments = payload
            case 0x0:
                guard let op = fragmentOpcode else { throw WebSocketError.protocolViolation("continuation without a start") }
                fragments.append(payload)
                guard fragments.count <= maxMessage else { throw WebSocketError.protocolViolation("message too large") }
                if fin {
                    let whole = fragments
                    fragments = Data()
                    fragmentOpcode = nil
                    return op == 1 ? .text(whole) : .binary(whole)
                }
            default:
                throw WebSocketError.protocolViolation("opcode \(opcode)")
            }
        }
    }

    /// Encodes a client frame. Client frames are always masked.
    static func encode(opcode: UInt8, payload: Data) -> Data {
        var f = Data([0x80 | opcode])
        let n = payload.count
        if n < 126 {
            f.append(0x80 | UInt8(n))
        } else if n < 65536 {
            f.append(0x80 | 126)
            f.append(UInt8(n >> 8)); f.append(UInt8(n & 0xff))
        } else {
            f.append(0x80 | 127)
            for i in (0..<8).reversed() { f.append(UInt8((UInt64(n) >> (UInt64(i) * 8)) & 0xff)) }
        }
        var mask = [UInt8](repeating: 0, count: 4)
        for i in 0..<4 { mask[i] = UInt8.random(in: 0...255) }
        f.append(contentsOf: mask)
        var body = [UInt8](payload)
        for i in body.indices { body[i] ^= mask[i % 4] }
        f.append(contentsOf: body)
        return f
    }
}

enum WebSocketHandshake {
    static let guid = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

    static func newKey() -> String {
        Data((0..<16).map { _ in UInt8.random(in: 0...255) }).base64EncodedString()
    }

    static func accept(for key: String) -> String {
        Data(Insecure.SHA1.hash(data: Data((key + guid).utf8))).base64EncodedString()
    }

    static func request(path: String, key: String) -> Data {
        // No Sec-WebSocket-Extensions: the parser doesn't do compression.
        Data(("GET \(path) HTTP/1.1\r\nHost: hive\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
            + "Sec-WebSocket-Key: \(key)\r\nSec-WebSocket-Version: 13\r\n\r\n").utf8)
    }

    /// Parses the server's answer once the header is complete. Returns
    /// (false, data) while more is needed, and (true, the bytes after the
    /// header) when done: those already belong to the frame stream.
    static func parseResponse(_ data: Data, key: String) throws -> (Bool, Data) {
        guard let end = data.range(of: Data("\r\n\r\n".utf8)) else {
            if data.count > 16 << 10 { throw WebSocketError.handshake("header too long") }
            return (false, data)
        }
        let head = String(decoding: data[data.startIndex..<end.lowerBound], as: UTF8.self)
        let lines = head.components(separatedBy: "\r\n")
        guard let status = lines.first, status.hasPrefix("HTTP/1.1 101") else {
            throw WebSocketError.handshake(lines.first ?? "no status")
        }
        let want = accept(for: key)
        let ok = lines.dropFirst().contains { line in
            let parts = line.split(separator: ":", maxSplits: 1).map { $0.trimmingCharacters(in: .whitespaces) }
            return parts.count == 2 && parts[0].lowercased() == "sec-websocket-accept" && parts[1] == want
        }
        guard ok else { throw WebSocketError.handshake("bad Sec-WebSocket-Accept") }
        return (true, Data(data[end.upperBound...]))
    }
}
