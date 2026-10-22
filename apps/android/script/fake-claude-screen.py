#!/usr/bin/env python3
"""The bottom of a Claude Code screen, as it really ends: work, then its
input box between rules, then its status bar. The tab sheet must preview
the work, not the chrome. Usage: fake-claude-screen.py bypass|manual|picker"""
import sys, time

RULE = "─" * 60
kind = sys.argv[1] if len(sys.argv) > 1 else "bypass"
if kind == "picker":
    lines = ["● Which database should the cache use?", "", " ❯ 1. Postgres", "   2. SQLite", "",
             "Enter to select · ↑/↓ to navigate · Esc to cancel"]
else:
    work = {"bypass": ["● Updated docs/scripts.md with trigger examples", "  ⎿  Wrote 42 lines"],
            "manual": ["● Looking at the screenshot you sent", "  ⎿  Read image (1.2 MB)"]}[kind]
    foot = {"bypass": "  ⏵⏵ bypass permissions on (shift+tab to cycle)",
            "manual": "  ⏸ manual mode on · ? for shortcuts"}[kind]
    lines = work + ["", RULE, "> ", RULE, foot]
sys.stdout.write("\r\n".join(lines))
sys.stdout.flush()
while True:
    time.sleep(3600)
