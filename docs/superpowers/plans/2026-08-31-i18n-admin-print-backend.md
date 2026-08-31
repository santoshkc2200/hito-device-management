# i18n Release Two — Admin Console, Print and Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the admin console, printed material, and everything the backend emits into Japanese and English, using the engine release one built.

**Architecture:** The admin console reuses `@hdms/i18n` with its own catalogue, resolving locale from the signed-in administrator's stored preference rather than a device setting. Print templates take the locale of the console that rendered them. The backend gains a `golang.org/x/text` catalogue used only where no browser exists — the CLI today, notification emails when 6.2 is built — and a lint rule that stops a translated string ever entering an API response.

**Tech Stack:** TypeScript 6.0, React 19, Vite 8, Vitest 4, TanStack Table 8, react-hook-form 7, zod 4, sonner 2, bwip-js 4, Go 1.26, `golang.org/x/text`, golangci-lint v2 with depguard.

**Spec:** [`docs/superpowers/specs/2026-08-31-i18n-l10n-design.md`](../specs/2026-08-31-i18n-l10n-design.md)

**Depends on:** [`2026-08-31-i18n-kiosk.md`](2026-08-31-i18n-kiosk.md) Tasks 2, 3 and 4 — the engine, the provider, and the `locale` columns. Do not start this plan until those are merged.

## Global Constraints

- Supported locales are exactly `"ja"` and `"en"`. `DEFAULT_LOCALE` is `"ja"`.
- The Japanese catalogue is the type reference; English is `const en: AdminMessages`, so a missing key is a compile error.
- Keys are named for meaning, never for the current English wording.
- **Extraction commits are mechanical.** No wording changes in a commit that moves a string.
- No API response carries a translated string. The backend translates only where no client exists to do it.
- `admin_accounts.locale` already exists (migration `0017`, release one Task 4). This plan adds the endpoint and the UI, not the column.
- TanStack Table column definitions must stay memoized. Unmemoized `columns` or `data` produce a silent 100% CPU render loop with no console error — a translator in a column definition is exactly the change that breaks this if done carelessly.
- Frontend commands run from `hdms-frontend/`; backend commands from `hdms-backend/`.

---

### Task 1: The admin locale endpoint

**Files:**
- Modify: `hdms-backend/api/openapi.yaml` — add `PATCH /auth/me/locale`
- Modify: `hdms-backend/internal/apiserver/` — the file that owns `/auth/me`
- Modify: `hdms-backend/queries/auth/` — the admin account update query
- Test: `hdms-backend/test/integration/` — alongside the existing auth tests

**Interfaces:**
- Consumes: `admin_accounts.locale` from release one Task 4.
- Produces: `updateMyLocale` in the generated TS client, taking `{ locale: "ja" | "en" }` and returning the updated `Admin`. Task 6 calls it.

- [ ] **Step 1: Extend the contract**

In `hdms-backend/api/openapi.yaml`, add under `paths`, next to the existing `/auth/me`:

```yaml
  /auth/me/locale:
    patch:
      operationId: updateMyLocale
      summary: Set the signed-in administrator's console language.
      tags: [auth]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/UpdateLocaleRequest"
      responses:
        "200":
          description: OK.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Admin"
        "400":
          description: Unsupported locale.
          content:
            application/problem+json:
              schema:
                $ref: "#/components/schemas/Problem"
```

and under `components.schemas`:

```yaml
    UpdateLocaleRequest:
      type: object
      required: [locale]
      properties:
        locale:
          type: string
          enum: [ja, en]
```

A dedicated endpoint rather than a field on the existing admin update: changing your own display language is not an administrative act on an account, and it must not require the role that editing an account requires.

- [ ] **Step 2: Write the failing integration test**

Add to the auth integration test file in `hdms-backend/test/integration/`:

```go
func TestAdminCanSetTheirOwnLocale(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	session := env.loginAsOperator(t)

	require.Equal(t, "ja", env.getMe(t, session).Locale, "a new account defaults to Japanese")

	updated := env.updateMyLocale(t, session, "en")
	require.Equal(t, "en", updated.Locale)
	require.Equal(t, "en", env.getMe(t, session).Locale)
}

func TestAdminLocaleRejectsAnUnsupportedValue(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	session := env.loginAsOperator(t)

	resp := env.updateMyLocaleRaw(t, session, "de")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestAdminLocaleIsScopedToTheCallersOwnAccount(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	operator := env.loginAsOperator(t)
	other := env.createAdmin(t, "other@example.test")

	env.updateMyLocale(t, operator, "en")
	require.Equal(t, "ja", env.getAdmin(t, other.Id).Locale,
		"setting my language must not touch anyone else's")
}
```

Use the helper names the existing auth integration tests already use.

- [ ] **Step 3: Run to verify it fails**

Run: `cd hdms-backend && go test -race -tags=integration ./test/... -run TestAdminLocale`
Expected: FAIL — the operation does not exist.

