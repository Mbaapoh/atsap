# Console deployment

How the AtsaPBX console reaches the API, in development and in production
(D-69). The rule behind every choice here: the console and the API share
**one origin**, because the session's refresh secret lives in an
`HttpOnly; SameSite=Strict` cookie scoped to `/atsapbx.v1.AuthService/`
(D-62), which a browser sends only to the same site.

| Item | Development | Production |
|---|---|---|
| Console origin | `http://localhost:5173` (Vite dev server) | `https://pbx.example.com`, the partner's own hostname |
| API base | Same origin; Vite proxies `/atsapbx.v1.*` to `http://localhost:8080` | Same origin; the reverse proxy forwards `/atsapbx.v1.*` to `app:8080` |
| API paths | `/atsapbx.v1.<Service>/<Method>` (ConnectRPC, JSON) | Same |
| SPA history fallback | Built into the Vite dev server | `try_files {path} /index.html` |
| HTTPS | None on the dev rig; the API runs with `ATSAPBX_COOKIE_INSECURE=true` there only | Required. The cookie is `Secure`; HSTS is set |
| Security headers | Not set | Set by the proxy (below) |

The console calls the API with relative URLs, built from the page's own
origin, so no API address is compiled into the build. One build serves
every installation.

## Production

`deploy/Caddyfile.example` is a complete front door: HTTPS, the API
forward, the console files with history fallback, caching, and these
headers:

| Header | Value | Why |
|---|---|---|
| `Content-Security-Policy` | `default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'` | The build has no inline script and no third-party origin, so everything can be `'self'`. `frame-ancestors 'none'` stops clickjacking |
| `Strict-Transport-Security` | `max-age=31536000; includeSubDomains` | HTTPS only, always |
| `X-Content-Type-Options` | `nosniff` | No MIME sniffing |
| `Referrer-Policy` | `no-referrer` | Nothing about the console leaks to linked sites. Reset links carry their secret in the URL fragment, which browsers never send anyway |
| `Cross-Origin-Opener-Policy` | `same-origin` | Isolates the window from pages it opens |
| `Permissions-Policy` | `camera=(), microphone=(), geolocation=(), payment=()` | The admin console needs none. The Phase B softphone will change this deliberately |

Build with `npm ci && npm run build`, then copy `dist/` to the proxy's
console root (`/srv/console` in the example). Hashed files under
`/assets/` are cached for a year; `index.html` always revalidates, so a new
release is picked up on the next load.

nginx works equally well: `location /atsapbx.v1. { proxy_pass
http://app:8080; }`, `try_files $uri /index.html;` for everything else, and
the same headers.

## Development

```bash
cd console
mise install          # Node 24, npm 11 (runtimes only)
npm ci
npm run dev           # http://localhost:5173, API proxied to :8080
```

The dev rig's API must be running (`make dev` at the repository root).
