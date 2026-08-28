# 04 — ARI actions

**Goal:** write functions that answer and hang up a *real* channel, by
sending POST/DELETE requests to ARI, and drive them from `os.Args`.

## The actions

| Action | HTTP | Path |
|---|---|---|
| Answer a channel | `POST` | `/channels/{id}/answer` |
| Hang up a channel | `DELETE` | `/channels/{id}?reason=...` (reason is optional) |
| Play media | `POST` | `/channels/{id}/play?media=sound:hello-world` |

All three take no request body — everything is in the path or query
string. A successful call returns 2xx (often with an empty body, or a
small JSON object for `play`, which returns a playback ID). A failure
returns non-2xx with `{"message": "..."}`, e.g. 404 if the channel ID
doesn't exist (channels disappear the moment they hang up).

## What you need

- `net/http`, `net/url` (for building the query string on hangup/play).
- A function per action, each returning `error`:
  `func answerChannel(channelID string) error`,
  `func hangupChannel(channelID, reason string) error`.
- `os.Args` to take the channel ID (and action) from the command line,
  so you can run e.g. `go run . answer 178790...`.

## Getting a real channel ID to test against

```bash
docker exec deploy-asterisk-1 asterisk -rx "channel originate Local/1000@stasis-in application Echo"
docker exec deploy-asterisk-1 asterisk -rx "core show channels concise"
```

The concise output is `!`-separated; the channel ID is the last field.
There will be two channels (`;1` and `;2`) — the one with `Stasis` as its
application is the one your ARI calls need to target.

## Steps

1. Write `answerChannel` and `hangupChannel` (and `play` if you want the
   extra practice with decoding its response).
2. In `main`, read the action name and channel ID from `os.Args`, call
   the matching function, print the result or error.

## You're done when

You can run `go run . answer <channel-id>` against a real channel and
then confirm it worked — either by checking `core show channels concise`
(state changes to `Up`), or better: have `02-ami-events` running in
another terminal and watch for the state-change event your action
triggers.

## Next

`05-ari-events-ws` replaces "grab a channel ID by hand from the CLI" with
the real mechanism: ARI tells you about new channels itself, over a
WebSocket.
