# Phase 6 — sub-phase breakdown

The parent plan, [phase-6-extensibility.md](../phase-6-extensibility.md), states
*what* the hospital may ask for next. This folder states *how each item is built,
in what order within itself, and how it is proven done* — one file per sub-phase,
each holding the lettered units that are the actual branch-and-commit slices
(`feat(phase-6.1c): ...`).

Phase 6 is not shaped like the five phases before it, and the breakdown should
not pretend otherwise:

- **There is no build order, because there is no dependency chain worth
  honouring.** The order is demand. Nine of these ten sub-phases are islands;
  the couplings that do exist are listed below and there are only five of them.
  What replaces a build order is an **intake gate** ([6.0](6.0-intake-and-extension-rules.md)):
  the rules every item obeys on the way in, so that "quick" does not become
  "quick and then unowned for three years".
- **Every item lands on a system people are using that morning.** Phases 0–5
  built toward a go-live; Phase 6 ships past one. That single fact produces most
  of 6.0: additive migrations, feature flags defaulted off, no breaking contract
  change, and no deploy during a freeze window.
- **This is the first phase that extends a frozen API contract.** Phase 5 added
  no endpoints by design. Phase 6 adds them, and the rules for doing that
  additively are written down once, in 6.0, rather than re-decided per feature.
- **Only 6.0 and 6.1 are scheduled.** Everything else here is a plan waiting for
  a request. Writing the plan is cheap; building unrequested features is not.

## The sub-phases

| # | Sub-phase | Parent § | Depends on | Est. | Schedule it when |
|---|---|---|---|---|---|
| [6.0](6.0-intake-and-extension-rules.md) | Intake gate, additive-change rules, flag pattern, live-system discipline | — (new) | Phase 5 go | 1 d | Before the first Phase 6 item — once |
| [6.1](6.1-rfid-staff-cards.md) | **RFID/NFC staff ID cards ★** — viability test, UID resolve path, enrolment, staged rollout | 6.1 | 6.0, Q9 | 3.75 d + lead time | Now. It is already asked for |
| [6.2](6.2-notifications.md) | Overdue producer, notification module, SMTP, preferences, quiet hours | 6.2 | 6.0, 5.5e | 6 d | Administrators are chasing overdues by hand |
| [6.3](6.3-directory-integration.md) | OIDC admin login, roster sync, leaver suspension, optional auto-registration | 6.3 | 6.0, 6.1 if both | 8 d | Roster maintenance becomes the complaint |
| [6.4](6.4-reservations.md) | `reserved` state, future windows, kiosk conflict handling, no-show expiry | 6.4 | 6.0, 6.6 & 6.9a if planned | 10 d | Contention is measured, not assumed |
| [6.5](6.5-maintenance-lifecycle.md) | Service schedules, service history, condition trend, warranty, retirement | 6.5 | 6.0 | 6 d | Devices start failing in service |
| [6.6](6.6-multi-location.md) | Locations, homing, kiosk binding, transfers, scoped dashboards | 6.6 | 6.0 | 5 d | A second ward or site is committed to |
| [6.7](6.7-consumables.md) | New `inventory` module: quantity-tracked stock, issue without return | 6.7 | 6.0 | 8 d | Cables and batteries are being tracked on paper anyway |
| [6.8](6.8-analytics.md) | Utilisation, loss/damage rates, turnaround, procurement evidence | 6.8 | 6.0, 6.6 if planned | 5 d | Someone asks a question the reports cannot answer |
| [6.9](6.9-second-language.md) | String extraction (**not done**), catalogue, kiosk toggle, translation | 6.9 | 6.0 | 4 d | A second language is actually needed on the counter |

≈ 57 developer-days if every item were built, which is exactly the outcome the
parent plan tells you to resist. The realistic first year is 6.0 + 6.1, then one
or two of 6.2/6.3 on evidence — about three weeks of work, not eleven.

## Ordering

Islands, not a chain. Only these couplings are real:

