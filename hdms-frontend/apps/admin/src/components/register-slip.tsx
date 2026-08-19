// The paper register slip (FR-70, docs/08-admin-console.md) — a designed
// artefact, not a blank notebook: asset tag copied from the label and
// employee number are the two fields the backfill screen (Phase 4)
// duplicate-checks against, so they're the widest columns.
export function RegisterSlip({ rows = 20 }: { rows?: number }) {
  return (
    <div className="print-area bg-white p-6 text-black" style={{ width: "297mm" }}>
      <h1 className="text-lg font-bold">HITO HOSPITAL — EQUIPMENT REGISTER</h1>
      <p className="text-xs">
        Use only when the borrower has no card, or the kiosk is unavailable. Copy the ASSET TAG
        exactly as printed on the device label.
      </p>
      <div className="mt-2 flex justify-between text-xs">
        <span>Page ref: 2026-__-__ p.___</span>
        <span>Attendant: ______________________</span>
      </div>
      <table className="mt-2 w-full border-collapse text-xs">
        <thead>
          <tr>
            {["#", "ASSET TAG", "BORROWER NAME", "EMPLOYEE NUMBER", "DEPT", "OUT time", "IN time", "SIGN"].map(
              (h) => (
                <th key={h} className="border border-black px-1.5 py-1 text-left font-semibold">
                  {h}
                </th>
              ),
            )}
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
        <span>Entered in system by: ______________</span>
        <span>Date: __________</span>
        <span>☐ all rows entered</span>
      </div>
    </div>
  );
}
