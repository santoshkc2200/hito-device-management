# Phase 4 — sub-phase breakdown

The parent plan, [phase-4-admin-console.md](../phase-4-admin-console.md), states
*what* Phase 4 must deliver. This folder states *how it is built, in what order,
and how each step is proven done* — one file per sub-phase, each holding the
lettered units that are the actual branch-and-commit slices
(`feat(phase-4.4b): ...`).

Phase 2 decided what is true and Phase 3 decided what a nurse sees. Phase 4 is
the only surface where a human can *change* what is true — every borrower is
created here, every card is issued here, every override is authorised here, and
every paper page is typed in here. Two consequences shape the whole breakdown:

- **Registration is on the critical path for the entire product.** The kiosk
  cannot register anyone (FR-40). Until [4.4b](4.4-users-and-registration.md) and
  [4.5c](4.5-credentials.md) land, Phase 3's field testing has no real cards in
  real hands. That is why the order below is *not* the parent's numbering.
- **The two starred screens fail by being tedious, not by being broken.**
  Registration and paper backfill both have timed exit criteria measured with a
  real administrator. A screen that works but takes six minutes has not passed.

## The sub-phases

| # | Sub-phase | Parent § | Depends on | Status |
|---|---|---|---|---|
| [4.0](4.0-contract-and-shell.md) | Additive contract v1.1.0 + admin shell | — (new) | Phase 2 freeze | ✔ Done |
| [4.1](4.1-auth-and-roles.md) | Role model, server-side enforcement, account lifecycle, auth UI | 4.1 | 4.0 | ✔ Done |
| [4.2](4.2-list-infrastructure.md) | Shared list contract + `DataTable` | — (new) | 4.0b | ✔ Done |
| [4.4](4.4-users-and-registration.md) | Users, **register borrower ★**, detail, CSV import, timed trial | 4.4 | 4.2, 4.1a | ✔ Done |
| [4.5](4.5-credentials.md) | Credential panel, destructive flows, blank stock, reader test | 4.5 | 4.4a | ✔ Done |
| [4.6](4.6-paper-backfill.md) | Paper backfill ★★ — entry bar, resolution, batch, conflicts, follow-through | 4.6 | 4.4b, 4.5c | ✔ Done |
| [4.7](4.7-dashboard.md) | Static panels, SSE live feed, attention strip | 4.2 | 4.5c, 4.6e | ✔ Done |
| [4.3](4.3-devices.md) | Device table, detail, mutations, CSV import | 4.3 | 4.2b, 4.5a | ✔ Done |
| [4.8](4.8-loans.md) | Loan tables, detail, overrides | 4.7 | 4.2b | ✔ Done |
| [4.9](4.9-reports-and-audit.md) | Reports API + UI, streaming CSV, audit viewer | 4.8, 4.9 | 4.2a | ✔ Done |
| [4.10](4.10-settings.md) | Categories and policy, kiosks, printing templates | 4.10 | 4.1a | ✔ Done |
| [4.11](4.11-quality-and-exit.md) | Accessibility, tests, performance, exit run | 4.11 | all above | ✔ Done |
| [4.12](4.12-kiosk-pairing.md) | Guided admin-to-kiosk pairing workflow | post-exit gap | 4.10b, Phase 3 pairing | Planned |

≈ 30.5 developer-days remaining after 4.0a.

**The parent's "~2 weeks" does not hold for one person.** It holds for three
people working the three tracks below, or it does not hold at all. Say so now
rather than discovering it in week two: solo, this phase is ~6 weeks. The
schedule lever is not "work faster on backfill" — it is deciding which of 4.3,
4.8, 4.9 and 4.10 slip past the pilot, all of which can, because none of them
block a kiosk or a card.

## Ordering

```
4.0a contract ✔
 └── 4.0b shell ─┬─ 4.1a roles ─┬─ 4.1b lifecycle ── 4.1c auth UI
                 │              │
                 │              └──────────────────────────────────┐
                 ├─ 4.2a list API ── 4.2b DataTable ─┬─ 4.4a ─ 4.4b ★ ─┬─ 4.4c ─ 4.4d ─ 4.4e
                 │                                   │                 │
                 │                                   │        4.5a ─ 4.5b ─ 4.5c ─ 4.5d
                 │                                   │                 │
                 │                                   │        4.6a ─ 4.6b ─ 4.6c ─ 4.6d ─ 4.6e ─ 4.6f ★★
                 │                                   │                 │
                 │                                   ├─ 4.3a ─ 4.3b ─ 4.3c        4.7a ─ 4.7b ─ 4.7c
                 │                                   ├─ 4.8a ─ 4.8b ─ 4.8c
                 │                                   └─ 4.9d audit viewer
                 ├─ 4.9a reports API ─ 4.9b ─ 4.9c CSV
                 └─ 4.10a settings ─ 4.10b kiosks ─ 4.10c templates
                                                              └── 4.11a/b/c/d exit
4.10b kiosk management ── 4.12a contract ── 4.12b admin workflow ── 4.12c browser E2E
```

