import Foundation
import Testing
import HiveKit
@testable import HiveCore

/// Badges scripts put on panes, and the notifications an app replays when
/// it connects.
@Suite struct ScriptUITests {
    func badge(_ block: String, _ app: String, _ id: String, _ state: String, msg: String = "") -> JSON {
        .object(["block_id": .string(block), "app": .string(app), "id": .string(id), "state": .string(state), "msg": .string(msg)])
    }

    @Test func badgesFromAListThenEvents() {
        var book = BadgeBook()
        book.load(.object(["badges": .array([badge("b1", "script:t", "tests", "error", msg: "3 failing"),
                                             badge("b1", "cli", "ci", "working")])]))
        #expect(book.badges(for: "b1").map(\.id) == ["tests", "ci"]) // failing first
        // An update replaces; a clear removes; another pane is apart.
        book.apply(badge("b1", "script:t", "tests", "done", msg: "pass"))
        book.apply(badge("b2", "script:t", "port", "done", msg: "port 8080"))
        book.apply(badge("b1", "cli", "ci", "clear"))
        #expect(book.badges(for: "b1").map(\.msg) == ["pass"])
        #expect(book.badges(for: "b2").first?.state == "done")
        // A badge that needs you comes first.
        book.apply(badge("b2", "script:x", "a", "error", msg: "boom"))
        #expect(book.badges(for: "b2").map(\.id) == ["a", "port"])
    }

    func note(_ id: String, _ t: String, acked: Bool = false) -> JSON {
        .object(["notification_id": .string(id), "time": .string(t), "title": .string("T " + id), "acked": .bool(acked)])
    }

    @Test func replayShowsWhatWasMissedAndSummarizesTheRest() {
        let list = JSON.object(["notifications": .array((1...8).map { note("n\($0)", "2026-10-21T10:0\($0):00Z") } + [note("old", "2026-10-21T09:00:00Z")])])
        let seen = ISO8601DateFormatter().date(from: "2026-10-21T10:00:30Z")
        let r = NoteReplay(list, seenUntil: seen, alreadyShown: ["n1"], max: 3)
        // n1 was shown already, old is before what was seen: n2...n8 are new,
        // the latest 3 show and the rest are counted.
        #expect(r.show.map(\.id) == ["n6", "n7", "n8"])
        #expect(r.more == 4)
        #expect(r.latest == ISO8601DateFormatter().date(from: "2026-10-21T10:08:00Z"))
    }

    @Test func noteActionsKeepTheirArguments() {
        let n = ScriptNote(.object(["notification_id": .string("n"), "title": .string("Approve?"), "body": .string("Read foo"),
            "block_id": .string("b"), "actions": .array([.object(["title": .string("Approve"), "action": .string("rule:approve"),
            "args": .object(["block_id": .string("b"), "revision": .number(7)]), "step": .string("Press enter in the agent's pane")])])]))
        #expect(n.actions.count == 1)
        #expect(n.actions[0].action == "rule:approve")
        #expect(n.actions[0].args["revision"].int == 7)
        #expect(n.bodyWithSteps == "Read foo\nApprove: Press enter in the agent's pane")
    }
}
