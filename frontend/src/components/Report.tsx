import { useEffect, useState } from "react";
import { webapp } from "../../wailsjs/go/models";
import {
  GenerateReport,
  ListAssetTypes,
  SaveReportHTML,
  SaveReportJSON,
  SaveReportPDF,
} from "../../wailsjs/go/webapp/ReportService";
import { EventsOff, EventsOn } from "../../wailsjs/runtime/runtime";

type Props = {
  // filePath is the shared "current file" set via the global Load file… bar.
  filePath: string;
  // scope is the global DataModel scope (comma-separated service names, empty =
  // whole file) chosen via the header selector. Only applies to .rbxl places.
  scope: string;
  // onPickFile opens the shared file picker (updates the global current file).
  onPickFile: () => void;
};

type DetailModal = { title: string; rows: webapp.ReportDetailRow[] };

// gradeColor mirrors the Fyne report tab's letter-grade palette.
function gradeColor(grade: string): string {
  switch (grade) {
    case "A+":
      return "#00c853";
    case "A":
      return "#4caf50";
    case "B":
      return "#8bc34a";
    case "C":
      return "#ffc107";
    case "D":
      return "#ff9800";
    case "E":
      return "#ff5722";
    default:
      return "#f44336";
  }
}

export default function Report({ filePath, scope, onPickFile }: Props) {
  const [assetTypes, setAssetTypes] = useState<string[]>([]);
  const [assetType, setAssetType] = useState("");

  const [generating, setGenerating] = useState(false);
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
  const [result, setResult] = useState<webapp.ReportResult | null>(null);
  const [error, setError] = useState("");

  // ignoreBan flips the overall grade between the ban-forced F and the natural
  // grade, matching the Fyne tab's "ignore ban" toggle.
  const [ignoreBan, setIgnoreBan] = useState(false);
  const [detail, setDetail] = useState<DetailModal | null>(null);
  const [warningOpen, setWarningOpen] = useState(false);

  useEffect(() => {
    ListAssetTypes()
      .then((types) => {
        setAssetTypes(types);
        if (types.length > 0) setAssetType((cur) => cur || types[0]);
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    EventsOn("report:progress", (p: { done: number; total: number }) => setProgress(p));
    return () => EventsOff("report:progress");
  }, []);

  // Reset the rendered profile when the shared file or DataModel scope changes.
  useEffect(() => {
    setResult(null);
    setError("");
    setIgnoreBan(false);
  }, [filePath, scope]);

  async function generate() {
    if (!filePath) {
      setError("Load an .rbxl/.rbxm file first.");
      return;
    }
    setGenerating(true);
    setError("");
    setResult(null);
    setProgress(null);
    setIgnoreBan(false);
    try {
      const r = await GenerateReport(filePath, assetType, scope);
      setResult(r);
      if (!r.hasData) {
        setError("No assets found in the selected file.");
      }
    } catch (e: any) {
      setError(String(e));
    } finally {
      setGenerating(false);
      setProgress(null);
    }
  }

  async function runExport(save: () => Promise<boolean>) {
    try {
      await save();
    } catch (e: any) {
      setError(String(e));
    }
  }

  const banActive = !!result && result.bannedCount > 0 && !ignoreBan;
  const overallGrade = result
    ? ignoreBan
      ? result.overallGradeIgnoringBan
      : result.overallGrade
    : "";
  const overallScore = result
    ? ignoreBan
      ? result.overallScoreIgnoringBan
      : result.overallScore
    : 0;

  function openDetail(title: string, rows: webapp.ReportDetailRow[]) {
    if (rows.length > 0) setDetail({ title, rows });
  }

  function gradeDetailRows(label: string): webapp.ReportDetailRow[] {
    if (!result) return [];
    switch (label) {
      case "Mismatched PBR Maps":
        return result.mismatchedPbr;
      case "Oversized Textures":
        return result.oversized;
      case "Duplicates":
        return result.duplicates;
      default:
        return [];
    }
  }

  // With no file selected, mirror the Asset Breakdown tab: a centered
  // "No file selected" prompt with a Load button (the file is globally managed
  // via the top bar, so there's no per-tab file picker in the toolbar).
  if (!filePath) {
    return (
      <div className="report-view empty">
        <div className="scan-empty">
          <div className="scan-empty-title">No file selected</div>
          <div className="hint">Use the Load file… bar at the top to pick an .rbxl or .rbxm file.</div>
          <button onClick={onPickFile}>Load file…</button>
        </div>
      </div>
    );
  }

  return (
    <div className="report-view">
      <div className="report-toolbar">
        <label className="report-field">
          Asset type
          <select value={assetType} onChange={(e) => setAssetType(e.target.value)}>
            {assetTypes.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
        </label>
        <span className="spacer" />
        <button className="primary" disabled={generating} onClick={generate}>
          {generating ? "Generating…" : "Generate Report"}
        </button>
        <button
          disabled={!result || !result.hasData}
          onClick={() => runExport(SaveReportJSON)}
        >
          Export JSON
        </button>
        <button
          disabled={!result || !result.hasData}
          onClick={() => runExport(SaveReportHTML)}
        >
          Export HTML
        </button>
        <button
          disabled={!result || !result.hasData}
          onClick={() => runExport(SaveReportPDF)}
        >
          Export PDF
        </button>
      </div>

      {generating && (
        <div className="scan-progress">
          {progress
            ? `Resolving assets… ${progress.done}/${progress.total}`
            : "Analyzing file…"}
        </div>
      )}

      {error && <div className="status-line error">{error}</div>}

      {result && result.warning && (
        <div className="report-banner warn">
          <span>{result.warning}</span>
          {result.warningDetail && (
            <button className="info-btn" onClick={() => setWarningOpen((v) => !v)}>
              ⓘ
            </button>
          )}
        </div>
      )}
      {result && warningOpen && result.warningDetail && (
        <pre className="report-warning-detail">{result.warningDetail}</pre>
      )}

      {result && result.hasData && (
        <div className="report-profile">
          <div className="report-heading">
            Performance Profile
            {result.assetTypeLabel ? ` (${result.assetTypeLabel})` : ""}
          </div>
          <div className="report-range-note">{result.rangeNote}</div>
          <div className="report-overall" style={{ color: gradeColor(overallGrade) }}>
            Overall: {overallGrade} ({overallScore}%)
          </div>

          {result.bannedCount > 0 && (
            <div className={"report-banner " + (banActive ? "banned" : "warn")}>
              <span>
                {banActive
                  ? `BANNED: ${result.bannedCount} image(s) over the ${
                      result.bannedLimitMb > 0
                        ? result.bannedLimitMb.toFixed(2) + " MB "
                        : ""
                    }GPU-memory limit — overall grade forced to F`
                  : `BAN IGNORED: ${result.bannedCount} image(s) over the limit. Showing the natural grade.`}
              </span>
              <span className="report-banner-actions">
                {result.banned.length > 0 && (
                  <button onClick={() => openDetail("Banned Images", result.banned)}>
                    View
                  </button>
                )}
                <button onClick={() => setIgnoreBan((v) => !v)}>
                  {ignoreBan ? "Restore" : "Ignore"}
                </button>
              </span>
            </div>
          )}

          <table className="report-table">
            <thead>
              {!result.wholeFileMode && result.cellCount > 0 && (
                <tr>
                  <th />
                  <th>Metric</th>
                  <th>total</th>
                  <th title="Content-weighted average — what a typical unit of content sits in. Graded.">
                    typical
                  </th>
                  <th title="Worst single cell. Shown for context; not graded.">max/cell</th>
                  <th />
                </tr>
              )}
            </thead>
            <tbody>
              {result.grades.map((g) => (
                <tr key={g.label}>
                  <td
                    className="report-grade-cell"
                    style={{ color: gradeColor(g.grade) }}
                  >
                    {g.grade}
                  </td>
                  <td className="report-metric" title={g.metricDescription}>
                    {g.label}
                  </td>
                  <td title={g.description}>{g.value}</td>
                  <td>{g.avgCellValue}</td>
                  <td>{g.maxCellValue}</td>
                  <td>
                    {g.hasDetail && (
                      <button onClick={() => openDetail(g.label, gradeDetailRows(g.label))}>
                        View
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {!result && !generating && !error && (
        <div className="placeholder">
          <h2>Summary</h2>
          <div>Choose an asset type and click Generate Report.</div>
        </div>
      )}

      {detail && (
        <div className="modal-backdrop" onMouseDown={() => setDetail(null)}>
          <div className="modal report-detail-modal" onMouseDown={(e) => e.stopPropagation()}>
            <h2>
              {detail.title} ({detail.rows.length})
            </h2>
            <div className="report-detail-list">
              {detail.rows.map((row, i) => (
                <div key={i} className="report-detail-row">
                  <div className="report-detail-header">{row.header}</div>
                  {(row.lines ?? []).map((line, j) => (
                    <div key={j} className="report-detail-line">
                      {line}
                    </div>
                  ))}
                </div>
              ))}
            </div>
            <div className="modal-actions">
              <button onClick={() => setDetail(null)}>Close</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
