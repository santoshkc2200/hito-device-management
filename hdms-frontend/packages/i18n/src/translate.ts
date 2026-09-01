import { DEFAULT_LOCALE, type Locale } from "./locale";

export type MessageTree = { [key: string]: string | MessageTree };

export type Catalogues<M extends MessageTree> = Record<Locale, M>;

/**
 * Dotted paths to every string leaf in a catalogue, so `t()` cannot be called
 * with a key that does not exist. This is the whole reason the kiosk carries a
 * typed catalogue instead of a runtime i18n framework: a missing key is a
 * compile error, not a raw key rendered at the counter.
 */
export type LeafKey<M> = M extends string
  ? never
  : {
      [K in keyof M & string]: M[K] extends string ? K : `${K}.${LeafKey<M[K]>}`;
    }[keyof M & string];

function lookup(tree: MessageTree | undefined, key: string): string | undefined {
  let node: string | MessageTree | undefined = tree;
  for (const segment of key.split(".")) {
    if (node === undefined || typeof node === "string") return undefined;
    node = node[segment];
  }
  return typeof node === "string" ? node : undefined;
}

function interpolate(template: string, params?: Record<string, string | number>): string {
  if (!params) return template;
  return template.replace(/\{(\w+)\}/g, (match, name: string) =>
    name in params ? String(params[name]) : match
  );
}

/**
 * Resolves a key in the active locale, falling back to the default locale and
 * finally to an empty string. It never returns the key itself — a raw key on a
 * kiosk screen is worse than a blank, because staff read it as an error code.
 */
export function translate<M extends MessageTree>(
  catalogues: Catalogues<M>,
  locale: Locale,
  key: LeafKey<M>,
  params?: Record<string, string | number>
): string {
  const active = lookup(catalogues[locale], key as string);
  const fallback = lookup(catalogues[DEFAULT_LOCALE], key as string);
  return interpolate(active ?? fallback ?? "", params);
}

/**
 * Japanese has a single plural category and English has two. This is the
 * entire plural requirement of this system; anything more belongs in the
 * catalogue as separate keys.
 */
export function plural(
  locale: Locale,
  count: number,
  forms: { one?: string; other: string }
): string {
  const category = new Intl.PluralRules(locale).select(count);
  const template = category === "one" ? (forms.one ?? forms.other) : forms.other;
  return interpolate(template, { count });
}