Three tracks that barely touch after 4.2b:

- **Onboarding track (critical path)** — 4.2 → 4.4 → 4.5 → 4.6. One person, start
  to finish, with no context switches. This is the track that unblocks the pilot.
- **Platform track** — 4.1 (roles, lifecycle, auth UI), 4.9a/4.9c (reports and
  streaming export), 4.10a (settings store). Mostly backend, mostly independent,
  and 4.1a must land early because everything after it inherits the role check.
- **Inventory track** — 4.3 devices, 4.8 loans, 4.7 dashboard, 4.9b/4.9d viewers.
  Pure consumption of 4.2b's table. The most parallelisable and the most
  slippable.

**The unblock point for Phase 3's pilot is 4.4b + 4.5c.** An admin who can
register a borrower and bind a blank card has everything the kiosk needs.
Everything after 4.7 can slip without stalling hardware validation.

## Deviations from the parent plan

- **4.0 is new**, and half of it is already spent. The parent lists no contract
  work, but 32 of Phase 4's operations did not exist in the frozen v1.0.0 spec.
  They were added in one deliberate additive pass rather than amended in
  piecemeal per feature, because a kiosk in the field may lag a deploy and every
  ad-hoc spec edit is a chance to break it. See [4.0](4.0-contract-and-shell.md).
- **4.2 is new.** The parent's 4.11 asks for URL-bound filters, keyboard
  navigation and designed empty states on *every* list view, and its exit
  criteria require a 5 000-row overdue list under one second. Both are one piece
  of work built once, not eleven pieces built per screen. Pulled forward ahead of
  every table that consumes it.
- **The order is re-sequenced, not the content.** 4.4 → 4.5 → 4.6 run before 4.3,
  4.7, 4.8. The parent's numbering is a table of contents; this is a build order.
  Numbering is otherwise preserved exactly — including the letters already
  hard-coded in `internal/apiserver/phase4_stubs.go`, so a 501 response still
  names the task that fills it in.
- **Registration and backfill get their own timed sub-tasks** (4.4e, 4.6f) rather
  than being folded into 4.11's exit run. Both are measured with a real
  administrator, both can fail, and both fail late if nobody owns the stopwatch.
- **There is no 4.x for "dashboard charts".** Explicitly deferred by
  [08](../../08-admin-console.md); CSV export covers analysis. If a chart appears
  in a pull request, it is scope creep, not a bonus.

## Conventions every sub-phase follows

**The contract is frozen and additive-only.** v1.1.0 added paths and optional
fields; nothing was removed or narrowed. Any further spec change in this phase
follows the same rule and bumps the minor version. Never make an existing field
required, never narrow an enum — a kiosk running last week's build must keep
working.

**Deleting a stub is how a task starts.** Every unimplemented Phase 4 operation
answers 501 with `"task": "4.x"` in the problem extensions
(`internal/apiserver/phase4_stubs.go`). The file is empty when the phase is done.
A screen wired to a stub fails loudly rather than rendering a plausible empty
table.

**Roles are enforced at the API, never in the UI.** The UI hides what a role
cannot do as a courtesy. The control is `requireRole` on the handler, and the
proof is the role × operation matrix test in [4.1](4.1-auth-and-roles.md). A
review question for every new endpoint: which role, and where is it in the
matrix?

**Every list view drives its filters from URL search params.** A pasted link is a
real workflow ("here is the overdue list"). This is not a 4.11 cleanup item; it
is a property of [4.2b](4.2-list-infrastructure.md)'s `DataTable`, so screens get
it for free and cannot forget it.

**Memoize TanStack Table inputs.** Unmemoized `columns` or `data` produce a
silent 100 % CPU render loop with no console error and no test failure. This has
already cost this project once. `DataTable` owns the memo boundary so individual
screens cannot reintroduce it.

**Loading, empty and error states are designed, not defaulted.** "An empty table
with no explanation is a support call." The three states are primitives from
[4.0b](4.0-contract-and-shell.md); a screen that renders a bare `<div />` while
loading is unfinished.

**Optimistic updates only where safe.** Anything touching loans or credentials
waits for the server. A wrongly-optimistic custody record is worse than a slow
one.

**Reasons are mandatory and audited.** Status change, suspend, revoke, reissue,
force return, write off, correct attribution — each takes a typed reason, each
emits an audit event with the acting admin. "Why does Dr. Sharma have three
revoked cards" must have an answer six months later.

