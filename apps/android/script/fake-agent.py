# A pane that looks like Claude Code asking for permission, for the e2e:
# it prints the dialog, then echoes every key it gets ("got b'1'") and asks
# again, so the test can read what the phone sent. Not a shell, so hive
# keeps the agent.
import os
import sys
import tty


def ask():
    sys.stdout.write("● Bash(python backtest.py --since 2026-09)\r\n\r\n")
    sys.stdout.write(" Do you want to proceed?\r\n")
    sys.stdout.write(" ❯ 1. Yes\r\n")
    sys.stdout.write("   2. Yes, and don't ask again this session\r\n")
    sys.stdout.write("   3. No, and tell Claude what to do differently (esc)\r\n")
    sys.stdout.flush()


sys.stdout.write("\x1b]0;Backtest the BTC 15m model\x07")
ask()
tty.setraw(0)
while True:
    b = os.read(0, 256)
    if not b:
        break
    sys.stdout.write("\r\ngot " + repr(b) + "\r\n\r\n")
    ask()
