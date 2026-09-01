import { useT } from "@/i18n";

// The paper register slip (FR-70, docs/08-admin-console.md) — a designed
// artefact, not a blank notebook: asset tag copied from the label and
// employee number are the two fields the backfill screen (Phase 4)
// duplicate-checks against, so they're the widest columns.
export function RegisterSlip({ rows = 20 }: { rows?: number }) {
  const t = useT();
  const columns = [
    t("registerSlip.columnRowNumber"),
    t("registerSlip.columnAssetTag"),
    t("registerSlip.columnBorrowerName"),
    t("registerSlip.columnEmployeeNumber"),
    t("registerSlip.columnDepartment"),
    t("registerSlip.columnOutTime"),
    t("registerSlip.columnInTime"),
    t("registerSlip.columnSignature"),
  ];
  return (
    <div className="print-area register-slip bg-white p-6 text-black" style={{ width: "297mm" }}>
      <h1 className="text-lg font-bold">{t("registerSlip.heading")}</h1>
      <p className="text-xs">{t("registerSlip.instructions")}</p>
      <div className="mt-2 flex justify-between text-xs">
        <span>{t("registerSlip.pageReferenceBlank")}</span>
        <span>{t("registerSlip.attendantBlank")}</span>
      </div>
      <table className="mt-2 w-full border-collapse text-xs">
        <thead>
          <tr>
            {columns.map((h) => (
              <th key={h} className="border border-black px-1.5 py-1 text-left font-semibold">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {Array.from({ length: rows }, (_, i) => (
            <tr key={i}>
              <td className="border border-black px-1.5 py-2">{i + 1}</td>
              <td className="border border-black px-1.5 py-2" />
              <td className="border border-black px-1.5 py-2" />
              <td className="border border-black px-1.5 py-2" />
              <td className="border border-black px-1.5 py-2" />
              <td className="border border-black px-1.5 py-2" />
              <td className="border border-black px-1.5 py-2" />
              <td className="border border-black px-1.5 py-2" />
            </tr>
          ))}
        </tbody>
      </table>
      <div className="mt-2 flex items-center gap-6 text-xs">
        <span>{t("registerSlip.enteredByBlank")}</span>
        <span>{t("registerSlip.dateBlank")}</span>
        <span>{t("registerSlip.allRowsEnteredCheckbox")}</span>
      </div>
    </div>
  );
}