- [ ] **Step 4: Generate and implement**

```bash
cd /Users/hitohospital/personal/dev/hito-device-management
task generate
```

Implement the handler beside the existing `/auth/me` handler. It reads the admin ID from the session context — never from the request body — validates the locale against `{"ja", "en"}`, returns `400` with a `validation-failed` problem otherwise, updates the row, and returns the refreshed `Admin`.

- [ ] **Step 5: Run the tests**

```bash
cd hdms-backend
go test ./...
go test -race -tags=integration ./test/...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add hdms-backend hdms-frontend/packages/api-client/src/gen
git commit -m "feat(phase-6.9g): add PATCH /auth/me/locale

A separate endpoint from account editing on purpose: changing your own display
language is not an administrative act and must not need the role that editing
an account needs."
```

---

### Task 2: Admin catalogue scaffold and provider

**Files:**
- Create: `hdms-frontend/apps/admin/src/i18n/ja.ts`
- Create: `hdms-frontend/apps/admin/src/i18n/en.ts`
- Create: `hdms-frontend/apps/admin/src/i18n/index.ts`
- Modify: `hdms-frontend/apps/admin/src/routes/root.tsx`
- Modify: `hdms-frontend/apps/admin/package.json` — add `"@hdms/i18n": "workspace:*"`

**Interfaces:**
- Consumes: `LocaleProvider`, `useTranslator`, `Catalogues` from `@hdms/i18n`; `Admin.locale` from release one Task 4.
- Produces: `type AdminMessages = typeof ja`, `const catalogues`, `function useT()`. Every later task in this plan uses `useT`.

- [ ] **Step 1: Create the catalogue files**

Same shape as the kiosk's. `hdms-frontend/apps/admin/src/i18n/ja.ts` starts holding English values — untranslated — so the types hold and nothing rendered changes until Task 6:

```ts
/**
 * The admin console's Japanese catalogue, and the type reference for every
 * other locale. Values start as English and are translated in one pass, so the
 * extraction commits stay mechanical and reviewable.
 */
export const ja = {
  nav: {
    dashboard: "Dashboard",
    devices: "Devices",
    users: "Users",
    loans: "Loans",
    credentials: "Credentials",
    reports: "Reports",
    audit: "Audit",
    settings: "Settings",
  },
  common: {
    save: "Save",
    cancel: "Cancel",
    delete: "Delete",
    edit: "Edit",
    search: "Search",
    loading: "Loading…",
    noResults: "No results",
  },
} as const;

export type AdminMessages = typeof ja;
```

`hdms-frontend/apps/admin/src/i18n/en.ts` mirrors it as `export const en: AdminMessages = { … }` with identical values.

`hdms-frontend/apps/admin/src/i18n/index.ts`:

```ts
import { useTranslator, type Catalogues } from "@hdms/i18n";
import { ja, type AdminMessages } from "./ja";
import { en } from "./en";

export const catalogues: Catalogues<AdminMessages> = { ja, en };

export type { AdminMessages };

export function useT() {
  return useTranslator(catalogues);
}
```

- [ ] **Step 2: Write the failing provider test**

`hdms-frontend/apps/admin/src/__tests__/locale-provider.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderAdminApp, mockSignedInAdmin } from "@/test/helpers";

describe("admin locale", () => {
  it("takes the signed-in administrator's stored preference", async () => {
    mockSignedInAdmin({ locale: "en" });
    renderAdminApp();
    expect(await screen.findByText("Dashboard")).toBeInTheDocument();
    expect(document.documentElement.lang).toBe("en");
  });

  it("defaults to Japanese when the account has no preference", async () => {
    mockSignedInAdmin({});
    renderAdminApp();
    expect(document.documentElement.lang).toBe("ja");
  });
});
```

Use the existing helpers in `hdms-frontend/apps/admin/src/test/`; if `mockSignedInAdmin` and `renderAdminApp` do not exist under those names, use the equivalents that do rather than adding a parallel set.

- [ ] **Step 3: Run to verify it fails**

Run: `cd hdms-frontend && pnpm --filter admin test -- locale-provider`
Expected: FAIL — `document.documentElement.lang` is unset.

- [ ] **Step 4: Mount the provider**

Add the dependency, run `pnpm install`, then in `hdms-frontend/apps/admin/src/routes/root.tsx` wrap the outlet:

```tsx
import { LocaleProvider, isLocale, DEFAULT_LOCALE } from "@hdms/i18n";
import { useCurrentAdmin } from "@/lib/auth";

export const rootRoute = createRootRoute({
  component: () => {
    const admin = useCurrentAdmin();
    const locale = isLocale(admin?.locale) ? admin.locale : DEFAULT_LOCALE;
    return (
      <LocaleProvider locale={locale}>
        <Outlet />
      </LocaleProvider>
    );
  },
});
```

