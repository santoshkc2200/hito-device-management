## What

<!-- One or two sentences: what changed and why. -->

## Requirement / ADR references

<!-- e.g. FR-45, INV-13, ADR-0004. See docs/01-requirements.md and docs/adr/. -->

## Test plan

- [ ] `task test` passes
- [ ] `task test:integration` passes (if this touches migrations, queries, or module wiring)
- [ ] `task lint` passes, including the module-boundary guard
- [ ] Manually exercised in `task dev` (if this touches a kiosk or admin screen)

## Checklist

- [ ] `task generate` produces no diff (spec, generated Go, generated TS all in sync)
- [ ] No PII added to logs, error bodies, or the schema without an explicit decision (docs/09-security-privacy-ops.md)
- [ ] New invariants added to docs/03-domain-model.md if this introduces one