```
6.0 intake gate ── gates everything below

  6.1a viability ★ ─ 6.1b resolve path ─ 6.1c enrolment ─ 6.1d rollout ─ 6.1e retire QR
        (a gates b–e absolutely: a failed UID test ends 6.1)

  5.5e job runner ─ 6.2a overdue producer ─ 6.2b delivery ─ 6.2c templates ─ 6.2d prefs ─ 6.2e remind button

  6.1 ─ 6.3a OIDC ─ 6.3b sync ─ 6.3c leavers ─ 6.3d auto-register        (identity churn: do 6.1 first)

  6.6 locations ─ 6.4 reservations        (a reservation in a two-site world is location-scoped)
  6.6 locations ─ 6.8 analytics           (location is the dimension the questions will use)
  6.9a extraction ─ any new kiosk screen  (6.4d, 6.7 — or you extract the same strings twice)

  6.5, 6.7 — standalone
```

Read the couplings as *if both are planned, this order*, never as *this must come
first*. Nothing here blocks anything if the other item is never requested.

**6.1 is the only item with a hard external gate.** [6.1a](6.1-rfid-staff-cards.md)
is a one-hour test with one reader that determines whether 6.1 is possible at
all. Run it before any procurement, any planning and any promise to the hospital.

## Deviations from the parent plan

- **6.0 is new.** The parent opens with "nothing here is required for go-live",
  which is true and is not a process. Feature flags, additive-contract rules,
  migration numbering after `0018`, freeze windows and a removal path for
  anything that does not get used are collected there.
- **6.1's "expected code change: none" is wrong, and 6.1 is planned as if it is.**
  Three specific places in the tree reject or mis-handle an RFID credential today
  (gaps 1–3 below). It is still the cheapest item on the list — roughly a day of
  code rather than none — and it is still first.
- **6.9's "translation work plus a toggle" is wrong for the same reason.** The
  kiosk's copy is hardcoded in JSX; the extraction that the parent assumes
  happened in Phase 3 did not. [6.9a](6.9-second-language.md) does it.
- **6.2 owns the overdue producer.** The parent lists "scheduled overdue scan" as
  one bullet among six. Nothing in the tree publishes `loan.overdue` and no
  Phase 5 job creates it, so it is the first slice of 6.2 and everything else in
  the sub-phase depends on it.
- **The prioritisation list becomes schedule triggers.** The parent's ranking is
  sound; a ranking with no trigger still gets built speculatively. Each row above
  names the observation that justifies starting.

## Conventions every sub-phase follows

**Ships behind a flag, default off, on for one kiosk first.** Every sub-phase
lands dark, is enabled for one counter or one administrator, and widens only
after a working day of use. This is the whole difference between Phase 6 and the
phases before it: there is no pilot window to hide in.

**Additive only — schema and contract.** New tables and nullable columns; new
endpoints and optional fields. No removed field, no narrowed enum, no changed
response shape while a kiosk that has not reloaded is still running the old
bundle. The rules and their tests are in [6.0b](6.0-intake-and-extension-rules.md).

**Migrations claim numbers in ship order from `0019`.** Phase 5 claimed `0017`
and `0018`. Because Phase 6's order is demand rather than plan, no number is
reserved in advance: the item being scheduled takes the next free one and records
it in its own file and in the table below. Every migration needs a working
`-- +goose Down` and `test/integration/migrations_test.go` green.

**ADRs continue from `0010`** (Phase 5 takes `0009` for hosting), also in ship
order — the numbers named inside the sub-phase files are indicative, and the real
one is claimed when the item is scheduled. Every sub-phase here that changes a
boundary, a state machine or an identity source writes one — those are precisely the decisions a future reader
will not be able to reconstruct from the diff.

**A new module means new depguard rules.** `internal/modules/<x>/internal/...`
must be unreachable, and the rule needs both project quirks right: `files`
patterns take a leading `**/`, and a bare `pkg:` entry denies the whole subtree,
so anchor exact matches with a trailing `$`. A boundary that lint does not
enforce is a comment.