Use whatever hook the admin app already exposes for the signed-in account; do not add a second source of truth for it.

- [ ] **Step 5: Run and commit**

Run: `cd hdms-frontend && pnpm --filter admin test`
Expected: PASS — no copy has changed yet.

```bash
git add hdms-frontend/apps/admin hdms-frontend/pnpm-lock.yaml
git commit -m "feat(phase-6.9g): mount the locale provider in the admin console

Locale comes from the signed-in administrator's account, not the device — an
admin console is used by a person, unlike a shared kiosk. Nothing reads the
catalogue yet."
```

---

### Task 3: Extract the shell, navigation and dashboard

**Files:**
- Modify: `hdms-frontend/apps/admin/src/routes/root.tsx`, `index.tsx`, `dashboard.tsx`, `authenticated.tsx`, `login.tsx`
- Modify: `hdms-frontend/apps/admin/src/components/dashboard/*.tsx`
- Modify: `hdms-frontend/apps/admin/src/components/states/*.tsx`
- Modify: the matching tests, in the same commits

**Interfaces:**
- Consumes: `useT` (Task 2).
- Produces: catalogue sections `nav`, `dashboard`, `login`, `states`.

- [ ] **Step 1: Extract, one file at a time**

For each file: add keys to `ja.ts` and `en.ts` with identical English values, replace the literal with `t("…")`, update that file's test to assert through the catalogue, run that file's tests, commit.

Empty and error states first — `components/states/` is small, shared by every table in the app, and gets the mechanics settled before the large route files:

```tsx
import { useT } from "@/i18n";

export function EmptyState({ entity }: { entity: string }) {
  const t = useT();
  return <p className="text-muted-foreground">{t("states.empty", { entity })}</p>;
}
```

- [ ] **Step 2: Run after each file**

Run: `cd hdms-frontend && pnpm --filter admin test -- <file-stem>`
Expected: PASS with no change to rendered output.

- [ ] **Step 3: Commit per file group**

```bash
git commit -m "refactor(phase-6.9g): extract admin shell and dashboard copy

Mechanical move only — no wording changes."
```

---

### Task 4: Extract the tables, keeping columns memoized

The route files are the bulk of the console: `devices.tsx`, `users.tsx`, `loans.tsx`, `credentials.tsx`, `audit.tsx`, `reports.tsx`, `disputed.tsx`, `backfill.tsx`, and the three detail routes. Their TanStack Table column definitions carry header strings, and this is where a careless extraction breaks the app.

**Files:**
- Modify: every route file listed above
- Modify: `hdms-frontend/apps/admin/src/components/data-table/*.tsx`
- Modify: the matching tests

**Interfaces:**
- Consumes: `useT` (Task 2), `collator` from `@hdms/i18n`.
- Produces: catalogue sections `devices`, `users`, `loans`, `credentials`, `audit`, `reports`, `disputed`, `backfill`, `columns`.

- [ ] **Step 1: Write the regression test that protects the render loop**

`hdms-frontend/apps/admin/src/components/data-table/__tests__/stable-columns.test.tsx`:

```tsx
import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LocaleProvider } from "@hdms/i18n";
import { useDeviceColumns } from "@/routes/devices";

describe("column definitions", () => {
  it("returns the same array reference across renders in a fixed locale", () => {
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <LocaleProvider locale="en">{children}</LocaleProvider>
    );
    const { result, rerender } = renderHook(() => useDeviceColumns(), { wrapper });
    const first = result.current;
    rerender();
    // An unmemoized column array is a new reference every render, which sends
    // TanStack Table v8 into a silent 100% CPU loop with nothing in the console.
    expect(result.current).toBe(first);
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd hdms-frontend && pnpm --filter admin test -- stable-columns`
Expected: FAIL — `useDeviceColumns` does not exist yet.

- [ ] **Step 3: Extract headers behind a memoized hook**

In each route file, lift the column definitions into a hook and memoize on the translator:

```tsx
import * as React from "react";
import { useT } from "@/i18n";

export function useDeviceColumns() {
  const t = useT();
  return React.useMemo<ColumnDef<Device>[]>(
    () => [
      { accessorKey: "assetTag", header: t("columns.assetTag") },
      { accessorKey: "name", header: t("columns.name") },
      { accessorKey: "status", header: t("columns.status") },
      // …the rest, unchanged apart from the header strings…
    ],
    [t]
  );
}
```

`useTranslator` already returns a `useCallback`-stable function that changes only when the locale changes, so `[t]` is the correct and sufficient dependency: the columns are rebuilt on a language switch and at no other time.

- [ ] **Step 4: Sort Japanese text with a collator**

Wherever a table sorts a text column by comparison, swap the comparator:

```tsx
import { collator, useLocale } from "@hdms/i18n";

  const { locale } = useLocale();
  const sortText = React.useMemo(() => collator(locale).compare, [locale]);
  // …then in the column def: sortingFn: (a, b, id) =>
  //   sortText(String(a.getValue(id)), String(b.getValue(id)))
```

