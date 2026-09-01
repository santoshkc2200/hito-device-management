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
