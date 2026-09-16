import * as React from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import type { FieldValues, Resolver } from "react-hook-form";
import type { z } from "zod";
import { useT, catalogues } from "@/i18n";

export function useLocalizedResolver<Input extends FieldValues, Output = Input>(
  schema: z.ZodType<Output, Input>,
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