Add a test asserting `["さとう", "あおき", "たなか"]` sorts to `["あおき", "さとう", "たなか"]` through the table, not just through the collator — the point is that the table uses it.

- [ ] **Step 5: Run the suite**

Run: `cd hdms-frontend && pnpm --filter admin test`
Expected: PASS.

Watch for a test that hangs rather than fails. That is the render loop, and it means a column array escaped memoization.

- [ ] **Step 6: Commit per route**

```bash
git commit -m "refactor(phase-6.9g): extract device table copy behind a memoized hook

Column definitions move into a useMemo keyed on the translator so a language
switch rebuilds them and nothing else does."
```

---

### Task 5: Localize form validation

zod messages are written at schema-definition time, outside React, so `useT` is not available where they are declared. Localize at the resolver boundary instead of threading a translator through every schema.

**Files:**
- Create: `hdms-frontend/apps/admin/src/lib/localized-resolver.ts`
- Create: `hdms-frontend/apps/admin/src/lib/localized-resolver.test.ts`
- Modify: every file calling `zodResolver` — `components/settings/policy-panel.tsx`, `kiosks-panel.tsx`, `templates-panel.tsx`, and the dialogs and detail routes that use react-hook-form

**Interfaces:**
- Consumes: `useT` (Task 2).
- Produces: `function useLocalizedResolver<T>(schema: z.ZodType<T>): Resolver<T>`.

- [ ] **Step 1: Write the failing test**

`hdms-frontend/apps/admin/src/lib/localized-resolver.test.ts`:

```ts
import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { z } from "zod";
import { LocaleProvider } from "@hdms/i18n";
import { useLocalizedResolver } from "./localized-resolver";

const schema = z.object({ name: z.string().min(1, "validation.required") });

describe("useLocalizedResolver", () => {
  it("resolves a message key through the catalogue", async () => {
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <LocaleProvider locale="en">{children}</LocaleProvider>
    );
    const { result } = renderHook(() => useLocalizedResolver(schema), { wrapper });
    const outcome = await result.current({ name: "" }, undefined, {
      fields: {},
      shouldUseNativeValidation: false,
    });
    expect(outcome.errors.name?.message).toBe("This field is required");
  });

  it("passes a message through unchanged when it is not a catalogue key", async () => {
    const plain = z.object({ name: z.string().min(1, "Name is required") });
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <LocaleProvider locale="en">{children}</LocaleProvider>
    );
    const { result } = renderHook(() => useLocalizedResolver(plain), { wrapper });
    const outcome = await result.current({ name: "" }, undefined, {
      fields: {},
      shouldUseNativeValidation: false,
    });
    expect(outcome.errors.name?.message).toBe("Name is required");
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd hdms-frontend && pnpm --filter admin test -- localized-resolver`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement**

`hdms-frontend/apps/admin/src/lib/localized-resolver.ts`:

```ts
import * as React from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import type { Resolver } from "react-hook-form";
import type { z } from "zod";
import { useT } from "@/i18n";
import { catalogues } from "@/i18n";

/**
 * zod schemas are declared outside React, so they carry catalogue keys as
 * their messages and the translation happens here, at the resolver boundary.
 * A message that is not a known key is passed through unchanged, so a schema
 * can still carry a literal where a key would be overkill.
 */
export function useLocalizedResolver<T extends z.ZodTypeAny>(
  schema: T
): Resolver<z.infer<T>> {
  const t = useT();
  return React.useMemo(() => {
    const base = zodResolver(schema);
    return async (values, context, options) => {
      const result = await base(values, context, options);
      for (const error of Object.values(result.errors ?? {})) {
        const message = (error as { message?: string })?.message;
        if (message && isCatalogueKey(message)) {
          (error as { message?: string }).message = t(message as never);
        }
      }
      return result;
    };
  }, [schema, t]);
}

function isCatalogueKey(candidate: string): boolean {
  let node: unknown = catalogues.ja;
  for (const segment of candidate.split(".")) {
    if (typeof node !== "object" || node === null) return false;
    node = (node as Record<string, unknown>)[segment];
  }
  return typeof node === "string";
}
```

- [ ] **Step 4: Convert the schemas**

Replace each literal message with a key and add the key to both catalogues:

```ts
const kioskFormSchema = z.object({
  name: z.string().min(1, "validation.nameRequired"),
});
```

```ts
  validation: {
    nameRequired: "Name is required",
    // …
  },
```

and swap `resolver: zodResolver(schema)` for `resolver: useLocalizedResolver(schema)` at each call site.

- [ ] **Step 5: Run and commit**

Run: `cd hdms-frontend && pnpm --filter admin test`
Expected: PASS.

