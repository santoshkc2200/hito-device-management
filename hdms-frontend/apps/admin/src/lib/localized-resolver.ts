import * as React from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import type { FieldValues, Resolver } from "react-hook-form";
import type { z } from "zod";
import { useT } from "@/i18n";
import { catalogues } from "@/i18n";

/**
 * zod schemas are declared outside React, so they carry catalogue keys as
 * their messages and the translation happens here, at the resolver boundary.
 * A message that is not a known key is passed through unchanged, so a schema
 * can still carry a literal where a key would be overkill.
 *
 * The Input/Output split (rather than a single `z.infer<T>`) mirrors
 * `zodResolver`'s own generics, because some schemas here coerce (e.g.
 * `z.coerce.bigint()` on category forms) and Input then differs from Output.
 */
export function useLocalizedResolver<Input extends FieldValues, Output = Input>(
  schema: z.ZodType<Output, Input>
): Resolver<Input, unknown, Output> {
  const t = useT();
  return React.useMemo(() => {
    const base = zodResolver<Input, unknown, Output>(schema);
    return async (values, context, options) => {
      const result = await base(values, context, options);
      translateErrors(result.errors, t);
      return result;
    };
  }, [schema, t]);
}

/**
 * `result.errors` is not flat: an array-typed field's error is itself an
 * array of per-item error objects (e.g. `errors.columns[1].message`), and
 * nested object fields error the same way. Walk the whole tree — not just
 * depth 1 — so a per-item message gets the same translation as a top-level
 * one.
 *
 * `ref` is skipped and non-plain objects are never descended into: a
 * `FieldError.ref` is the live DOM input element, which carries circular
 * references (parentNode/childNodes) that would otherwise recurse forever.
 */
function translateErrors(node: unknown, t: (key: never) => string): void {
  if (Array.isArray(node)) {
    for (const item of node) translateErrors(item, t);
    return;
  }
  if (!isPlainObject(node)) return;
  const message = node.message;
  if (typeof message === "string" && isCatalogueKey(message)) {
    node.message = t(message as never);
  }
  for (const key of Object.keys(node)) {
    if (key === "message" || key === "ref") continue;
    translateErrors(node[key], t);
  }
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  if (typeof value !== "object" || value === null) return false;
  const proto: unknown = Object.getPrototypeOf(value);
  return proto === Object.prototype || proto === null;
}

function isCatalogueKey(candidate: string): boolean {
  let node: unknown = catalogues.ja;
  for (const segment of candidate.split(".")) {
    if (typeof node !== "object" || node === null) return false;
    node = (node as Record<string, unknown>)[segment];
  }
  return typeof node === "string";
}
