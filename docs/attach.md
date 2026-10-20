---
title: Attach protocol
section: Automate
order: 22
description: How a client mirrors terminals: snapshots, ordered output, resizes, and resyncs.
---

# Attach protocol

Clients that draw terminals, like the app, connect to `GET /v1/attach` with a
WebSocket, one per client. The protocol keeps a client's copy of each
terminal identical to the server's.

## Mirroring a terminal

1. Send `{"type":"hello","name":"…","kind":"app","platform":"darwin","version":"…"}`.
   The server answers `welcome` with your client ID. `client.list` shows
   the name and platform, so others know who holds a pane's size.
2. Send `{"type":"subscribe","block_id":"…"}`. The server answers
   `subscribed` with a `seq`, then a snapshot frame.
3. Restore a libghostty terminal from the snapshot. Apply every frame that
   follows, in order.

`seq` counts the bytes the program has printed. Each frame says where it
starts, so a client can check that nothing is missing. A resize arrives as a
frame too, at the exact point in the output where it happened, because text
reflows differently depending on where the resize lands.

If a client falls behind, the server sends `resync` for that block instead of
dropping bytes. Subscribe again for a fresh snapshot.

To show a whole session, start the event stream first (`events`), then read
`session.view`, then subscribe to the panes on screen. Events that arrive in
between are then not missed. On `events_resync`, read the view again.

The same path works on a tailnet listener: a client on another machine
attaches to `wss://box.example.ts.net/v1/attach`.

## Binary frames

```
byte 0     kind: 1 output, 2 snapshot, 3 resize
bytes 1-2  length of the block ID
…          block ID
8 bytes    seq
…          data
```

A resize carries four 16-bit numbers: columns, rows, and the cell's width and
height in pixels.

## Messages

| Client sends | Does |
| --- | --- |
| `subscribe`, `unsubscribe` | Start or stop mirroring a block |
| `input` | Type `data`, or `data_base64` |
| `key` | Press a named key |
| `resize` | Ask for a size. `take: true` claims it from another client; `release: true` sets it and gives the claim up, so the client in use takes the pane up again. Without `take`, only the holder may release, or anyone while nobody holds it. |
| `call` | Call any API method, with an `id` |
| `events` | Stream events on this connection, with `since` |

| Server sends | Means |
| --- | --- |
| `welcome` | Hello accepted: your `client_id` and the server's `version` |
| `subscribed` | Mirroring a block, from `seq`; a snapshot frame follows |
| `resync` | You fell behind on a block: subscribe again |
| `exited` | The block's program exited; no more output |
| `result` | The answer to a `call`, with its `id`: `result`, or `error` |
| `events_subscribed` | The event stream started, at `seq` |
| `event` | One event |
| `events_resync` | Events were lost: read the view again |
| `error` | A message couldn't be done, and why |

Your copy of the terminal must not answer the program. The server's terminal
already did.