```bash
git add hdms-frontend/apps/admin/src
git commit -m "refactor(phase-6.9g): localize zod messages at the resolver boundary

Schemas are declared outside React, so they carry catalogue keys and the
resolver resolves them. A literal message still passes through untouched."
```

---

### Task 6: Japanese catalogue, the switcher and the no-literals guard

**Files:**
- Modify: `hdms-frontend/apps/admin/src/i18n/ja.ts` — every value replaced with Japanese
- Create: `hdms-frontend/apps/admin/src/components/settings/language-panel.tsx`
- Create: `hdms-frontend/apps/admin/src/components/settings/language-panel.test.tsx`
- Modify: `hdms-frontend/apps/admin/src/routes/settings.tsx`
- Create: `hdms-frontend/apps/admin/src/i18n/no-literals.test.ts`

**Interfaces:**
- Consumes: `updateMyLocale` (Task 1), `useLocale` (release one Task 3).
- Produces: `<LanguagePanel />`.

- [ ] **Step 1: Write the failing switcher test**

`hdms-frontend/apps/admin/src/components/settings/language-panel.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@hdms/i18n";
import { LanguagePanel } from "./language-panel";

describe("LanguagePanel", () => {
  it("persists the choice to the account and switches immediately", async () => {
    const updateMyLocale = vi.fn().mockResolvedValue({ locale: "en" });
    render(
      <LocaleProvider locale="ja">
        <LanguagePanel updateMyLocale={updateMyLocale} />
      </LocaleProvider>
    );

    await userEvent.click(screen.getByRole("radio", { name: "English" }));

    expect(updateMyLocale).toHaveBeenCalledWith({ locale: "en" });
    expect(document.documentElement.lang).toBe("en");
  });

  it("reverts the display if the save fails, so what is shown matches what is stored", async () => {
    const updateMyLocale = vi.fn().mockRejectedValue(new Error("network"));
    render(
      <LocaleProvider locale="ja">
        <LanguagePanel updateMyLocale={updateMyLocale} />
      </LocaleProvider>
    );

    await userEvent.click(screen.getByRole("radio", { name: "English" }));

    expect(document.documentElement.lang).toBe("ja");
  });
});
```

- [ ] **Step 2: Run to verify it fails, then implement**

Run: `cd hdms-frontend && pnpm --filter admin test -- language-panel`
Expected: FAIL — module not found.

`hdms-frontend/apps/admin/src/components/settings/language-panel.tsx`:

```tsx
import * as React from "react";
import { toast } from "sonner";
import { LOCALES, useLocale, type Locale } from "@hdms/i18n";
import { useT } from "@/i18n";

const LABELS: Record<Locale, string> = { ja: "日本語", en: "English" };

export interface LanguagePanelProps {
  updateMyLocale: (body: { locale: Locale }) => Promise<unknown>;
}

export function LanguagePanel({ updateMyLocale }: LanguagePanelProps) {
  const t = useT();
  const { locale, setLocale } = useLocale();

  const choose = async (next: Locale) => {
    const previous = locale;
    setLocale(next); // optimistic: the console switches under the click
    try {
      await updateMyLocale({ locale: next });
    } catch {
      setLocale(previous);
      toast.error(t("settings.language.saveFailed"));
    }
  };

  return (
    <fieldset>
      <legend>{t("settings.language.heading")}</legend>
      {LOCALES.map((option) => (
        <label key={option}>
          <input
            type="radio"
            name="locale"
            value={option}
            lang={option}
            checked={locale === option}
            onChange={() => void choose(option)}
          />
          {LABELS[option]}
        </label>
      ))}
    </fieldset>
  );
}
```

Each option is labelled in its own language, with a matching `lang` attribute, so a screen reader pronounces `日本語` in Japanese while the page is in English. Mount it in `routes/settings.tsx` alongside the existing panels.

- [ ] **Step 3: Add the no-literals guard**

Copy `hdms-frontend/apps/kiosk/src/i18n/no-literals.test.ts` to `hdms-frontend/apps/admin/src/i18n/no-literals.test.ts`, changing only the `ROOT` constant to resolve to `apps/admin/src`. Run it, and fix every offender by extraction:

Run: `cd hdms-frontend && pnpm --filter admin test -- no-literals`

Expect a long first list. Reserve `// i18n-allow-literal` for strings that must not be translated — asset tag formats, `HDMS`, email addresses, the sample tokens in `routes/labels.tsx` — and give each one a reason in the comment.

- [ ] **Step 4: Translate the catalogue**

Replace every value in `ja.ts` with Japanese. `en.ts` is untouched. Notes that matter:

- Administrative vocabulary must match what the hospital's own forms use: `貸出` and `返却` for loan and return, `備品` or `機器` for equipment — pick one per the counter card and use it everywhere.
- Keep `{count}`, `{name}`, `{entity}` intact; word order moves them.
- Do not translate an asset tag, employee number, support code, or email address.
- Column headers are read at a glance in a table — favour the short form.

