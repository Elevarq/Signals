# Publication Checklist — Arq Signals v0.1.0

## Build & Test
- [ ] `git clone && cd arq-signals && make build` succeeds on clean checkout
- [ ] `make test` passes (all tests green)
- [ ] `make boundary` passes (no proprietary leakage)
- [ ] `make docker-build` produces working image
- [ ] `go vet ./...` reports no issues

## Repository Files
- [ ] LICENSE present (BSD-3-Clause)
- [ ] README.md is public-facing, professional
- [ ] CHANGELOG.md documents v0.1.0
- [ ] CONTRIBUTING.md present
- [ ] SECURITY.md present
- [ ] GOVERNANCE.md present
- [ ] CODE_OF_CONDUCT.md present
- [ ] .gitignore covers binaries, databases, secrets

## GitHub Configuration
- [ ] Issue templates present (.github/ISSUE_TEMPLATE/)
- [ ] PR template present (.github/PULL_REQUEST_TEMPLATE.md)
- [ ] CI workflow present (.github/workflows/ci.yml)

## Documentation
- [ ] docs/faq.md answers common questions
- [ ] docs/adoption-guide.md covers deployment scenarios
- [ ] examples/signals.yaml is annotated
- [ ] examples/docker-compose.yml works end-to-end
- [ ] examples/snapshot-example/ shows output format

## Security Review
- [ ] No credentials, API keys, or passwords in any file
- [ ] No internal company URLs or endpoints
- [ ] No proprietary analysis logic, scoring, or AI code
- [ ] Boundary tests verify no forbidden imports
- [ ] go.mod has correct public module path
- [ ] No .env files committed

## Content Review
- [ ] No internal-only terminology
- [ ] No "free version" or "limited edition" framing
- [ ] README explains what the tool does NOT do
- [ ] FAQ addresses data privacy questions
- [ ] License is clearly stated
