import {
  checkEmployeeNo,
  createUser,
  getUnboundCredentialCount,
  listDepartments,
  registerUserWithCard,
  resolveCredential,
  zCreateUserRequest,
} from "@hdms/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRoute, Link } from "@tanstack/react-router";
import { CheckCircle2, CreditCard, Printer, ScanLine, UserPlus, XCircle } from "lucide-react";
import { useEffect, useRef, useState } from "react";
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
import { useLocalizedResolver } from "@/lib/localized-resolver";
import { authenticatedRoute } from "./authenticated";

const registerSchema = zCreateUserRequest.extend({
  employeeNo: z.string().min(1, "validation.employeeNoRequired"),
  fullName: z.string().min(1, "validation.fullNameRequired"),
  email: z.string().email("validation.emailInvalid").optional().or(z.literal("")),
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

function DuplicateCheck({
  employeeNo,
  onAvailabilityChange,
}: {
  employeeNo: string;
  onAvailabilityChange?: (available: boolean) => void;
}) {
  const debounced = useDebounced(employeeNo);
  const trimmed = debounced.trim();

  const { data, isFetching } = useQuery({
    queryKey: ["users", "check-employee-no", trimmed],
    queryFn: async () => {
      const { data, error } = await checkEmployeeNo({ query: { employeeNo: trimmed } });
      if (error) throw error;
      return data;
    },
    enabled: trimmed.length > 0,
  });

  useEffect(() => {
    if (!trimmed) {
      onAvailabilityChange?.(true);
    } else if (data) {
      onAvailabilityChange?.(data.available);
    }
  }, [trimmed, data, onAvailabilityChange]);

  if (!trimmed || isFetching || !data) return null;

  if (!data.available) {
    return (
      <FieldDescription className="text-xs text-destructive flex items-center flex-wrap gap-1">
        <XCircle className="size-3.5 inline shrink-0" />
        <span>{data.employeeNo} is already registered.</span>
        {data.existingUserId && (
          <Link
            to="/users"
            search={{ q: data.employeeNo }}
            className="underline font-medium hover:text-foreground inline-flex items-center ml-1"
          >
            View existing record
          </Link>
        )}
      </FieldDescription>
    );
  }

  return (
    <FieldDescription className="flex items-center gap-1 text-xs text-success font-medium">
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
  const inputRef = useRef<HTMLInputElement>(null);

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
          ref={inputRef}
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
        <FieldDescription
          className={
            status.ready
              ? "flex items-center gap-1 text-xs text-success"
              : "flex items-center gap-1 text-xs text-destructive"
          }
        >
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
  const [selectedDepartmentId, setSelectedDepartmentId] = useState<string>("");
  const [isEmployeeNoAvailable, setIsEmployeeNoAvailable] = useState(true);
  const [success, setSuccess] = useState<{
    fullName: string;
    employeeNo: string;
    departmentId?: string;
    department?: string;
    token?: string;
    mode: CardMode;
  } | null>(null);
  const [tokenDialogOpen, setTokenDialogOpen] = useState(false);

  const fullNameInputRef = useRef<HTMLInputElement>(null);

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
    resolver: useLocalizedResolver(registerSchema),
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
      setSuccess({
        fullName: user.fullName,
        employeeNo: user.employeeNo,
        departmentId: user.departmentId || selectedDepartmentId || undefined,
        department: departments?.find((d) => d.id === user.departmentId)?.name,
        token,
        mode: cardMode,
      });
      if (token) {
        setTokenDialogOpen(true);
      }
    },
    onError: (err: any) => {
      if (err?.status === 409 || err?.type === "unique-constraint-violation") {
        form.setError("employeeNo", {
          type: "manual",
          message: "Employee number is already registered.",
        });
      }
    },
  });

  function resetForm() {
    form.reset({ employeeNo: "", fullName: "", departmentId: "", email: "", phone: "", notes: "" });
    setCardMode("scan");
    setSelectedDepartmentId("");
    setResolvedCredentialId(null);
    setIsEmployeeNoAvailable(true);
    setSuccess(null);
    setTimeout(() => {
      fullNameInputRef.current?.focus();
    }, 50);
  }

  function handleRegisterAnother() {
    const retainedDepartmentId = success?.departmentId || selectedDepartmentId || form.getValues("departmentId") || "";
    form.reset({
      employeeNo: "",
      fullName: "",
      departmentId: retainedDepartmentId,
      email: "",
      phone: "",
      notes: "",
    });
    setCardMode("scan");
    setSelectedDepartmentId(retainedDepartmentId);
    setResolvedCredentialId(null);
    setIsEmployeeNoAvailable(true);
    setSuccess(null);
    setTimeout(() => {
      fullNameInputRef.current?.focus();
    }, 50);
  }



  const canSubmit = (cardMode !== "scan" || !!resolvedCredentialId) && isEmployeeNoAvailable;

  if (success) {
    return (
      <div className="mx-auto flex max-w-md flex-col items-center gap-5 rounded-lg border border-border bg-card p-8 text-center">
        <CheckCircle2 className="size-12 text-success" />
        <div>
          <h2 className="text-xl font-semibold">{success.fullName} is registered</h2>
          <p className="font-identifier text-sm text-muted-foreground mt-1">
            {success.employeeNo}
            {success.department && ` · ${success.department}`}
          </p>
          {success.mode === "scan" && (
            <p className="mt-2 text-sm text-muted-foreground flex items-center justify-center gap-1.5">
              <CreditCard className="size-4 text-success" />
              Card bound and ready to use.
            </p>
          )}
          {success.mode === "print" && (
            <p className="mt-2 text-sm text-muted-foreground flex items-center justify-center gap-1.5">
              <Printer className="size-4 text-primary" />
              New card generated.
            </p>
          )}
          {success.mode === "none" && (
            <p className="mt-2 text-sm text-warning font-medium">
              No card issued yet — this borrower cannot borrow until a card is assigned.
            </p>
          )}
        </div>

        <div className="flex flex-col w-full gap-2 pt-2">
          {success.token && (
            <Button
              variant="outline"
              className="w-full gap-2"
              onClick={() => setTokenDialogOpen(true)}
            >
              <Printer className="size-4" />
              Print card
            </Button>
          )}
          <Button autoFocus className="w-full gap-2" onClick={handleRegisterAnother}>
            <UserPlus className="size-4" />
            Register another
          </Button>
        </div>

        <TokenRevealDialog
          open={!!success.token && tokenDialogOpen}
          onOpenChange={setTokenDialogOpen}
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
              <Input
                id="fullName"
                autoFocus
                {...form.register("fullName")}
                ref={(e) => {
                  form.register("fullName").ref(e);
                  (fullNameInputRef as any).current = e;
                }}
              />
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
                <DuplicateCheck
                  employeeNo={employeeNo}
                  onAvailabilityChange={setIsEmployeeNoAvailable}
                />
              )}
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel htmlFor="departmentId">Department</FieldLabel>
              <Select
                value={selectedDepartmentId ? selectedDepartmentId : undefined}
                onValueChange={(v) => {
                  setSelectedDepartmentId(v);
                  form.setValue("departmentId", v, { shouldDirty: true, shouldTouch: true, shouldValidate: true });
                }}
              >
                <SelectTrigger id="departmentId" aria-label="Department" className="w-full">
                  <SelectValue placeholder="Select a department">
                    {departments?.find((d) => d.id === selectedDepartmentId)?.name}
                  </SelectValue>
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
            <label className="flex items-start gap-2 text-sm cursor-pointer">
              <input
                type="radio"
                name="cardMode"
                className="mt-1 accent-primary"
                checked={cardMode === "scan"}
                onChange={() => setCardMode("scan")}
              />
              <span className="flex-1">
                <span className="font-medium">Scan a blank card</span>
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
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input
                type="radio"
                name="cardMode"
                className="accent-primary"
                checked={cardMode === "print"}
                onChange={() => setCardMode("print")}
              />
              <span className="font-medium">Print a new card now</span>
            </label>
            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <input
                type="radio"
                name="cardMode"
                className="accent-primary"
                checked={cardMode === "none"}
                onChange={() => setCardMode("none")}
              />
              <span>
                <span className="font-medium">Register without a card</span>{" "}
                <span className="text-muted-foreground">(they cannot borrow yet)</span>
              </span>
            </label>
          </div>
        </div>

        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={resetForm}>
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

