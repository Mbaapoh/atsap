# learn/

A scratch space for learning Go by hand-writing the two client protocols
this whole project is built on: Asterisk's **AMI** (Manager Interface,
a line-oriented protocol over TCP) and **ARI** (REST Interface, HTTP +
a WebSocket event stream). It's a separate Go module (`atsap-learn`) so it
never touches the `api/` module or its dependencies — break things freely.

The production versions of everything you'll build here already exist in
`../api/internal/ari` and `../api/internal/ami`. **Don't open those until
you've had a real attempt at each stage** — the value is in hitting the
problems yourself first (how do I know where one AMI message ends? how do
I know what shape the JSON is?). Afterwards, comparing your version to the
real one is a great way to see *why* it's shaped the way it is.

## Workflow: no Docker builds, ever

Your code runs on the host with plain `go run` — it recompiles in under a
second. The only thing that needs to be running in Docker is Asterisk
itself (plus Postgres, which Asterisk depends on to start):

```bash
# from the repo root, once per session:
docker compose -f deploy/docker-compose.yml up -d postgres asterisk

# the go app container competes with you for the same Stasis app —
# make sure it's not running while you work through these stages:
docker compose -f deploy/docker-compose.yml stop app
```

Leave that running for your whole learning session. Then, for every stage:

```bash
source learn/env.sh          # once per shell — points at localhost, not "asterisk"
cd learn/01-ami-hello
go run .                     # edit main.go, run again, repeat
```

That's the entire loop. No `docker build`, no `docker compose up --build`,
nothing to wait on.

**To generate test traffic** (no softphone needed), from another terminal:

```bash
docker exec deploy-asterisk-1 asterisk -rx "channel originate Local/1000@stasis-in application Echo"
docker exec deploy-asterisk-1 asterisk -rx "core show channels concise"   # see channel IDs
docker exec deploy-asterisk-1 asterisk -rx "channel request hangup all"
```

Or register a real softphone to extension `1000` / `devpassword123` at
`localhost:5060` (UDP).

When you want your changes to Asterisk's own config to take effect (you
generally won't need to for these stages), `docker compose -f
deploy/docker-compose.yml restart asterisk` is instant — still no build.

## The stages

Work through them in order — each one only needs what came before it.

| # | Folder | Builds | Go concepts practiced | Asterisk concept |
|---|--------|--------|------------------------|-------------------|
| 1 | `01-ami-hello` | Connect to AMI, log in | `net.Dial`, `bufio.Reader`, string formatting | AMI's banner + Login action |
| 2 | `02-ami-events` | Print events as they arrive | loops, `map[string]string`, parsing, functions | AMI's Key: Value message framing |
| 3 | `03-ari-info` | Authenticated GET to ARI | `net/http`, `encoding/json`, structs | ARI is just HTTP + basic auth |
| 4 | `04-ari-actions` | Answer / hang up a real channel | functions returning `error`, `os.Args`, URL building | ARI's REST actions (POST/DELETE) |
| 5 | `05-ari-events-ws` | Read the ARI event stream | goroutines, closures, JSON with a type discriminator | ARI's WebSocket event feed |
| 6 | `06-mini-app` | Your own tiny VoIP app | tying it together: `context`, `os/signal`, concurrency | StasisStart → answer, end to end |

Each stage folder has its own `README.md` with the goal, the exact API
details you need (endpoint paths, JSON field names, env vars), and a
"you're done when" check. `main.go` in each folder is intentionally close
to empty — that's where you write your attempt.

## After stage 6

A few directions if you want to keep going:
- Compare your `05`/`06` against `../api/internal/ari/events.go` — notice
  it adds reconnect-with-backoff. Try adding that to yours.
- Compare your AMI client against `../api/internal/ami/client.go` — notice
  it's built around an `EventHandler` function type and a mutex-guarded
  connection. Why might that matter once you add concurrent writes?
- `../api/cmd/ari-playground` is a small CLI built on the *production*
  packages — once your own version works, see how it compares.
- Try reading CDR rows back out of Postgres after a call (`docker exec
  deploy-postgres-1 psql -U asterisk -d asterisk -c "select * from cdr"`)
  and correlate them with the events you saw.
