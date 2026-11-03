# VAPT Level 1 checklist

This document maps the usual **VAPT Level 1** (OWASP Top 10 / OWASP ASVS level 1
style) test cases to the controls in MapNest, where each control lives in the
code, and how a tester can verify it. Status is one of:

- **Pass**: the control is in place by default.
- **Config**: in place once the operator follows the production checklist in
  [SECURITY.md](../../SECURITY.md) (for example HTTPS).
- **Accepted**: a known, documented limitation (see SECURITY.md).

Examples use `UI=https://maps.example.com` (the web UI, nginx) and
`API=http://127.0.0.1:8081` (msapi, on the server host). `$KEY` is
`MSAPI_API_KEY`.

## 1. Authentication

| # | Test | Status | Control | Where |
|---|---|---|---|---|
| 1.1 | Default or weak credentials | Pass | No default password. msapi refuses to start if `MSAPI_ADMIN_PASSWORD` is missing, shorter than 12 characters, a known default (`mapserver`, `admin`, …) or equal to the username; same for an API key under 32 characters. `MSAPI_INSECURE_DEV=true` relaxes this for local development only and logs a warning. | `server/msapi/internal/config/config.go` (`checkSecrets`) |
| 1.2 | Brute force / account lockout | Pass | After `MSAPI_LOGIN_MAX_ATTEMPTS` (5) failures per username **or** client IP, sign-in answers `429` with `Retry-After` for `MSAPI_LOGIN_LOCKOUT_MINUTES` (15), even with the right password. nginx also limits `/api/v1/auth/login` to 10 requests/minute per IP. | `internal/api/security.go` (`loginLimiter`), `web/nginx.conf` |
| 1.3 | Username enumeration | Pass | One message for a wrong user or a wrong password (`wrong username or password`), same delay; values compared as SHA-256 digests in constant time. | `internal/api/auth.go` (`login`, `equalDigest`) |
| 1.4 | Credentials over plain HTTP | Config | Use the TLS overlay (`web/docker-compose.tls.yml`, Caddy, TLS 1.2+ only, automatic certificates, HTTP→HTTPS redirect) or your own TLS proxy. | `web/tls/Caddyfile` |
| 1.5 | API key handling | Pass | `X-API-Key` header only (never in the URL, so not in logs); constant-time comparison; failed keys are written to the audit log. | `internal/api/auth.go`, `server.go` |
| 1.6 | Password autocomplete / masking | Pass | `type="password"`, `autocomplete="current-password"`. | `web/src/features/auth/Login.tsx` |

Verify:

```bash
# 1.1: refuses to start
MSAPI_ADMIN_PASSWORD=mapserver docker compose up msapi   # -> "MSAPI_ADMIN_PASSWORD is a default or guessable value"

# 1.2: the 6th attempt is 429 with Retry-After
for i in 1 2 3 4 5 6; do
  curl -s -o /dev/null -w "%{http_code}\n" -X POST $UI/api/v1/auth/login \
    -H 'Content-Type: application/json' -d '{"username":"admin","password":"wrong"}'
done
```

## 2. Session management

| # | Test | Status | Control | Where |
|---|---|---|---|---|
| 2.1 | Session token randomness | Pass | 256-bit random id from `crypto/rand`, HMAC-SHA256 signed. | `internal/api/auth.go` |
| 2.2 | Session fixation | Pass | A new id is created on every sign-in; a client cannot choose it. | `auth.go` (`login`) |
| 2.3 | Logout invalidates the session | Pass | `POST /auth/logout` deletes the server-side session; the old token gets `401`. "Sign out everywhere": `DELETE /auth/sessions`. | `auth.go`, `internal/store/sessions.go` |
| 2.4 | Absolute timeout | Pass | `MSAPI_SESSION_HOURS` (default 8). | `config.go`, `auth.go` (`validSession`) |
| 2.5 | Idle timeout | Pass | `MSAPI_SESSION_IDLE_MINUTES` (default 30). Background health polling does not extend it. | `auth.go` (`validSession`, `touch`) |
| 2.6 | Token tampering | Pass | HMAC verified before any lookup; tampered tokens get `401`. | `auth.go` (`parseToken`) |
| 2.7 | Password change revokes sessions | Pass | The HMAC key includes the admin password. | `auth.go` (`sessionKey`) |
| 2.8 | Session ids at rest | Pass | Only SHA-256 of the id is stored, so a stolen database or backup cannot be replayed. | `migrations/0004_sessions.sql` |
| 2.9 | Cookie flags (Secure/HttpOnly/SameSite) | N/A | No cookies are used; the token is sent as `Authorization: Bearer`. | `web/src/api/client.ts` |
| 2.10 | CSRF | Pass | Not applicable to bearer tokens (browsers never attach them automatically); no CORS headers are sent, so other origins cannot read API responses. | — |
| 2.11 | Token storage | Accepted | `sessionStorage` (cleared when the tab closes, not shared between tabs). Mitigated by the strict CSP in 4.1. | `client.ts` |
| 2.12 | Client data after logout | Pass | The UI clears its query cache on sign-out and on any `401`. | `web/src/app/App.tsx` |