Reuse the untranslated-key script from release one Task 7 Step 7 against the admin catalogue to find anything still in English, and review the list by eye.

- [ ] **Step 5: Run everything and commit**

Run: `cd hdms-frontend && pnpm --filter admin test && pnpm --filter admin build`
Expected: PASS. The build proves `en.ts` is complete against `ja.ts`.

```bash
git add hdms-frontend/apps/admin/src
git commit -m "feat(phase-6.9h): translate the admin console and add the language switcher

The switcher writes through to the account and reverts the display if the save
fails, so what is on screen always matches what is stored. A source scan fails
the build on any string left unextracted."
```

---

### Task 7: CSV export encoding

**Files:**
- Modify: `hdms-frontend/apps/admin/src/routes/devices.tsx:284`
- Create: `hdms-frontend/apps/admin/src/lib/csv.ts`
- Create: `hdms-frontend/apps/admin/src/lib/csv.test.ts`
- Modify: any other route that builds a CSV blob

**Interfaces:**
- Consumes: `useT` (Task 2).
- Produces: `function csvBlob(lines: string[]): Blob`.

- [ ] **Step 1: Write the failing test**

`hdms-frontend/apps/admin/src/lib/csv.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { csvBlob } from "./csv";

describe("csvBlob", () => {
  it("starts with a UTF-8 byte-order mark", async () => {
    const bytes = new Uint8Array(await csvBlob(["資産番号,名称"]).arrayBuffer());
    // Excel on Japanese Windows reads a BOM-less UTF-8 file as Shift_JIS and
    // renders every kanji as mojibake.
    expect([bytes[0], bytes[1], bytes[2]]).toEqual([0xef, 0xbb, 0xbf]);
  });

  it("keeps the content intact after the mark", async () => {
    const text = await csvBlob(["a,b", "1,2"]).text();
    expect(text.replace(/^﻿/, "")).toBe("a,b\n1,2");
  });
});
```

- [ ] **Step 2: Run to verify it fails, then implement**

Run: `cd hdms-frontend && pnpm --filter admin test -- csv`

`hdms-frontend/apps/admin/src/lib/csv.ts`:

```ts
/**
 * Excel on Japanese Windows treats a BOM-less UTF-8 file as Shift_JIS, so
 * every exported kanji arrives as mojibake. The mark costs three bytes and is
 * ignored by everything else that reads CSV.
 */
export function csvBlob(lines: string[]): Blob {
  return new Blob(["﻿", lines.join("\n")], { type: "text/csv;charset=utf-8;" });
}
```

Replace the construction at `routes/devices.tsx:284` and every other CSV blob site with `csvBlob(csvLines)`. Translate the header row through the catalogue while you are there — the column names in an export are user-visible text like any other.

- [ ] **Step 3: Verify by hand**

Export a device list containing at least one Japanese device name, open it in Excel on a Japanese-locale Windows machine, and confirm the kanji render. This is not provable from a unit test.

- [ ] **Step 4: Commit**

```bash
git add hdms-frontend/apps/admin/src
git commit -m "fix(phase-6.9h): write a BOM at the head of every CSV export

Verified in Excel on Japanese Windows; without the mark every kanji in an
export arrives as mojibake."
```

---

### Task 8: Print surfaces

**Files:**
- Modify: `hdms-frontend/apps/admin/src/components/label-templates.tsx`
- Modify: `hdms-frontend/apps/admin/src/components/register-slip.tsx`
- Modify: `hdms-frontend/apps/admin/src/routes/labels.tsx`
- Modify: `hdms-backend/cmd/hdms-cli/templates/`
- Modify: `docs/phases/phase-5/5.7-documentation-and-training.md` — the counter card

**Interfaces:**
- Consumes: `useT` (Task 2).
- Produces: nothing new.

- [ ] **Step 1: Extract the label and slip copy**

The text around a barcode is user-visible copy; the barcode payload never is. Extract the labels, headings, and instructions in `label-templates.tsx` and `register-slip.tsx` through `useT`, and leave every `bwip-js` call and every token string untouched.

- [ ] **Step 2: Add the print font fallback**

Print rendering does not inherit the app's font stack reliably across browsers. In the print stylesheet used by these templates, name the family explicitly:

```css
@media print {
  .label-template,
  .register-slip {
    font-family: "IBM Plex Sans", "Noto Sans JP", "Hiragino Sans", "Yu Gothic", sans-serif;
  }
}
```

- [ ] **Step 3: Verify a real print**

Print one sheet of device stickers and one register slip with Japanese device names, on the actual label stock. Check that no glyph is clipped by the label boundary and that the barcode still scans from the printed sheet — a font substitution that shifts the layout can move the quiet zone.

- [ ] **Step 4: Update the counter card**

