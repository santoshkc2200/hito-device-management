import { type CredentialKind, getUnboundCredentialCount, issueBlankBatch } from "@hdms/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRoute } from "@tanstack/react-router";
import { Printer } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { BlankCardLabel, LabelSheet } from "@/components/label-templates";
import { Button } from "@/components/ui/button";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { authenticatedRoute } from "./authenticated";

const CARD_SHEET_SETTINGS = {
  pageWidthMm: 210,
  pageHeightMm: 297,
  columns: 2,
  rows: 5,
  labelWidthMm: 85.6,
  labelHeightMm: 54,
  marginTopMm: 12,
  marginLeftMm: 15,
  gapXMm: 8,
  gapYMm: 6,
};

function CredentialsPage() {
  const queryClient = useQueryClient();
  const [count, setCount] = useState(10);
  const [kind, setKind] = useState<CredentialKind>("qr");
  const [batch, setBatch] = useState<string[] | null>(null);

  const { data: unboundCount } = useQuery({
    queryKey: ["credentials", "unbound-count"],
    queryFn: async () => {
      const { data, error } = await getUnboundCredentialCount();
      if (error) throw error;
      return data.count;
    },
  });

  const mutation = useMutation({
    mutationFn: async () => issueBlankBatch({ body: { count, kind } }),
    onSuccess: async ({ data, error }) => {
      if (error) throw error;
      await queryClient.invalidateQueries({ queryKey: ["credentials", "unbound-count"] });
      setBatch(data!.items.map((c) => c.token));
      toast.success(`${data!.items.length} blank cards minted`);
    },
    onError: () => toast.error("Could not mint the batch"),
  });

  if (batch) {
    return (
      <div className="flex flex-col gap-4">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold">Print the new batch</h1>
            <p className="text-sm text-muted-foreground">
              {batch.length} cards — keep this sheet with the drawer stock once cut and laminated.
            </p>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => setBatch(null)}>
              Done
            </Button>
            <Button onClick={() => window.print()}>
              <Printer className="size-4" data-icon="inline-start" />
              Print sheet
            </Button>
          </div>
        </div>
        <div className="overflow-auto rounded-md border border-border bg-secondary p-4">
          <LabelSheet settings={CARD_SHEET_SETTINGS}>
            {batch.map((token) => (
              <BlankCardLabel key={token} token={token} />
            ))}
          </LabelSheet>
        </div>
      </div>
    );
  }

  return (
    <div className="mx-auto flex max-w-md flex-col gap-6">
      <div>
        <h1 className="text-xl font-semibold">Blank card stock</h1>
        <p className="text-sm text-muted-foreground">
          {typeof unboundCount === "number"
            ? `${unboundCount} unbound ${unboundCount === 1 ? "card" : "cards"} currently in the drawer.`
            : "Loading…"}
          {typeof unboundCount === "number" && unboundCount < 10 && (
            <span className="ml-1 font-medium text-warning">Running low.</span>
          )}
        </p>
      </div>
      <div className="rounded-lg border border-border bg-card p-6">
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="batch-count">How many cards?</FieldLabel>
            <Input
              id="batch-count"
              type="number"
              min={1}
              max={200}
              value={count}
              onChange={(e) => setCount(Math.max(1, Number(e.target.value) || 1))}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="batch-kind">Kind</FieldLabel>
            <Select value={kind} onValueChange={(v) => setKind(v as CredentialKind)}>
              <SelectTrigger id="batch-kind" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="qr">QR</SelectItem>
                <SelectItem value="code128">Code 128</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Button disabled={mutation.isPending} onClick={() => mutation.mutate()}>
            Mint batch
          </Button>
        </FieldGroup>
      </div>
    </div>
  );
}

export const credentialsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/credentials",
  component: CredentialsPage,
});