Verify:

```bash
TOKEN=$(curl -s -X POST $UI/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<password>"}' | jq -r .token)
curl -s -o /dev/null -w "%{http_code}\n" $UI/api/v1/stores -H "Authorization: Bearer $TOKEN"   # 200
curl -s -X POST $UI/api/v1/auth/logout -H "Authorization: Bearer $TOKEN"
curl -s -o /dev/null -w "%{http_code}\n" $UI/api/v1/stores -H "Authorization: Bearer $TOKEN"   # 401
```

## 3. Access control

| # | Test | Status | Control | Where |
|---|---|---|---|---|
| 3.1 | Unauthenticated access to admin endpoints | Pass | Everything under `/api/v1` needs a session or the API key, except `/health`, `/auth/login` and `/auth/logout`. Unknown paths answer `401` before `404`, so routes cannot be enumerated anonymously. | `internal/api/server.go` (`Handler`) |
| 3.2 | API documentation exposure | Pass | `/api/v1/docs` and `/swagger` need sign-in unless `MSAPI_PUBLIC_DOCS=on`. | `server.go` |
| 3.3 | Information in health checks | Pass | Anonymous `/health` returns only ok/degraded per check: no version, no hostnames, no error text. | `internal/api/health.go` |
| 3.4 | Admin API network exposure | Pass | Port 8081 binds to `127.0.0.1` by default; the UI reaches it over the Docker network. | `server/docker-compose.yml` (`MSAPI_BIND`) |
| 3.5 | Arbitrary mapfile via `map=` | Pass | `MS_MAP_PATTERN` allows only `services.map` and `ws_<name>.map`. | `server/mapserver/mapserver.conf` |
| 3.6 | Per-user roles, per-layer WMS access | Accepted | Single admin; WMS/WFS public by design (SECURITY.md). | — |

Verify:

```bash
curl -s -o /dev/null -w "%{http_code}\n" $UI/api/v1/stores          # 401
curl -s -o /dev/null -w "%{http_code}\n" $UI/api/v1/docs            # 401
curl -s $UI/api/v1/health                                           # no "version", no hostnames
curl -s "$UI/ows?map=/etc/passwd&SERVICE=WMS&REQUEST=GetCapabilities" | head -3   # MapServer refuses the path
```

## 4. Security headers and browser protections

| # | Test | Status | Control | Where |
|---|---|---|---|---|
| 4.1 | Content-Security-Policy | Pass | UI: `script-src 'self'` (no inline script, no eval, no third-party scripts), `object-src 'none'`, `frame-ancestors 'none'`, `base-uri 'self'`, `form-action 'self'`; images only from self, `data:`, `blob:` and OpenStreetMap tiles. API: `default-src 'none'`. WMS/WFS: `default-src 'none'; sandbox`. | `web/nginx/mapnest-security-headers.conf`, `internal/api/security.go`, `web/nginx/mapnest-ows-headers.conf` |
| 4.2 | Clickjacking | Pass | `X-Frame-Options: DENY` and `frame-ancestors 'none'`. | same |
| 4.3 | MIME sniffing | Pass | `X-Content-Type-Options: nosniff` on UI, API and WMS. | same, `server/mapserver/mapnest-vhost.conf` |
| 4.4 | HSTS | Config | `Strict-Transport-Security: max-age=31536000; includeSubDomains` whenever the request came over HTTPS. | `web/nginx.conf` (`$mapnest_hsts`) |
| 4.5 | Referrer leakage | Pass | `Referrer-Policy: strict-origin-when-cross-origin` (UI; OpenStreetMap needs the origin), `no-referrer` (API). | same |
| 4.6 | Permissions-Policy | Pass | Camera, microphone, geolocation, payment, USB disabled. | same |
| 4.7 | Cross-origin isolation | Pass | `Cross-Origin-Opener-Policy: same-origin`; API also `Cross-Origin-Resource-Policy: same-origin`. | same |
| 4.8 | Caching of sensitive responses | Pass | API: `Cache-Control: no-store` (icons: `private`). `index.html`: `no-cache`. | `security.go`, `nginx.conf` |
| 4.9 | Third-party script integrity | Pass | The only CDN asset (Swagger UI) is pinned and loaded with Subresource Integrity; its page has a hash-based CSP. | `internal/api/system.go` |
| 4.10 | Source maps in production | Pass | Not generated. | `web/vite.config.ts` |

