# 06 — Your own mini VoIP app

**Goal:** combine everything: run AMI events (02) and ARI events (05) at
the same time, and the moment a `StasisStart` arrives, automatically
answer it (04) and play a sound. Shut down cleanly on Ctrl+C.

This is the same shape as the real app in
`../../api/cmd/atsap-api/main.go` — after this stage, go read that file.
It should look familiar.

## What's new here: doing two things at once

AMI events and ARI events arrive on two independent connections, on their
own schedule. You need to be reading both *concurrently*, not one after
the other (reading AMI in a loop would never let you check ARI). That's
what goroutines are for: run each event loop in its own `go func() { ... }()`,
and let `main` block until told to stop.

"Told to stop" is `context.Context` + `os/signal.NotifyContext`: it gives
you a `ctx` that's cancelled the moment the user hits Ctrl+C, and a
`ctx.Done()` channel `main` can block on. Your event-reading loops should
check `ctx.Err()` (or select on `ctx.Done()`) so they stop too, instead of
leaking forever.

## What you need

- Everything from 02, 04, and 05 — copy your working code in.
- `context.Background()`, `signal.NotifyContext(ctx, syscall.SIGINT,
  syscall.SIGTERM)`.
- Two goroutines: one running your AMI read loop, one running your ARI
  event loop.
- Inside the ARI event loop's handler: when `type == "StasisStart"`,
  extract `channel.id` from the JSON and call your `answerChannel`
  (optionally follow with `play`, e.g. `sound:hello-world`).

## Steps

1. Start both connections (AMI login, ARI websocket dial).
2. Launch each one's read/event loop in a goroutine.
3. On `StasisStart` in the ARI loop, answer the channel.
4. In `main`, block on `<-ctx.Done()`, then close both connections and
   exit.

## You're done when

Run `go run .`, then either register a softphone to extension `1000` and
call it, or:

```bash
docker exec deploy-asterisk-1 asterisk -rx "channel originate Local/1000@stasis-in application Echo"
```

The channel should go `Up` on its own — your program answered it without
you touching `04`'s manual command. Ctrl+C should stop the program
cleanly (no hanging, no panic).

## What now

- Read `../../api/cmd/atsap-api/main.go` and the packages it uses
  (`../../api/internal/ari`, `../../api/internal/ami`). You just built a
  rough version of the same thing by hand — see what's different (retry
  with backoff, structured logging via `log/slog`, graceful HTTP shutdown)
  and why.
- Try `../../api/cmd/ari-playground` — a CLI built on the production
  packages that does roughly what your `04`/`05` do, for comparison.
- Extend your mini-app: route based on the dialed extension
  (`channel.dialplan.exten` in the `StasisStart` payload), play different
  sounds, or originate an outbound call instead of just answering inbound
  ones.
