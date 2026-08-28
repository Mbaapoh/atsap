# 03 — ARI info

**Goal:** make one authenticated HTTP request to Asterisk's REST API and
decode the JSON response into a Go struct.

## What ARI actually is

Unlike AMI, ARI is just HTTP. Every ARI call is a normal request with
HTTP Basic Auth, and every response is JSON. The simplest possible ARI
call is `GET /ari/asterisk/info`, which needs no channel, no call, no
setup — just credentials. It returns something shaped like:

```json
{
  "build": { "os": "Linux", "date": "...", ... },
  "system": { "version": "21.12.3", "entity_id": "..." },
  "config": { "default_language": "en", ... },
  "status": { "startup_time": "...", "last_reload_time": "..." }
}
```

## What you need

- `ARI_URL` (e.g. `http://localhost:8088/ari`), `ARI_USERNAME`,
  `ARI_PASSWORD` — from `../env.sh`.
- `http.NewRequest` + `req.SetBasicAuth(user, pass)`.
- A struct with `json:"..."` tags matching the shape above (you only need
  the fields you care about — you don't have to model the whole response).
- `encoding/json.Unmarshal` (or `json.NewDecoder(resp.Body).Decode(...)`).

## Steps

1. Build the request: `GET {ARI_URL}/asterisk/info`.
2. Set basic auth, send it with `http.DefaultClient.Do` (or a client you
   construct yourself).
3. Check `resp.StatusCode` — ARI returns non-2xx with a JSON body like
   `{"message": "..."}` on errors; try it with a wrong password and see.
4. Decode the body into your struct.
5. Print the Asterisk version (`system.version`).

## You're done when

`go run .` prints something like `Asterisk version: 21.12.3`.

## Next

`04-ari-actions` uses the same request-building pattern, but to change
something (answer a real channel) instead of just reading.
