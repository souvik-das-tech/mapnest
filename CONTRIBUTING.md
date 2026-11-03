# Contributing to MapNest

Thanks for helping. MapNest is small on purpose: one Go binary, one React app,
stock MapServer. Changes that keep it light are the easiest to accept.

## Ground rules

- Everything runs in Docker. You do not need Go or Node on your machine; the
  commands below run the toolchains in containers.
- Security problems go through private reporting, not issues: see
  [SECURITY.md](SECURITY.md).
- Be kind: see [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Development

```bash
# API: format check, vet, tests
docker run --rm -v "$PWD:/repo" -w /repo/server/msapi golang:1.26-alpine \
  sh -c 'test -z "$(gofmt -l .)" && go vet ./... && go test ./...'

# UI dev server with hot reload (proxies /api and /ows like nginx does)
docker run --rm -it -p 5173:5173 -v "$PWD/web:/app" -w /app \
  -e MSAPI_UPSTREAM=http://host.docker.internal:8081 \
  -e MAPSERVER_UPSTREAM=http://host.docker.internal:8080 \
  node:24-alpine sh -c 'npm ci && npm run dev -- --host'
```

For a local stack with throwaway secrets you may set `MSAPI_INSECURE_DEV=true`
in `server/.env`. Never do this on a shared or public host.

## Pull requests

1. One topic per pull request; describe what changed and why.
2. Add or update tests for behaviour changes (`server/msapi/internal/...`).
3. Keep `README.md`, the OpenAPI spec (`internal/api/openapi.yaml`) and
   `docs/security/VAPT-L1.md` in step with the code.
4. CI must pass: gofmt, go vet, tests, govulncheck, TypeScript build, npm audit,
   image build and scans, CodeQL and gitleaks.

By contributing you agree that your contribution is licensed under the
Apache License 2.0, like the rest of the project.
