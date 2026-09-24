import { createStaffReservation, getStaffBookingPolicy, type StaffDevice } from "@hdms/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useT } from "@/i18n";

// Hospital dates use Asia/Tokyo (+09:00 year-round), regardless of the phone's timezone.
function hospitalDateTime(date: Date): string {
  const nextMinute = Math.ceil(date.getTime() / 60_000) * 60_000;
  return new Date(nextMinute + 9 * 60 * 60_000).toISOString().slice(0, 16);
}

function parseHospitalDateTime(value: string): Date {
  return new Date(`${value}:00+09:00`);
}

export function ReservationForm({ device }: { device: StaffDevice }) {
  const t = useT();
  const queryClient = useQueryClient();
  const [startAt, setStartAt] = useState("");
  const [endAt, setEndAt] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [created, setCreated] = useState(false);
  const policyQuery = useQuery({
    queryKey: ["staff", "booking-policy"],
    queryFn: async () => {
      const { data, error } = await getStaffBookingPolicy();
      if (error) throw error;
      return data;
    },
  });
  const policy = policyQuery.data;
  const canBook = device.availability === "available" ||
    (device.availability === "in_use" && !!device.expectedBackAt);
  const now = new Date();
  const expectedReady = device.availability === "in_use" && device.expectedBackAt
    ? new Date(new Date(device.expectedBackAt).getTime() + (policy?.returnBufferMinutes ?? 0) * 60_000)
    : now;
  const earliest = expectedReady > now ? expectedReady : now;

  const mutation = useMutation({
    mutationFn: async () => {
      if (!policy) throw new Error("policy-unavailable");
      const start = parseHospitalDateTime(startAt);
      const end = parseHospitalDateTime(endAt);
      if (!startAt || !endAt || !Number.isFinite(start.getTime()) || !Number.isFinite(end.getTime()) ||
        start < earliest || end <= start || start.getTime() > Date.now() + policy.advanceDays * 24 * 60 * 60_000 ||
        end.getTime() - start.getTime() > policy.maxDurationDays * 24 * 60 * 60_000) {
        throw new Error("invalid-window");
      }
      const result = await createStaffReservation({
        body: { deviceId: device.id, startAt: start.toISOString(), endAt: end.toISOString() },
      });
      if (result.error) throw result.error;
      return result.data;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["staff", "me", "reservations"] });
      setError(null);
      setCreated(true);
    },
    onError: (cause: unknown) => {
      const kind = typeof cause === "object" && cause !== null && "type" in cause
        ? String(cause.type).split("/").pop()
        : cause instanceof Error ? cause.message : "";
      if (kind === "reservation-conflict") setError(t("booking.conflict"));
      else if (kind === "reservation-too-close") setError(t("booking.tooClose", { minutes: policy?.returnBufferMinutes ?? 0 }));
      else if (kind === "device-in-use") setError(t("booking.tooEarly", { minutes: policy?.returnBufferMinutes ?? 0 }));
      else if (kind === "device-unavailable") setError(t("booking.unavailable"));
      else if (kind === "invalid-window" || kind === "validation-failed") setError(t("booking.invalidWindow", { advanceDays: policy?.advanceDays ?? 0, maxDurationDays: policy?.maxDurationDays ?? 0 }));
      else setError(t("booking.failed"));
    },
  });

  if (created) {
    return (
      <section className="rounded-xl border bg-card p-4 shadow-sm" role="status">
        <p className="font-medium">{t("booking.created")}</p>
        <Link to="/" className="mt-2 inline-block text-sm font-medium text-primary hover:underline">
          {t("booking.viewMine")}
        </Link>
      </section>
    );
  }

  if (!canBook) {
    return <p className="text-sm text-muted-foreground">
      {device.availability === "in_use" ? t("booking.noExpectedReturn") : t("booking.unavailable")}
    </p>;
  }

  if (policyQuery.isLoading) return <p className="text-sm text-muted-foreground">{t("loading")}</p>;
  if (!policy) return <p role="alert" className="text-sm text-destructive">{t("booking.policyLoadFailed")}</p>;

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    mutation.mutate();
  }

  return (
    <section className="rounded-xl border bg-card p-4 shadow-sm">
      <h2 className="text-lg font-semibold">{t("booking.title")}</h2>
      {device.availability === "in_use" && (
        <p className="mt-1 text-sm text-muted-foreground">{t("booking.afterReturn", { minutes: policy.returnBufferMinutes })}</p>
      )}
      <p className="mt-1 text-sm text-muted-foreground">{t("booking.timezoneNote")}</p>
      <p className="mt-1 text-sm text-muted-foreground">{t("booking.windowNote", { advanceDays: policy.advanceDays, maxDurationDays: policy.maxDurationDays })}</p>
      {policy.returnBufferMinutes > 0 && (
        <p className="mt-1 text-sm text-muted-foreground">{t("booking.gapNote", { minutes: policy.returnBufferMinutes })}</p>
      )}
      <form onSubmit={submit} className="mt-4 flex flex-col gap-4">
        <label className="flex flex-col gap-1 text-sm font-medium">
          {t("booking.startAt")}
          <Input
            type="datetime-local"
            value={startAt}
            min={hospitalDateTime(earliest)}
            max={hospitalDateTime(new Date(now.getTime() + policy.advanceDays * 24 * 60 * 60_000))}
            onChange={(event) => setStartAt(event.target.value)}
            required
          />
        </label>
        <label className="flex flex-col gap-1 text-sm font-medium">
          {t("booking.endAt")}
          <Input
            type="datetime-local"
            value={endAt}
            min={startAt || hospitalDateTime(earliest)}
            onChange={(event) => setEndAt(event.target.value)}
            required
          />
        </label>
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        <Button type="submit" disabled={mutation.isPending} className="w-full">
          {mutation.isPending ? t("booking.submitting") : t("booking.submit")}
        </Button>
      </form>
    </section>
  );
}
