package xyz.matm.hive.agents

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class AskTest {
    private val claude = """
        ● Update(backtest.py)
          + for window in windows(since):
          +     results.append(run(window))

        ● Bash(python backtest.py --since 2026-09)

         Do you want to proceed?
         ❯ 1. Yes
           2. Yes, and don't ask again this session for commands in
           ~/kalshi-btc15
           3. No, and tell Claude what to do differently (esc)

    """.trimIndent().lines()

    @Test fun liftsClaudesChoices() {
        val ask = Ask.find(claude)!!
        assertEquals("Do you want to proceed?", ask.question)
        assertEquals(listOf("1", "2", "3"), ask.choices.map { it.key })
        assertEquals("Yes", ask.choices[0].label)
        assertEquals("Yes, and don't ask again this session for commands in ~/kalshi-btc15", ask.choices[1].label)
        assertEquals("No, and tell Claude what to do differently", ask.choices[2].label)
        assertEquals(0, ask.selected)
    }

    @Test fun shortLabelsForButtons() {
        val ask = Ask.find(claude)!!
        assertEquals(listOf("Yes", "Yes, this session", "No"), ask.choices.map { it.short })
    }

    @Test fun nothingWithoutAQuestion() {
        assertNull(Ask.find(listOf("All 41 tests pass.", "1. load.ts", "2. format.ts", "> ")))
        assertNull(Ask.find(emptyList()))
    }

    @Test fun anOldDialogAboveThePromptIsNotAsked() {
        val answered = claude + listOf("", "● Ran the backtest.", "", "─────────", "> ", "─────────")
        assertNull(Ask.find(answered))
    }

    @Test fun theMarkerCanSitOnAnotherChoice() {
        val lines = listOf(" Allow this edit?", "   1. Yes", " ❯ 2. No")
        val ask = Ask.find(lines)!!
        assertEquals(1, ask.selected)
        assertEquals(listOf("Yes", "No"), ask.choices.map { it.label })
    }
}
