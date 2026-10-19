package xyz.matm.hive.ui

/**
 * An input method's text for a terminal: committed text goes at once; a
 * composition (CJK, swipe, voice) stays here until committed or finished,
 * since a terminal can't take back what it was sent. Returns what to send.
 */
class Composer {
    private var composing = ""

    /** Committed text replaces any composition. */
    fun commit(t: String): String { composing = ""; return t }

    fun compose(t: String): String { composing = t; return "" }

    /** The composition, as it stands, goes. */
    fun finish(): String = composing.also { composing = "" }

    /** Deleting n characters: inside a composition only it shrinks; returns how many backspaces the pane needs. */
    fun delete(n: Int): Int {
        if (composing.isEmpty()) return n
        composing = composing.dropLast(n)
        return 0
    }
}
