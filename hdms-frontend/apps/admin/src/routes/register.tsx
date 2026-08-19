import {
  createUser,
  getUnboundCredentialCount,
  listDepartments,
  listUsers,
  registerUserWithCard,
  resolveCredential,
  zCreateUserRequest,
} from "@hdms/api-client";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRoute } from "@tanstack/react-router";
import { CheckCircle2, ScanLine, XCircle } from "lucide-react";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { TokenRevealDialog } from "@/components/token-reveal-dialog";
import { authenticatedRoute } from "./authenticated";

const registerSchema = zCreateUserRequest.extend({
  employeeNo: z.string().min(1, "Employee number is required"),
  fullName: z.string().min(1, "Full name is required"),
  email: z.string().email("Enter a valid email").optional().or(z.literal("")),
});
type RegisterFormValues = z.infer<typeof registerSchema>;

type CardMode = "scan" | "print" | "none";

function useDebounced<T>(value: T, delayMs = 300): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(id);
  }, [value, delayMs]);
  return debounced;
}

function DuplicateCheck({ employeeNo }: { employeeNo: string }) {
  const debounced = useDebounced(employeeNo);
  const { data, isFetching } = useQuery({
    queryKey: ["users", "duplicate-check", debounced],
    queryFn: async () => {
      const { data, error } = await listUsers({ query: { q: debounced, limit: 5 } });
      if (error) throw error;
      return data.items;
    },
    enabled: debounced.trim().length > 0,
  });

  const duplicate = data?.find((u) => u.employeeNo.toLowerCase() === debounced.trim().toLowerCase());

  if (!debounced.trim() || isFetching) return null;
  if (duplicate) {
    return (
      <FieldDescription className="text-destructive">
        {duplicate.fullName} already holds employee number {duplicate.employeeNo}.
      </FieldDescription>
    );
  }
  return (
    <FieldDescription className="flex items-center gap-1 text-success">
      <CheckCircle2 className="size-3.5" /> Available
    </FieldDescription>
  );
}

function ScanCardField({
  onResolved,
}: {
  onResolved: (result: { credentialId: string; ready: boolean; message: string } | null) => void;
}) {
  const [tokenInput, setTokenInput] = useState("");
  const [status, setStatus] = useState<{ ready: boolean; message: string } | null>(null);

  const mutation = useMutation({
    mutationFn: async (token: string) => resolveCredential({ query: { token } }),
    onSuccess: ({ data, error }) => {
      if (error || !data) {
        setStatus({ ready: false, message: "This card does not match any known credential." });
        onResolved(null);
        return;
      }
      if (data.type !== "unbound") {
        setStatus({
          ready: false,
          message:
            data.type === "user" || data.type === "device"
              ? "This card is already registered to someone else."
              : "This card cannot be bound.",
        });
        onResolved(null);
        return;
      }
      if (data.credentialStatus !== "active") {
        setStatus({ ready: false, message: `This card is ${data.credentialStatus}, not active.` });
        onResolved(null);
        return;
      }
      setStatus({ ready: true, message: `Ready to bind — ${data.kind.toUpperCase()} card.` });
      onResolved({ credentialId: data.credentialId, ready: true, message: "" });
    },
    onError: () => {
      setStatus({ ready: false, message: "Could not look up that card." });
      onResolved(null);
    },
  });

  return (
    <Field>
      <FieldLabel htmlFor="scan-token">Scan a blank card</FieldLabel>
      <div className="relative">
        <ScanLine className="absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          id="scan-token"
          className="pl-8 font-identifier"
          placeholder="Waiting for scan…"
          value={tokenInput}
          autoFocus
          onChange={(e) => {
            setTokenInput(e.target.value);
            setStatus(null);
            onResolved(null);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter" && tokenInput.trim()) {
              e.preventDefault();
              mutation.mutate(tokenInput.trim());
            }
          }}
        />
      </div>
      {status && (
        <FieldDescription className={status.ready ? "flex items-center gap-1 text-success" : "flex items-center gap-1 text-destructive"}>
          {status.ready ? <CheckCircle2 className="size-3.5" /> : <XCircle className="size-3.5" />}
          {status.message}
        </FieldDescription>
      )}
    </Field>
  );
}

