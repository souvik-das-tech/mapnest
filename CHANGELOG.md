# Changelog

All notable changes are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-11-03

First public release.

### Security
- Server-side sessions (SQLite) with absolute expiry (`MSAPI_SESSION_HOURS`,
  default 8) and idle timeout (`MSAPI_SESSION_IDLE_MINUTES`, default 30);
  sign-out revokes the session; `DELETE /api/v1/auth/sessions` signs out
  everywhere; `GET /api/v1/auth/session` shows the current session.
- Sign-in lockout per username and client IP (`MSAPI_LOGIN_MAX_ATTEMPTS`,
  `MSAPI_LOGIN_LOCKOUT_MINUTES`), `429` with `Retry-After`.
- Strong secrets are required: no default admin password, 12+ character
  password, 32+ character API key; known defaults are refused.
- Security headers on every API response (CSP, X-Frame-Options, nosniff,
  Referrer-Policy, Permissions-Policy, `Cache-Control: no-store`) and on the
  web UI (strict CSP without inline scripts, HSTS over HTTPS).
- nginx: version hidden, rate limits for sign-in, API and WMS/WFS, method
  restrictions, slow-client timeouts, dotfiles blocked.
- Apache (MapServer): `ServerTokens Prod`, `TraceEnable Off`, no directory
  listing, only GET/POST/HEAD.
- MapServer: client-supplied SLD disabled (SSRF), `wfs_maxfeatures 10000`.
- Anonymous `/health` no longer reveals the version or error details.
- OpenAPI docs and Swagger UI need sign-in unless `MSAPI_PUBLIC_DOCS=on`;
  Swagger assets use Subresource Integrity.
- Audit log: security events and all changes carry `"audit":true` with actor
  and client IP; client IP is taken from trusted proxies only
  (`MSAPI_TRUSTED_PROXIES`).
- Restored backups re-validate icons.
- Containers: unprivileged nginx, dropped capabilities, `no-new-privileges`,
  `pids_limit`, log rotation, health checks; admin API bound to `127.0.0.1`.
- Optional HTTPS with Caddy (`web/docker-compose.tls.yml`).

### Added
- MapNest logo, wordmark and social preview (`docs/assets/`), used in the Admin UI.
- Documentation: API guide (API-only use), styling, configuration, deployment
  and architecture guides (`docs/`).
- Apache-2.0 license, `SECURITY.md`, VAPT Level 1 checklist
  (`docs/security/VAPT-L1.md`), contributing guide, CI (tests, govulncheck,
  npm audit, Trivy, CodeQL, gitleaks) and Dependabot.

### Changed (upgrade notes)
- `MSAPI_ADMIN_PASSWORD` and `MSAPI_API_KEY` must now be set in
  `server/.env` and be strong; msapi exits with a clear message otherwise.
- The web container listens on 8080 inside (published port unchanged, 3000).
- Existing sign-ins end once after the upgrade (new token format).