The counter card sits beside the kiosk. If the kiosk speaks Japanese and the card beside it speaks English, the card is worse than nothing. Update `docs/phases/phase-5/5.7-documentation-and-training.md` to specify a bilingual card, with the Japanese first, and note that the card must be reprinted whenever the kiosk's instruction copy changes.

- [ ] **Step 5: Commit**

```bash
git add hdms-frontend/apps/admin/src docs/phases/phase-5
git commit -m "feat(phase-6.9i): localize label, slip and counter-card copy

The text around a barcode is copy like any other; the payload is not. Print
stylesheets name the Japanese face explicitly because print does not reliably
inherit the app's stack. Verified against printed stock — the barcode still
scans."
```

---

### Task 9: The backend rule

The backend emits no user-visible prose today. This task establishes where it may, and makes the boundary enforceable rather than conventional.

**Files:**
- Create: `hdms-backend/internal/platform/i18n/i18n.go`
- Create: `hdms-backend/internal/platform/i18n/i18n_test.go`
- Modify: `hdms-backend/cmd/hdms-cli/main.go`
- Modify: `hdms-backend/.golangci.yml`
- Create: `hdms-backend/internal/apiserver/no_translation_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `i18n.Catalogue`, `func New(tag language.Tag) *Catalogue`, `func (c *Catalogue) T(key string, args ...any) string`, `func FromEnv() *Catalogue`.

- [ ] **Step 1: Write the failing test**

`hdms-backend/internal/platform/i18n/i18n_test.go`:

```go
package i18n_test

import (
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/i18n"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"
)

func TestCatalogueRendersPerLocale(t *testing.T) {
	t.Parallel()

	require.Equal(t, "3 件の機器を取り込みました", i18n.New(language.Japanese).T("cli.import.done", 3))
	require.Equal(t, "Imported 3 devices", i18n.New(language.English).T("cli.import.done", 3))
}

func TestUnknownKeyReturnsTheKeyRatherThanPanicking(t *testing.T) {
	t.Parallel()
	require.Equal(t, "cli.nope", i18n.New(language.Japanese).T("cli.nope"))
}

func TestFromEnvDefaultsToJapanese(t *testing.T) {
	t.Parallel()
	t.Setenv("LANG", "")
	require.Equal(t, "3 件の機器を取り込みました", i18n.FromEnv().T("cli.import.done", 3))
}

func TestFromEnvHonoursAnEnglishLang(t *testing.T) {
	t.Parallel()
	t.Setenv("LANG", "en_US.UTF-8")
	require.Equal(t, "Imported 3 devices", i18n.FromEnv().T("cli.import.done", 3))
}
```

A CLI is the one place a raw key is the right fallback — an operator at a terminal can act on `cli.nope`, and a silent empty string there hides a bug.

- [ ] **Step 2: Run to verify it fails, then implement**

Run: `cd hdms-backend && go test ./internal/platform/i18n/...`

`hdms-backend/internal/platform/i18n/i18n.go`:

```go
// Package i18n localizes text the backend emits where no client exists to do
// it — CLI output today, notification email when 6.2 is built.
//
// It is deliberately unavailable to the API layer. An API response carries a
// machine-readable code and the client renders the words; see the depguard
// rule in .golangci.yml, which enforces exactly that.
package i18n

import (
	"os"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

var defaultTag = language.Japanese

var matcher = language.NewMatcher([]language.Tag{language.Japanese, language.English})

func init() {
	must(message.SetString(language.Japanese, "cli.import.done", "%d 件の機器を取り込みました"))
	must(message.SetString(language.English, "cli.import.done", "Imported %d devices"))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

type Catalogue struct {
	printer *message.Printer
}

func New(tag language.Tag) *Catalogue {
	return &Catalogue{printer: message.NewPrinter(tag)}
}

// FromEnv resolves the locale from LANG, defaulting to Japanese.
func FromEnv() *Catalogue {
	lang := os.Getenv("LANG")
	if lang == "" {
		return New(defaultTag)
	}
	tag, _ := language.MatchStrings(matcher, strings.SplitN(lang, ".", 2)[0])
	return New(tag)
}

func (c *Catalogue) T(key string, args ...any) string {
	return c.printer.Sprintf(key, args...)
}
```

Add the dependency: `cd hdms-backend && go get golang.org/x/text@latest && go mod tidy`.

- [ ] **Step 3: Wire the CLI**

In `hdms-backend/cmd/hdms-cli/main.go`, add a `--locale` flag that overrides `LANG`, construct the catalogue once at start-up, and route the operator-facing output lines through it. Do not translate log lines, error wrapping, or anything written to stderr for a developer — only the output an operator reads as the result of a command.

- [ ] **Step 4: Enforce the boundary**

In `hdms-backend/.golangci.yml`, add to `linters.settings.depguard.rules`:

```yaml
        api-emits-codes-not-prose:
          files: ["**/internal/apiserver/**"]
          deny:
            - pkg: github.com/hito-hospital/hdms/internal/platform/i18n$
              desc: "API responses carry codes, never translated prose — the client renders the words (docs/superpowers/specs/2026-08-31-i18n-l10n-design.md)"
            - pkg: golang.org/x/text/message$
              desc: "API responses carry codes, never translated prose — the client renders the words"
```

Note the trailing `$`: a bare `pkg:` entry in depguard v2 denies the whole subtree, which would also block `golang.org/x/text/language` if that is ever needed for a header parse. Anchor it to the exact package.

Add `hdms-backend/internal/apiserver/no_translation_test.go`, because depguard cannot see a header read:

```go
package apiserver_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The contract rule is that no endpoint varies its response text by the
// caller's language. Reading Accept-Language is the first step of breaking it,
// so it is the thing to catch.
func TestNoHandlerReadsAcceptLanguage(t *testing.T) {
	t.Parallel()

	var offenders []string
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return err
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(source), "Accept-Language") {
			offenders = append(offenders, path)
		}
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, offenders, "an API response must not vary by the caller's language")
}
```

- [ ] **Step 5: Run everything**

```bash
cd /Users/hitohospital/personal/dev/hito-device-management
task lint
task test
task test:integration
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add hdms-backend
git commit -m "feat(phase-6.9j): localize CLI output and fence the API off from prose