Verify:

```bash
curl -sI $UI/ | grep -iE 'content-security|x-frame|x-content-type|referrer|permissions|strict-transport|server'
curl -sI -H "X-API-Key: $KEY" $UI/api/v1/stores | grep -iE 'content-security|cache-control|x-frame'
```

## 5. Information disclosure and server configuration

| # | Test | Status | Control | Where |
|---|---|---|---|---|
| 5.1 | Server version banner | Pass | nginx `server_tokens off`; Apache `ServerTokens Prod`, `ServerSignature Off`; Caddy's `Server` header removed; `X-Powered-By` stripped. | `web/nginx.conf`, `mapnest-vhost.conf`, `web/tls/Caddyfile` |
| 5.2 | Verbose errors / stack traces | Pass | Panics and internal errors are logged server-side and answered with `{"error":"internal error"}`. | `internal/api/server.go` (`recoverPanics`, `internalError`) |
| 5.3 | HTTP TRACE / unexpected methods | Pass | Apache `TraceEnable Off` and only GET/POST/HEAD; nginx allows only GET (UI), GET/POST (`/ows`), POST (`/auth/login`); Go's router answers `405` for others. | `mapnest-vhost.conf`, `nginx.conf` |
| 5.4 | Directory listing | Pass | Apache `Options -Indexes`; nginx serves only the built app (`try_files`). | same |
| 5.5 | Hidden files (`.env`, `.git`) | Pass | nginx returns `404` for any path segment starting with a dot; `.env` files are git-ignored and never copied into images (`.dockerignore`). | `nginx.conf`, `.gitignore` |
| 5.6 | Secrets in the repository | Pass | No credentials in tracked files; CI runs gitleaks on every push. | `.github/workflows/security.yml` |
| 5.7 | Secrets in API responses | Pass | Store passwords and S3 secret keys are write-only: responses carry only `has_secret`. | `internal/api/stores.go` |
| 5.8 | Secrets at rest | Pass | Store secrets encrypted with AES-256-GCM (`MSAPI_SECRET_KEY`). | `internal/crypto` |
| 5.9 | Secrets in logs | Pass | Passwords, tokens and query strings are never logged. | `server.go` (`logRequests`) |

Verify:

```bash
curl -sI $UI/ | grep -i '^server'                                   # "Server: nginx" with no version
curl -s -o /dev/null -w "%{http_code}\n" -X TRACE http://<host>:8080/   # 403 or 405
curl -s -o /dev/null -w "%{http_code}\n" $UI/.env                   # 404
```

## 6. Input validation and injection

| # | Test | Status | Control | Where |
|---|---|---|---|---|
| 6.1 | SQL injection | Pass | Values are bound parameters; table and column names go through `pgx.Identifier.Sanitize()`; every user-database session is read-only; documentation asks for a SELECT-only role. | `internal/inspect/postgis.go`, `internal/api/attributes.go`, `internal/pgread/pgread.go` |
| 6.2 | Mapfile injection | Pass | Every string written into a mapfile is escaped by `mapfile.Quote`; names match strict patterns (`^[A-Za-z][A-Za-z0-9_]{0,62}$`); SLD attribute names are checked; the generated mapfile is validated by `mapserv` before it goes live. | `internal/mapfile/build.go`, `internal/model/store.go`, `internal/sld/filter.go` |
| 6.3 | XSS (stored / reflected) | Pass | React escapes all output; no `dangerouslySetInnerHTML`; strict CSP (4.1); GetFeatureInfo is parsed as GML, not rendered as HTML; any HTML from `/ows` is sandboxed. | `web/src`, `mapnest-ows-headers.conf` |
| 6.4 | XXE in SLD uploads | Pass | Go's `encoding/xml` does not resolve external entities or DTDs. | `internal/sld/xmlnode.go` |
| 6.5 | SSRF through WMS `SLD=` | Pass | Client-supplied SLD is disabled (`wms_sld_enabled false`), so MapServer never fetches a URL given by a client. | `internal/mapfile/templates/map.tmpl` |
| 6.6 | SSRF through store connections | Accepted | Admin-only feature (like GeoServer); see SECURITY.md. | — |
| 6.7 | Path traversal | Pass | Local-folder stores resolve symlinks and must stay inside `MSAPI_RASTER_DIR`; icon names match `^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}\.png$` and are stored under a content hash; backup extraction accepts only `msadmin.db` and `icons/*.png`. | `internal/localfs`, `internal/api/icons.go`, `internal/api/system.go` |
| 6.8 | File upload | Pass | Icons: PNG only (decoded, not trusted by extension), ≤ 2 MB, ≤ 512×512 px. Restore: gzip tar ≤ 512 MB, schema version checked, icons re-validated. | `icons.go`, `system.go` |
| 6.9 | Mass assignment / unexpected JSON | Pass | JSON bodies are decoded with `DisallowUnknownFields` and a 1 MB limit. | `server.go` (`decodeJSON`) |

