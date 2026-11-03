## What and why

## How it was tested
- [ ] `go test ./...` (in Docker, see README "Development")
- [ ] `npm run build` in `web/`
- [ ] Tried in the running stack

## Security checklist
- [ ] No secrets, credentials or real data in the diff
- [ ] New endpoints are behind authentication (or the reason is documented)
- [ ] New user input is validated; names written into mapfiles go through `mapfile.Quote`
- [ ] `docs/security/VAPT-L1.md` updated if a control changed
