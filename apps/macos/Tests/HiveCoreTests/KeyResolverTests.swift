import Testing
import HiveKit
@testable import HiveCore

/// Keys from the server's keymap: chords, sequences and modes, resolved in
/// the app so typing never waits on the network.
@Suite struct KeyResolverTests {
    func b(_ key: String, _ action: String, mode: String = "", args: JSON = .null) -> JSON {
        .object(["key": .string(key), "mode": .string(mode), "action": .string(action), "args": args, "source": "test"])
    }
    func c(_ s: String) -> KeyChord { KeyChord.parse(s)![0] }

    @Test func parsesTheServersSpelling() {
        #expect(KeyChord.parse("shift+cmd+d") == [KeyChord(shift: true, cmd: true, key: "d")])
        #expect(KeyChord.parse("ctrl+b>c")?.count == 2)
        #expect(KeyChord.parse("alt+cmd+left")?.first?.key == "left")
        #expect(KeyChord.parse("cmd+") == nil)
    }

    @Test func chordsSequencesAndModes() {
        let km = JSON.object(["bindings": .array([
            b("cmd+d", "client.split_right"),
            b("ctrl+b>c", "client.new_tab"),
            b("ctrl+cmd+r", "client.mode", args: .object(["mode": "resize"])),
            b("left", "client.resize", mode: "resize", args: .object(["direction": "left"])),
        ]), "modes": .array([.object(["name": "resize", "exclusive": true])])])
        var r = KeyResolver(km)
        #expect(r.press(c("cmd+d")) == .action(action: "client.split_right", args: .null))
        #expect(r.press(c("x")) == .pass)
        // A sequence: its first key waits, the second acts; anything else
        // drops it and goes through.
        #expect(r.press(c("ctrl+b")) == .pending)
        #expect(r.press(c("c")) == .action(action: "client.new_tab", args: .null))
        #expect(r.press(c("ctrl+b")) == .pending)
        #expect(r.press(c("z")) == .pass)
        // A mode: its keys act, an exclusive one swallows the rest, Escape leaves.
        #expect(r.press(c("ctrl+cmd+r")) == .enteredMode("resize"))
        #expect(r.mode == "resize")
        #expect(r.press(c("left")) == .action(action: "client.resize", args: .object(["direction": "left"])))
        #expect(r.press(c("q")) == .swallowed)
        #expect(r.press(c("escape")) == .leftMode)
        #expect(r.mode == nil)
        #expect(r.press(c("left")) == .pass)
    }

    @Test func menuKeysForActions() {
        let km = JSON.object(["bindings": .array([b("shift+cmd+d", "client.split_down"), b("ctrl+b>c", "client.new_tab"),
                                                  b("cmd+1", "client.tab", args: .object(["index": 0]))])])
        let r = KeyResolver(km)
        #expect(r.chord(for: "client.split_down") == KeyChord(shift: true, cmd: true, key: "d"))
        #expect(r.chord(for: "client.new_tab") == nil)  // a sequence can't be a menu's key
        #expect(r.chord(for: "client.tab", args: .object(["index": 0]))?.key == "1")
        #expect(r.chord(for: "client.tab", args: .object(["index": 1])) == nil)
    }
}
