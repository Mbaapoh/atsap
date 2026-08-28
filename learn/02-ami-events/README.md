# 02 — AMI events

**Goal:** after logging in (stage 1), keep reading from the same
connection and print every unsolicited **Event** message Asterisk sends,
until you kill the program.

## What's new here

After a successful login, Asterisk doesn't just go quiet — it starts
pushing you messages continuously: every channel created, every state
change, every hangup, as calls happen. These look exactly like the login
response did: a block of `Key: Value` lines ending in a blank line, except
one of the keys is `Event` (e.g. `Event: Newchannel`) instead of
`Response`.

So the core problem here is: **write a function that reads one complete
message** (a block of lines up to a blank line) and returns it as
something structured — a `map[string]string` is a natural fit, keyed by
the field name. You'll call that function in a loop, forever.

## What you need

- Everything from stage 1 (dial, banner, login).
- A loop that keeps reading messages after login succeeds.
- A way to split a `"Key: Value"` line into its two parts —
  `strings.Cut` or `strings.SplitN` both work.
- Only print messages that have an `Event` key (skip anything else, e.g.
  stray login-response leftovers).

## Steps

1. Copy your working stage-1 connect+login code (or import it — up to
   you; these are standalone exercises so copy-paste is fine here).
2. Write `readMessage(r *bufio.Reader) (map[string]string, error)`:
   loop reading lines until you hit an empty line, building up the map.
3. In `main`, after login, loop calling `readMessage` and print
   `msg["Event"]` plus the whole map whenever `"Event"` is present.

## You're done when

With your program running, generate a test call from another terminal:

```bash
docker exec deploy-asterisk-1 asterisk -rx "channel originate Local/1000@stasis-in application Echo"
docker exec deploy-asterisk-1 asterisk -rx "channel request hangup all"
```

You should see a stream of events print — `Newchannel`, `Newstate`,
`Hangup`, and others — in real time.

## Next

`03-ari-info` switches to the *other* API — ARI over plain HTTP — before
`04` and `05` bring the two together.
