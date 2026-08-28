# 05 — ARI events over WebSocket

**Goal:** connect to ARI's event stream and print every event as it
happens — including the moment a new call arrives, before you have any
channel ID.

## The mechanism

The HTTP calls in stage 3/4 are for *doing* things. To *find out* what's
happening — a new call arriving, a channel hanging up — ARI has a
separate WebSocket endpoint:

```
ws://localhost:8088/ari/events?app=voip-app&api_key=voipapp:devpassword123&subscribeAll=true
```

Notice: same host/port as the HTTP API, `ws://` instead of `http://`,
and credentials go in the query string this time (`api_key=user:pass`),
not a header — WebSocket handshakes don't carry Basic Auth the way
regular requests do.

`app=voip-app` matters: it's the name of the **Stasis application**
(see `../../core/conf/extensions.conf` — the dialplan hands every call to
`Stasis(voip-app)`). You only receive events for calls that land in that
named app. `subscribeAll=true` means "send me everything," not just
channel events.

Once connected, every message you receive is one JSON object per event,
with a `"type"` field telling you what it is: `StasisStart` (a channel
just entered your app — this is where you'd normally answer it),
`StasisEnd`, `ChannelHangupRequest`, `ChannelStateChange`, and many more.

## What you need

- `github.com/gorilla/websocket` for the WebSocket transport — the raw
  framing (masking, opcodes, etc.) genuinely isn't worth hand-rolling.
  From this folder: `go get github.com/gorilla/websocket`.
  `websocket.DefaultDialer.Dial(url, nil)` gets you a `*websocket.Conn`;
  `conn.ReadMessage()` blocks until the next frame arrives.
- A small struct like `struct { Type string }` with a `json:"type"` tag,
  just to peek at which event you got — you don't need to model every
  event's full shape to get started.

## Steps

1. Build the WebSocket URL from `ARI_URL` (swap `http` for `ws`),
   `ARI_APP_NAME`, and `ARI_USERNAME`/`ARI_PASSWORD`.
2. Dial it.
3. Loop calling `ReadMessage`, unmarshal each payload just far enough to
   read `.Type`, print the type and the raw JSON.

## You're done when

With your program running, generate a call:

```bash
docker exec deploy-asterisk-1 asterisk -rx "channel originate Local/1000@stasis-in application Echo"
```

You should see `StasisStart` print, with the channel's ID inside the raw
JSON (`channel.id`) — that's the ID stage 4's `answerChannel` needs, and
now you got it from the event instead of typing it in by hand.

## Next

`06-mini-app` wires this straight into `04`'s `answerChannel`: the moment
`StasisStart` arrives, answer it automatically. That's the whole shape of
the real app in `../../api/cmd/atsap-api/main.go`.
