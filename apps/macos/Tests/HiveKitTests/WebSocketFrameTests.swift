import Foundation
import Testing
@testable import HiveKit

/// Builds an unmasked server frame.
func serverFrame(opcode: UInt8, payload: [UInt8], fin: Bool = true) -> [UInt8] {
    var f: [UInt8] = [(fin ? 0x80 : 0) | opcode]
    switch payload.count {
    case ..<126: f.append(UInt8(payload.count))
    case ..<65536: f.append(126); f += [UInt8(payload.count >> 8), UInt8(payload.count & 0xff)]
    default:
        f.append(127)
        for i in (0..<8).reversed() { f.append(UInt8((UInt64(payload.count) >> (UInt64(i) * 8)) & 0xff)) }
    }
    return f + payload
}

@Suite struct WebSocketFrameTests {
    @Test func textAndBinary() throws {
        var p = WebSocketParser()
        let msgs = try p.feed(Data(serverFrame(opcode: 1, payload: Array("hi".utf8)) + serverFrame(opcode: 2, payload: [1, 2, 3])))
        #expect(msgs == [.text(Data("hi".utf8)), .binary(Data([1, 2, 3]))])
    }

    @Test func splitAcrossReadsAndLargeLengths() throws {
        var p = WebSocketParser()
        let big = [UInt8](repeating: 7, count: 70_000) // needs a 64-bit length
        let wire = serverFrame(opcode: 2, payload: big) + serverFrame(opcode: 1, payload: Array("after".utf8))
        var got: [WebSocketMessage] = []
        var i = 0
        while i < wire.count { // feed in odd-sized chunks
            let n = min(997, wire.count - i)
            got += try p.feed(Data(wire[i..<i + n]))
            i += n
        }
        #expect(got.count == 2)
        #expect(got.first == .binary(Data(big)))
        #expect(got.last == .text(Data("after".utf8)))
    }

    @Test func fragmentsWithInterleavedPing() throws {
        var p = WebSocketParser()
        let wire = serverFrame(opcode: 1, payload: Array("hel".utf8), fin: false)
            + serverFrame(opcode: 9, payload: Array("p".utf8))
            + serverFrame(opcode: 0, payload: Array("lo".utf8), fin: true)
        let msgs = try p.feed(Data(wire))
        #expect(msgs == [.ping(Data("p".utf8)), .text(Data("hello".utf8))])
    }

    @Test func closeFrame() throws {
        var p = WebSocketParser()
        let msgs = try p.feed(Data(serverFrame(opcode: 8, payload: [0x03, 0xe8])))
        #expect(msgs == [.close(1000)])
    }

    @Test func clientFramesAreMasked() {
        let f = WebSocketParser.encode(opcode: 1, payload: Data("abc".utf8))
        #expect(f[0] == 0x81)
        #expect(f[1] & 0x80 == 0x80) // mask bit
        let mask = Array(f[2..<6])
        let body = f[6...].enumerated().map { $0.element ^ mask[$0.offset % 4] }
        #expect(body == Array("abc".utf8))
    }

    @Test func handshakeAcceptAndTrailingBytes() throws {
        let key = "dGhlIHNhbXBsZSBub25jZQ=="
        #expect(WebSocketHandshake.accept(for: key) == "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=") // RFC 6455 example
        let response = "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n\r\n"
        let wire = Data(response.utf8) + Data(serverFrame(opcode: 1, payload: Array("x".utf8)))
        let (done, rest) = try WebSocketHandshake.parseResponse(wire, key: key)
        #expect(done)
        #expect(rest == Data(serverFrame(opcode: 1, payload: Array("x".utf8))))
    }
}

@Suite struct ParserLimitTests {
    @Test func oversizedFramesAreRejected() {
        var p = WebSocketParser()
        p.maxMessage = 1024
        #expect(throws: WebSocketError.self) { _ = try p.feed(Data(serverFrame(opcode: 2, payload: [UInt8](repeating: 0, count: 2048)))) }
    }

    @Test func jsonIntDoesNotTrap() throws {
        #expect(JSON.number(.infinity).int == nil)
        #expect(JSON.number(1e300).int == nil)
        #expect(JSON.number(42).int == 42)
    }
}