golang.org/x/text backs the operator-facing CLI output. A depguard rule keeps
the catalogue out of the API layer and a source test keeps Accept-Language out
of it, so the contract rule is enforced rather than remembered."
```

---

### Task 10: Exit verification

**Files:**
- Modify: `hdms-frontend/e2e/` — an admin pass in each locale
- Modify: `docs/phases/phase-6/6.9-second-language.md` — point it at the spec and both plans

- [ ] **Step 1: Add the admin e2e passes**

One signed-in admin journey per locale — sign in, list devices, open one, edit it, export the CSV — asserting through the catalogue. Not a screen-by-screen sweep.

- [ ] **Step 2: Run the axe suite per locale over the admin app**

Wrap the admin app's existing `vitest-axe` assertions in `describe.each(LOCALES)`, as release one Task 9 did for the kiosk, including the `document.documentElement.lang` assertion.

- [ ] **Step 3: Run the whole gate**

```bash
cd /Users/hitohospital/personal/dev/hito-device-management
task lint
task test
task test:integration
task e2e
```

Expected: PASS on all four. A green result from any single command is not the gate; all four are.

- [ ] **Step 4: Check the exit criteria**

- [ ] No user-visible string literal remains in kiosk or admin components — both `no-literals` tests green in CI
- [ ] Both locales pass the worst-case layout and axe suites
- [ ] A native speaker who works at the counter has reviewed the kiosk copy in place, on the iPad
- [ ] A scan succeeds with the IME active, on the hardware
- [ ] A CSV containing kanji opens correctly in Excel on Japanese Windows
- [ ] A printed label sheet with Japanese device names still scans

- [ ] **Step 5: Retire the superseded plan**

Replace the body of `docs/phases/phase-6/6.9-second-language.md` with a short note that it is superseded, linking to the spec and both plans, and keeping its risk table — it was right about most of the risks.

- [ ] **Step 6: Commit**

```bash
git add hdms-frontend/e2e hdms-frontend/apps/admin docs/phases/phase-6
git commit -m "test(phase-6.9j): verify both locales end to end and retire the old 6.9 plan"
```

- [ ] **Step 7: Schedule the four-week check**

Four weeks after ship, check whether the second language is actually being selected — the kiosk toggle and the admin switcher both write somewhere observable. If it is not being used, the translation was not the barrier, and the request behind it should be re-examined rather than extended.

---

## Self-review

**Spec coverage.** Admin locale resolution — Tasks 1, 2, 6. Admin extraction — Tasks 3, 4, 5. Collation — Task 4. CSV BOM — Task 7. Print surfaces and the counter card — Task 8. Backend rule, CLI, and the enforcement of "codes out, prose only where there's no client" — Task 9. Enforcement for admin — Task 6 Step 3. Exit criteria — Task 10. Everything else in the spec belongs to release one.

**Gap found and closed:** the spec lists notification emails as a backend translation target keyed on the recipient's stored locale. 6.2 does not exist, so there is nothing to translate; Task 9 builds the catalogue those templates will use and its package comment names the intended second caller, which is as far as this plan should go without building unrequested features.

**Type consistency.** `AdminMessages`, `catalogues`, `useT`, `useLocalizedResolver`, `csvBlob`, `LanguagePanel`, `updateMyLocale`, `useDeviceColumns`, `i18n.Catalogue`, `i18n.New`, `i18n.FromEnv`, `Catalogue.T` — each defined once and referenced under the same name after. `Locale`, `LOCALES`, `DEFAULT_LOCALE`, `isLocale`, `useLocale`, `LocaleProvider`, `collator` come from release one and are used with the signatures defined there.
