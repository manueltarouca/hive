package xyz.matm.hive.model

/**
 * The phone's tab order, as the Mac's TabOrder: pinned tabs first. The strip
 * keeps pins, what needs you, and where you are; the tab sheet groups them
 * all by what they're doing. Indices are the session's.
 */
object TabGroups {
    /** state: the agent state that matters most in the tab (blocked, working, done), or null. */
    data class Tab(val index: Int, val pinned: Boolean, val state: String?)

    class Groups(val pinned: List<Int>, val needsYou: List<Int>, val working: List<Int>, val quiet: List<Int>)

    /** Up to this many tabs, the strip shows them all. */
    const val ALL_IN_STRIP = 4

    fun shown(pinned: List<Boolean>): List<Int> = pinned.indices.filter { pinned[it] } + pinned.indices.filter { !pinned[it] }

    fun strip(tabs: List<Tab>, current: Int): List<Int> {
        if (tabs.size <= ALL_IN_STRIP) return shown(tabs.map { it.pinned })
        val out = tabs.filter { it.pinned }.map { it.index } +
            tabs.filter { !it.pinned && it.state == "blocked" }.map { it.index } +
            tabs.filter { !it.pinned && it.state == "done" }.map { it.index }
        return if (current in out || tabs.none { it.index == current }) out else out + current
    }

    fun groups(tabs: List<Tab>): Groups {
        val rest = tabs.filter { !it.pinned }
        return Groups(
            pinned = tabs.filter { it.pinned }.map { it.index },
            needsYou = rest.filter { it.state == "blocked" }.map { it.index } + rest.filter { it.state == "done" }.map { it.index },
            working = rest.filter { it.state == "working" }.map { it.index },
            quiet = rest.filter { it.state !in setOf("blocked", "done", "working") }.map { it.index },
        )
    }
}