function RegisterBorrowerForm() {
  const queryClient = useQueryClient();
  const [cardMode, setCardMode] = useState<CardMode>("scan");
  const [resolvedCredentialId, setResolvedCredentialId] = useState<string | null>(null);
  const [success, setSuccess] = useState<{
    fullName: string;
    employeeNo: string;
    department?: string;
    token?: string;
  } | null>(null);
  const [tokenDialogDismissed, setTokenDialogDismissed] = useState(false);

  const { data: departments } = useQuery({
    queryKey: ["departments"],
    queryFn: async () => {
      const { data, error } = await listDepartments();
      if (error) throw error;
      return data.items;
    },
  });

  const { data: unboundCount } = useQuery({
    queryKey: ["credentials", "unbound-count"],
    queryFn: async () => {
      const { data, error } = await getUnboundCredentialCount();
      if (error) throw error;
      return data.count;
    },
  });

  const form = useForm<RegisterFormValues>({
    resolver: zodResolver(registerSchema),
    defaultValues: { employeeNo: "", fullName: "", departmentId: "", email: "", phone: "", notes: "" },
  });
  const employeeNo = form.watch("employeeNo");

  const mutation = useMutation({
    mutationFn: async (values: RegisterFormValues) => {
      if (cardMode === "none") {
        const { data, error } = await createUser({ body: values });
        if (error) throw error;
        return { user: data!, token: undefined };
      }
      const { data, error } = await registerUserWithCard({
        body: {
          ...values,
          credentialId: cardMode === "scan" ? (resolvedCredentialId ?? undefined) : undefined,
        },
      });
      if (error) throw error;
      return { user: data!.user, token: data!.token };
    },
    onSuccess: async ({ user, token }) => {
      await queryClient.invalidateQueries({ queryKey: ["users"] });
      await queryClient.invalidateQueries({ queryKey: ["credentials", "unbound-count"] });
      setTokenDialogDismissed(false);
      setSuccess({
        fullName: user.fullName,
        employeeNo: user.employeeNo,
        department: departments?.find((d) => d.id === user.departmentId)?.name,
        token,
      });
    },
  });

  function reset() {
    form.reset({ employeeNo: "", fullName: "", departmentId: "", email: "", phone: "", notes: "" });
    setCardMode("scan");
    setResolvedCredentialId(null);
    setSuccess(null);
  }

  const canSubmit = cardMode !== "scan" || !!resolvedCredentialId;

  if (success) {
    return (
      <div className="mx-auto flex max-w-md flex-col items-center gap-4 rounded-lg border border-border bg-card p-8 text-center">
        <CheckCircle2 className="size-10 text-success" />
        <div>
          <h2 className="text-lg font-semibold">{success.fullName} is registered</h2>
          <p className="font-identifier text-sm text-muted-foreground">
            {success.employeeNo}
            {success.department && ` · ${success.department}`}
          </p>
          {!success.token && cardMode !== "none" && (
            <p className="mt-1 text-sm text-muted-foreground">Card bound and ready to use.</p>
          )}
          {!success.token && cardMode === "none" && (
            <p className="mt-1 text-sm text-warning">No card issued yet.</p>
          )}
        </div>
        <Button onClick={reset}>Register another</Button>
        <TokenRevealDialog
          open={!!success.token && !tokenDialogDismissed}
          onOpenChange={(o) => !o && setTokenDialogDismissed(true)}
          token={success.token}
          subject={{
            type: "user",
            fullName: success.fullName,
            employeeNo: success.employeeNo,
            department: success.department,
          }}
        />
      </div>
    );
  }

  return (
    <div className="mx-auto flex max-w-xl flex-col gap-6">
      <div>
        <h1 className="text-xl font-semibold">Register borrower</h1>
        <p className="text-sm text-muted-foreground">
          One screen, no navigation away — registering and issuing a card is one transaction.
        </p>
      </div>
      <form
        onSubmit={form.handleSubmit((v) => mutation.mutate(v))}
        className="flex flex-col gap-6 rounded-lg border border-border bg-card p-6"
      >
        <FieldGroup>
          <div className="grid grid-cols-2 gap-3">
            <Field data-invalid={!!form.formState.errors.fullName}>
              <FieldLabel htmlFor="fullName">Full name</FieldLabel>
              <Input id="fullName" autoFocus {...form.register("fullName")} />
              {form.formState.errors.fullName && (
                <FieldError>{form.formState.errors.fullName.message}</FieldError>
              )}
            </Field>
            <Field data-invalid={!!form.formState.errors.employeeNo}>
              <FieldLabel htmlFor="employeeNo">Employee no.</FieldLabel>
              <Input id="employeeNo" className="font-identifier" {...form.register("employeeNo")} />
              {form.formState.errors.employeeNo ? (
                <FieldError>{form.formState.errors.employeeNo.message}</FieldError>
              ) : (
                <DuplicateCheck employeeNo={employeeNo} />
              )}
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel htmlFor="departmentId">Department</FieldLabel>
              <Select
                value={form.watch("departmentId") || undefined}
                onValueChange={(v) => form.setValue("departmentId", v)}
              >
                <SelectTrigger id="departmentId" className="w-full">
                  <SelectValue placeholder="Select a department" />
                </SelectTrigger>
                <SelectContent>
                  {departments?.map((d) => (
                    <SelectItem key={d.id} value={d.id}>
                      {d.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor="phone">Phone</FieldLabel>
              <Input id="phone" {...form.register("phone")} />
            </Field>
          </div>
          <Field data-invalid={!!form.formState.errors.email}>
            <FieldLabel htmlFor="email">Email</FieldLabel>
            <Input id="email" type="email" {...form.register("email")} />
            {form.formState.errors.email && <FieldError>{form.formState.errors.email.message}</FieldError>}
          </Field>
        </FieldGroup>

        <div className="border-t border-border pt-4">
          <p className="mb-2 text-sm font-semibold">Assign card</p>
          <div className="flex flex-col gap-3">
            <label className="flex items-start gap-2 text-sm">
              <input
                type="radio"
                className="mt-1"
                checked={cardMode === "scan"}
                onChange={() => setCardMode("scan")}
              />
              <span className="flex-1">
                Scan a blank card
                {typeof unboundCount === "number" && (
                  <span className="ml-1 text-muted-foreground">({unboundCount} remain unbound)</span>
                )}
                {cardMode === "scan" && (
                  <div className="mt-2">
                    <ScanCardField
                      onResolved={(r) => setResolvedCredentialId(r ? r.credentialId : null)}
                    />
                  </div>
                )}
              </span>
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input type="radio" checked={cardMode === "print"} onChange={() => setCardMode("print")} />
              Print a new card now
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input type="radio" checked={cardMode === "none"} onChange={() => setCardMode("none")} />
              Register without a card (they cannot borrow yet)
            </label>
          </div>
        </div>

        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={reset}>
            Cancel
          </Button>
          <Button type="submit" disabled={!canSubmit || mutation.isPending}>
            {mutation.isPending ? "Registering…" : "Register & issue"}
          </Button>
        </div>
      </form>
    </div>
  );
}

export const registerRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/register",
  component: RegisterBorrowerForm,
});
