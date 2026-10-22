import Foundation
import HiveKit
import Testing
@testable import HiveCore

struct ScriptsPageTests {
    func json(_ s: String) -> JSON { (try? JSON.parse(Data(s.utf8))) ?? .null }

    @Test func scriptsReadPlainly() {
        let list = ScriptInfo.list(json("""
        {"scripts":[
          {"name":"needs-you","state":"running","permissions":["ui"],"mode":"act","description":"Reminds you.",
           "options":[{"key":"remind_min","title":"Remind me after","type":"number","value":10,"min":1,"max":240},
                      {"key":"include_done","type":"bool","value":false}]},
          {"name":"limit-resume","state":"pending","permissions":[],"asks":["input","screen","ui"],"mode":"act"},
          {"name":"digest","state":"disabled","permissions":["ui"],"mode":"watch"},
          {"name":"broken","state":"failed","error":"SyntaxError: x\\n at 1","permissions":[]}
        ]}
        """))
        #expect(list.map(\.name) == ["broken", "digest", "limit-resume", "needs-you"])
        let pending = list[2], running = list[3], off = list[1], failed = list[0]
        #expect(pending.stateText == "Waiting for your OK" && pending.needsYou)
        #expect(pending.mayDo == ["Type into terminals, agents included", "Read what terminals show", "Notify you, mark panes and bring hive to the front"])
        #expect(pending.shownPermissions == ["input", "screen", "ui"])
        #expect(pending.warning == "It can read your terminals and type into them, agents included.")
        #expect(running.warning == nil && running.on && !running.watching)
        #expect(running.options[0].label == "Remind me after" && running.options[0].max == 240)
        #expect(running.options[1].label == "Include Done" && running.options[1].value == .bool(false))
        #expect(!off.on && off.watching && off.stateText == "Turned off")
        #expect(failed.stateText == "Failed: SyntaxError: x")
    }

    @Test func activitySaysWhatHappened() {
        let e = ActivityEntry.list(json("""
        {"entries":[
          {"t":"2026-10-22T18:00:00Z","principal":"script:limit-resume","script":"limit-resume","method":"agent.act","summary":"typed 7 characters","preview":"resume⏎","result":"ok","seq":5},
          {"t":"2026-10-22T18:01:00Z","principal":"script:limit-resume","script":"limit-resume","method":"agent.act","summary":"answered the agent","result":"watching","seq":6},
          {"t":"2026-10-22T18:02:00Z","principal":"peer:501","script":"limit-resume","method":"script.set_mode","summary":"mode watch","result":"ok","seq":7}
        ]}
        """))
        #expect(e[0].text() == "Typed 7 characters \u{201C}resume⏎\u{201D}" && e[0].problem == nil && e[0].time != nil)
        #expect(e[1].problem == "Refused: watch only")
        #expect(e[2].byPerson && e[2].text() == "You set it to watch only")
        let set = ActivityEntry(json(#"{"principal":"tailscale:me via tailnet (notification button)","method":"script.set_option","summary":"auto = false","result":"ok"}"#))
        let opts = [ScriptOption(json(#"{"key":"auto","title":"Type it by itself","type":"bool","value":false}"#))]
        #expect(set.text(options: opts) == "You set \u{201C}Type it by itself\u{201D} to off from a notification")
    }

    @Test func chipsOnlyForPanes() {
        #expect(ActedChip(json(#"{"script":"team","summary":"pressed escape"}"#)) == nil)
        let c = ActedChip(json(#"{"script":"team","summary":"pressed escape","block_id":"b1","result":"ok"}"#))
        #expect(c?.text == "team pressed escape" && c?.refused == false)
        let r = ActedChip(json(#"{"script":"team","summary":"pressed escape","block_id":"b1","result":"watching"}"#))
        #expect(r?.text == "team was stopped from acting" && r?.refused == true)
        #expect(ActedChip(json(#"{"script":"team","summary":"mode watch","block_id":"b1","by":"peer:501"}"#)) == nil)
    }
}