**Migrations.** goose, embedded, in order. Phase 4 claims `0012`–`0016`:

| File | Sub-phase |
|---|---|
| `migrations/0012_admin_roles.sql` | 4.1a |
| `migrations/0013_auth_lifecycle.sql` | 4.1b |
| `migrations/0014_list_indexes.sql` | 4.2a |
| `migrations/0015_import_batches.sql` | 4.4d |
| `migrations/0016_settings.sql` | 4.10a |

Every migration has a working `-- +goose Down`; `test/integration/migrations_test.go`
must stay green.

**sqlc.** Write `.sql` files under `queries/<module>/`; never hand-edit
`internal/**/store/*.sql.go`. New query directories need a `sqlc.yaml` entry
*before* the first query, not after.

**Module boundaries.** Unchanged from Phase 2: `apiserver` composes modules,
modules receive IDs rather than entities, and only `checkout` may import a
sibling's `…api` package. Reports and CSV export are read-only projections and
belong in the module that owns the data, not in a new "reporting" module that
reaches across every table.

**No secrets in the DOM.** Credential tokens are never logged, never placed in a
URL, and appear in the UI only inside the deliberate reveal/print surfaces built
in [4.5](4.5-credentials.md).

**The `frontend-design` and `shadcn` skills** are used when building screens, so
the visual language is deliberate rather than default-shaped. Dark mode is out of
scope for v1. Mobile-responsive admin is out of scope for v1 — the admin works at
a desk.

## Definition of done — applies to every sub-phase

- [x] `task lint` green, including the depguard boundary rules
- [x] `task test:backend` and `task test:integration` green under `-race`
- [x] `pnpm -r lint`, `pnpm -r test`, `pnpm -r build` green
- [x] `task generate` produces no diff (spec and generated code committed together)
- [x] Every endpoint the sub-phase adds appears in the role matrix test and in the
      kiosk-scope classification — both suites fail on an unclassified operation
- [x] Every new invariant or rule has a test named after it
- [x] axe reports no violations on any screen the sub-phase touched
- [x] Loading, empty and error states exist for every view the sub-phase adds
- [x] The sub-phase's own exit criteria, in its own file, are checked off
- [x] `docs/` updated if the implementation deviated from the design docs — the
      docs are the contract for Phases 5–6, so drift is a defect

## Gaps in the tree this breakdown found

Recorded here because each one is a task in a sub-phase rather than a surprise:

1. **Roles do not exist in code.** The DB enum is
   `admin_role AS ENUM ('superadmin','admin','operator')` (`migrations/0001_init.sql`)
   while the parent and [08](../../08-admin-console.md) specify
   `admin`/`technician`/`viewer`. `auth.HasRoleAtLeast` exists and **nothing
   calls it** — no handler reads the role today. Closed in
   [4.1a](4.1-auth-and-roles.md).
2. **No settings store of any kind.** 4.10's policy, thresholds and templates
   have nowhere to live; `label-settings.ts` keeps label geometry in the
   browser's local storage, which does not survive a different admin PC. Closed
   in [4.10a](4.10-settings.md).
3. **No audit query API.** The `audit` module writes events and its store has
   `ListAuditEvents`, but nothing exposed it until v1.1.0 added `/audit`. Closed
   in [4.9d](4.9-reports-and-audit.md).
4. **Missing auth lifecycle:** no recovery codes, no lockout, no password reset,
   no forced TOTP re-enrolment. Closed in [4.1b](4.1-auth-and-roles.md).
5. **The admin shell has five nav items and no error boundary.**
   `components/app-shell.tsx` covers devices, users, register, credentials,
   labels; Phase 4 needs roughly twice that, plus role-aware rendering and a
   toast surface. Closed in [4.0b](4.0-contract-and-shell.md).
6. **`Dashboard` is thin** relative to what 4.2 of the parent asks for — counts,
   `availabilityByCategory`, `turnedAwayCounts`, `lastPaperEntry`. The overdue
   list, kiosk strip and unbound count are separate endpoints today. v1.1.0 added
   optional fields; [4.7](4.7-dashboard.md) decides per panel whether to compose
   client-side or fill the optional field.
7. **No import provenance.** `users.registered_by` accepts the literal `'import'`
   but nothing records *which* import, so 4.4c's provenance requirement has no
   source. Closed in [4.4d](4.4-users-and-registration.md) with an import-batch
   table.
8. **`packages/ui` exports only `cn` and a token sheet.** Deliberately left that
   way in Phase 3 — the kiosk's button is 64 px tall and the admin's is not.
   Phase 4 builds admin components in `apps/admin`, and promotes to
   `packages/ui` only what the kiosk actually shares. Do not "fix" this by
   merging the two button components.
</content>
</invoke>
