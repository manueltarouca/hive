---
title: Privacy
section: Reference
order: 10
description: hive collects nothing and connects nowhere.
---

# Privacy

hive collects no telemetry. It has no analytics, no crash reporting and no
account.

The server listens only on a Unix socket on your machine, readable only by
you. It makes no outbound connections. The app connects only to that socket.

The programs you run in hive make their own connections. hive doesn't see or
control them.
