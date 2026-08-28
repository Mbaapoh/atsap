# 01 — AMI hello

**Goal:** open a raw TCP connection to Asterisk's Manager Interface, read
its banner, log in by hand, and print whether it worked.

## What AMI actually is

It's not HTTP. It's a plain TCP socket where every message — both what
you send and what Asterisk sends back — is a set of `Key: Value` lines
ending in `\r\n`, with the whole message terminated by one blank line
(`\r\n\r\n`). The very first thing Asterisk sends when you connect,
before any message, is a single banner line like:

```
Asterisk Call Manager/9.0.0
```

To log in, you send an **Action**:

```
Action: Login
Username: voipapp
Secret: devpassword123

```
(note the trailing blank line — that's what tells Asterisk your message is complete)

Asterisk replies with a message containing `Response: Success` (or
`Response: Error` with a `Message:` field explaining why).

## What you need

- `AMI_ADDR` (host:port, e.g. `localhost:5038`), `AMI_USERNAME`,
  `AMI_PASSWORD` — already exported if you `source ../env.sh`.
- `net.Dial("tcp", addr)` to connect.
- Something to read line by line — `bufio.NewReader` + `ReadString('\n')`
  works fine here.
- `fmt.Fprintf` (or `conn.Write`) to send your Login action.

## Steps

1. Dial the AMI address.
2. Read and print the banner line.
3. Write a `Login` action with your username/password, ending in a blank
   line.
4. Read lines until you hit a blank line, printing each one — that's
   Asterisk's response to your login.
5. Check whether the response contained `Response: Success`.

## You're done when

Running `go run .` prints the banner and a response block containing
`Response: Success`. Try it once with a deliberately wrong password too
— see what `Response: Error` looks like.

## Next

Once this works, `02-ami-events` reuses this same connection to read the
*stream* of events Asterisk sends after login, instead of just one reply.