## 7. Denial of service and resource limits

| # | Test | Status | Control | Where |
|---|---|---|---|---|
| 7.1 | Rate limiting | Pass | nginx per IP: sign-in 10/min, API 30/s, WMS/WFS 60/s (with bursts), 100 concurrent connections; `429` when exceeded. | `web/nginx.conf` |
| 7.2 | Slow clients (slowloris) | Pass | nginx header/body/send timeouts; msapi `ReadHeaderTimeout` 10 s, `ReadTimeout` 10 min, 64 KB header limit. | `nginx.conf`, `cmd/msapi/main.go` |
| 7.3 | Huge map requests | Pass | `MAXSIZE 4096` px per GetMap; `wfs_maxfeatures 10000`; MapServer `IO_TIMEOUT`. | `map.tmpl`, `server/docker-compose.yml` |
| 7.4 | Large request bodies | Pass | 20 MB in nginx (512 MB only for restore), 1 MB JSON, 2 MB icons. | `nginx.conf`, `server.go`, `icons.go` |
| 7.5 | Container resource limits | Pass | `pids_limit` on every container; log rotation (10 MB × 5). | compose files |

## 8. Infrastructure, containers and supply chain

| # | Test | Status | Control | Where |
|---|---|---|---|---|
| 8.1 | Containers running as root | Pass / Accepted | Web: `nginx-unprivileged`, no capabilities. msapi: all capabilities dropped except `CHOWN`, `DAC_OVERRIDE`, `FOWNER` (to write the shared mapfile volume). MapServer: stock image, Apache drops to `www-data`. All: `no-new-privileges`. | compose files, `web/Dockerfile` |
| 8.2 | Health checks | Pass | Docker `HEALTHCHECK` for web and msapi. | Dockerfiles |
| 8.3 | Vulnerable dependencies | Pass | CI: `govulncheck`, `npm audit` (high and above), Trivy image scan, CodeQL; Dependabot for Go, npm, Docker and Actions. | `.github/workflows`, `.github/dependabot.yml` |
| 8.4 | Reproducible builds | Pass | `npm ci` from the lockfile with install scripts disabled; Go modules verified against `go.sum`, `-mod=readonly`; base images pinned to minor versions. | Dockerfiles |
| 8.5 | TLS configuration | Config | TLS 1.2 and 1.3 only, modern ciphers (Caddy defaults), HTTP/2 and HTTP/3. | `web/tls/Caddyfile` |

## 9. Logging and monitoring

| # | Test | Status | Control | Where |
|---|---|---|---|---|
| 9.1 | Audit trail of security events | Pass | JSON log lines with `"audit":true` for sign-in success/failure/lockout, logout, session revocation, rejected API keys and every change (POST/PUT/DELETE), each with actor and client IP. | `internal/api/security.go` (`audit`), `server.go` (`logRequests`) |
| 9.2 | Client IP spoofing in logs | Pass | `X-Forwarded-For` / `X-Real-IP` are believed only from `MSAPI_TRUSTED_PROXIES`; nginx overwrites (never appends) them. | `security.go` (`clientIP`), `web/nginx/mapnest-proxy.conf` |

Verify:

```bash
docker logs msapi 2>&1 | grep '"audit":true' | tail
```

## Automated tests

The security behaviour is covered by Go tests that run in CI:

| Test | File |
|---|---|
| Weak and default secrets refused | `internal/config/config_test.go` |
| Lockout, limiter window | `internal/api/security_test.go` (`TestLoginLockout`, `TestLimiterWindow`) |
| Logout, revoke all, idle timeout, password change | `security_test.go` |
| Security headers, private docs, health redaction | `security_test.go` |
| Client IP from trusted proxies only | `security_test.go` (`TestClientIP`) |
| Backup path traversal, local folder escape | `internal/api/backup_test.go`, `internal/localfs/localfs_test.go` |