**Kiosk work must survive the offline queue.** Phase 5.1 decided what queues and
what refuses. Any new kiosk action either has an answer for "what happens when
this is queued for forty minutes" or it is not offered offline at all.

**Every new operational surface gets a runbook page in the same commit.** Phase
5.7's fourteen pages are the baseline; a Phase 6 job, alert or integration that
fails at 08:30 needs its page to already exist.

**Every sub-phase declares how it will be known to be worth its maintenance.**
An exit criterion four weeks after ship: a usage number, and the removal path if
the number is zero. Unused features are not free — they are tested, secured and
carried for years.

### Migration ledger

Filled in as items are scheduled, not before.

| File | Sub-phase | Claimed |
|---|---|---|
| `0021_notifications.sql` | 6.2 | 2026-09-18 |
| _(next free: `0022`)_ | — | — |

## Gaps in the tree this breakdown found

Verified against the code, not the docs. Each is a task in a sub-phase rather
than a surprise on the day.

1. **RFID credentials cannot be issued at all.**
   `mintToken` (`hdms-backend/internal/modules/credentials/module.go:155`)
   generates a token for `qr`/`code128`, accepts a caller value for `manual`, and
   returns `ErrKindNotIssuableInV1` for everything else — which is `nfc` and
   `rfid`. Closed in [6.1c](6.1-rfid-staff-cards.md).
2. **A scanned UID will not resolve against an enrolled one unless the formats
   match by luck.** `NormalizeUID`
   (`internal/modules/credentials/internal/domain/credential.go:82`) exists and is
   tested, but `canonicalToken` (`module.go:219`) never calls it: it parses an
   HDMS token or trims whitespace. A reader typing `04:A2:B3` cannot match a
   stored `04A2B3`, and the failure is silent — "unknown card". Closed in
   [6.1b](6.1-rfid-staff-cards.md).
3. **The contract has no field for a card-supplied token.**
   `IssueCredentialRequest.manualToken` (`api/openapi.yaml:2331`) is documented as
   "used verbatim, only when kind is manual". Enrolling a tapped card needs an
   additive contract change — the first one since the freeze. Closed in
   [6.1c](6.1-rfid-staff-cards.md) under [6.0b](6.0-intake-and-extension-rules.md)'s rules.
4. **`enabled_sources` is an unvalidated `text[]`** (`migrations/0001_init.sql:48`,
   default `{scanner,camera}`); nothing rejects a typo, so `rfid` enabled as
   `RFID` fails silently at the kiosk. Closed in [6.1d](6.1-rfid-staff-cards.md).
5. **Nothing publishes `loan.overdue`.** The topic is declared
   (`internal/platform/events/events.go:32`), the hub routes it (`hub.go:41`) and
   audit subscribes to it (`internal/modules/audit/subscriptions.go:16`) — there is
   no producer anywhere. Closed in [6.2a](6.2-notifications.md).
6. **The `notification` module is two comment-only files.** `module.go` and
   `notificationapi/notification.go` declare the boundary and nothing else. The
   package exists so the boundary was proven in Phase 0; the service is entirely
   new code. Closed in [6.2b](6.2-notifications.md).
7. **Kiosk copy is hardcoded in JSX** — e.g.
   `apps/kiosk/src/screens/awaiting-device-screen.tsx:141`, and its test asserts
   the literal string. There is no string catalogue, no `i18n` dependency and no
   locale plumbing anywhere in the frontend. Closed in [6.9a](6.9-second-language.md).
8. **`device_status` has no `reserved` value**
   (`migrations/0003_catalog.sql:12`), and the custody exclusion constraint
   (`migrations/0007_lending.sql:64`) covers loans only — a reservation is a
   future claim on a device with no row to exclude against. This is why
   [6.4](6.4-reservations.md) is ten days and not two.
9. **`device_condition` already exists** (`good`/`fair`/`damaged`,
   `migrations/0003_catalog.sql:15`) and is a point-in-time value with no history.
   [6.5c](6.5-maintenance-lifecycle.md) adds the trend rather than the concept.
