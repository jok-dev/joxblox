import { useEffect, useMemo, useState } from "react";
import { webapp } from "../../wailsjs/go/models";
import { GetCachedScan, ScanFile } from "../../wailsjs/go/webapp/ScanService";
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

const MEGABYTE = 1024 * 1024;

// formatBytes mirrors the Go format.FormatSizeAuto rule used everywhere else in
// the app: MB above 1 MB, KB above 1 KB, otherwise raw bytes.
function formatBytes(bytes: number): string {
  if (bytes >= MEGABYTE) return `${(bytes / MEGABYTE).toFixed(2)} MB`;
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(2)} KB`;
  return `${Math.round(bytes)} bytes`;
}

function Card({ value, caption, accent }: { value: string; caption: string; accent?: boolean }) {
  return (
    <div className={"stat-card" + (accent ? " accent" : "")}>
      <div className="stat-value">{value}</div>
      <div className="stat-caption">{caption}</div>
    </div>
  );
}

// The metric a chart breaks the file down by. Each contributes the same way the
// summary cards are totalled in the Go scan service, so the per-type bars sum to
// the card totals (download = self bytes, gpu = GPU memory, triangles weighted
// by use count).
type MetricKey = "count" | "bytes" | "gpu" | "triangles";

const METRICS: { key: MetricKey; label: string; format: (n: number) => string }[] = [
  { key: "count", label: "Assets", format: (n) => n.toLocaleString() },
  { key: "bytes", label: "Download Size", format: formatBytes },
  { key: "gpu", label: "GPU Memory", format: formatBytes },
  { key: "triangles", label: "Triangles", format: (n) => n.toLocaleString() },
];

function rowMetric(row: webapp.ScanRow, metric: MetricKey): number {
  switch (metric) {
    case "count":
      return 1;
    case "bytes":
      return row.bytesSize ?? 0;
    case "gpu":
      return row.gpuMemoryBytes ?? 0;
    case "triangles":
      return (row.meshTriangles ?? 0) * Math.max(row.useCount ?? 1, 1);
  }
}

// Slice colors for the instance-type pie. The last entry is reserved for the
// grouped "Other" bucket; the rest cycle for the named slices.
const PIE_COLORS = [
  "#4f8cff",
  "#34d399",
  "#fbbf24",
  "#f87171",
  "#a78bfa",
  "#22d3ee",
  "#fb923c",
  "#f472b6",
];
const OTHER_COLOR = "#52525b";

type Slice = { label: string; value: number; color: string };

// arcPath builds an SVG wedge from a0 to a1 (radians) around (cx, cy).
function arcPath(cx: number, cy: number, r: number, a0: number, a1: number): string {
  const x0 = cx + r * Math.cos(a0);
  const y0 = cy + r * Math.sin(a0);
  const x1 = cx + r * Math.cos(a1);
  const y1 = cy + r * Math.sin(a1);
  const largeArc = a1 - a0 > Math.PI ? 1 : 0;
  return `M ${cx} ${cy} L ${x0} ${y0} A ${r} ${r} 0 ${largeArc} 1 ${x1} ${y1} Z`;
}

// PieChart renders a labelled pie with a legend. Slices are pre-sorted.
function PieChart({ slices }: { slices: Slice[] }) {
  const total = slices.reduce((sum, s) => sum + s.value, 0);
  if (total === 0) {
    return <div className="hint">No instances to break down.</div>;
  }
  const size = 140;
  const r = size / 2;
  let angle = -Math.PI / 2;
  return (
    <div className="summary-pie">
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} className="pie-svg">
        {slices.length === 1 ? (
          <circle cx={r} cy={r} r={r} fill={slices[0].color} />
        ) : (
          slices.map((s) => {
            const a0 = angle;
            const a1 = a0 + (s.value / total) * 2 * Math.PI;
            angle = a1;
            return <path key={s.label} d={arcPath(r, r, r, a0, a1)} fill={s.color} />;
          })
        )}
      </svg>
      <div className="pie-legend">
        {slices.map((s) => (
          <div className="pie-legend-row" key={s.label} title={s.label}>
            <span className="pie-swatch" style={{ background: s.color }} />
            <span className="pie-legend-label">{s.label}</span>
            <span className="pie-legend-value">
              {s.value.toLocaleString()}
              <span className="pie-legend-pct">{Math.round((s.value / total) * 100)}%</span>
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

type Bar = { label: string; sublabel?: string; title?: string; value: number };

// A simple CSS horizontal bar chart: each row is a label, a proportional track,
// and a formatted value. Bars are assumed pre-sorted descending by the caller.
function BarChart({
  bars,
  format,
}: {
  bars: Bar[];
  format: (n: number) => string;
}) {
  const max = bars.reduce((m, b) => Math.max(m, b.value), 0);
  if (bars.length === 0) {
    return <div className="hint">No data for this metric.</div>;
  }
  return (
    <div className="bar-chart">
      {bars.map((b, i) => (
        <div className="bar-row" key={`${b.label}-${i}`} title={b.title}>
          <div className="bar-label">
            {b.label}
            {b.sublabel && <span className="bar-sublabel">{b.sublabel}</span>}
          </div>
          <div className="bar-track">
            <div
              className="bar-fill"
              style={{ width: max > 0 ? `${(b.value / max) * 100}%` : "0%" }}
            />
          </div>
          <div className="bar-value">{format(b.value)}</div>
        </div>
      ))}
    </div>
  );
}

export default function Report({ filePath, scope, onPickFile }: Props) {
  const [scanning, setScanning] = useState(false);
  const [checking, setChecking] = useState(false);
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
  const [report, setReport] = useState<webapp.ScanReport | null>(null);
  const [error, setError] = useState("");

  // The metric driving both breakdown charts (by type and heaviest assets).
  const [metric, setMetric] = useState<MetricKey>("gpu");

  const isRbxl = filePath.toLowerCase().endsWith(".rbxl");

  useEffect(() => {
    EventsOn("scan:progress", (p: { done: number; total: number }) => setProgress(p));
    return () => EventsOff("scan:progress");
  }, []);

  // When the global file or DataModel scope changes, reset and look up any
  // cached scan for the new file + scope (shared with the Asset Breakdown tab).
  useEffect(() => {
    setReport(null);
    setError("");
    if (!filePath) return;
    void checkCache(scope);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filePath, scope]);

  async function checkCache(pf: string) {
    if (!filePath) return;
    setChecking(true);
    try {
      const opts = webapp.ScanOptions.createFrom({ pathFilter: pf, largeTextureThresholdMb: 0 });
      const res = await GetCachedScan(filePath, opts);
      setReport(res.found ? res.report : null);
    } catch {
      setReport(null);
    } finally {
      setChecking(false);
    }
  }

  async function runScan() {
    if (scanning || !filePath) return;
    setScanning(true);
    setError("");
    setReport(null);
    setProgress({ done: 0, total: 0 });
    try {
      const opts = webapp.ScanOptions.createFrom({ pathFilter: scope, largeTextureThresholdMb: 0 });
      const r = await ScanFile(filePath, opts);
      setReport(r);
    } catch (e: any) {
      setError(String(e));
    } finally {
      setScanning(false);
      setProgress(null);
    }
  }

  const activeMetric = METRICS.find((m) => m.key === metric)!;

  // Aggregate the selected metric per asset type, sorted descending.
  const byType = useMemo<Bar[]>(() => {
    const rows = report?.rows ?? [];
    const totals = new Map<string, number>();
    for (const r of rows) {
      const name = r.assetTypeName || "Unknown";
      totals.set(name, (totals.get(name) ?? 0) + rowMetric(r, metric));
    }
    return Array.from(totals.entries())
      .map(([label, value]) => ({ label, value }))
      .filter((b) => b.value > 0)
      .sort((a, b) => b.value - a.value);
  }, [report, metric]);

  // The heaviest individual assets for the selected metric (top 15).
  const topAssets = useMemo<Bar[]>(() => {
    const rows = report?.rows ?? [];
    return rows
      .map((r) => ({
        label: String(r.assetId),
        sublabel: r.assetTypeName || undefined,
        title: r.instancePath || undefined,
        value: rowMetric(r, metric),
      }))
      .filter((b) => b.value > 0)
      .sort((a, b) => b.value - a.value)
      .slice(0, 15);
  }, [report, metric]);

  // Count scan rows per instance class for the pie, keeping the top slices named
  // and rolling the long tail into a single "Other" wedge.
  const byInstanceType = useMemo<Slice[]>(() => {
    const rows = report?.rows ?? [];
    const counts = new Map<string, number>();
    for (const r of rows) {
      const name = r.instanceType || "Unknown";
      counts.set(name, (counts.get(name) ?? 0) + 1);
    }
    const sorted = Array.from(counts.entries())
      .map(([label, value]) => ({ label, value }))
      .sort((a, b) => b.value - a.value);
    const TOP = PIE_COLORS.length;
    const named = sorted.slice(0, TOP).map((s, i) => ({ ...s, color: PIE_COLORS[i] }));
    const rest = sorted.slice(TOP);
    if (rest.length > 0) {
      const otherTotal = rest.reduce((sum, s) => sum + s.value, 0);
      named.push({ label: `Other (${rest.length})`, value: otherTotal, color: OTHER_COLOR });
    }
    return named;
  }, [report]);

  const s = report?.summary;
  const pct = progress && progress.total > 0 ? Math.round((progress.done / progress.total) * 100) : 0;
  const showEmpty = !report && !scanning;

  return (
    <div className={"report-view" + (showEmpty ? " empty" : "")}>
      {scanning && (
        <div className="scan-progress">
          <div className="bar">
            <div className="fill" style={{ width: `${pct}%` }} />
          </div>
          <span className="hint">
            {progress && progress.total > 0
              ? `Resolving assets ${progress.done} / ${progress.total}`
              : "Extracting asset references…"}
          </span>
        </div>
      )}

      {error && <div className="status-line error">{error}</div>}

      {s && (
        <div className="stat-cards">
          <Card value={String(s.totalAssets)} caption="Assets" />
          <Card value={s.totalBytesDisplay || "0 B"} caption="Download Size" />
          <Card value={s.totalGpuDisplay || "0 B"} caption="GPU Memory" />
          <Card value={s.totalTriangles.toLocaleString()} caption="Triangles" />
          <Card value={String(s.failedCount)} caption="Failed" accent={s.failedCount > 0} />
        </div>
      )}

      {/* Empty / prompt states (no report and not scanning). */}
      {!report && !scanning && (
        <div className="scan-empty">
          {!filePath ? (
            <>
              <div className="scan-empty-title">No file selected</div>
              <div className="hint">Use the Load file… bar at the top to pick an .rbxl or .rbxm file.</div>
              <button onClick={onPickFile}>Load file…</button>
            </>
          ) : checking ? (
            <div className="hint">Checking for previous results…</div>
          ) : (
            <>
              <div className="scan-empty-title">
                {isRbxl ? "Place file ready to analyze" : "Model file ready to analyze"}
              </div>
              <div className="hint">
                {scope ? `Scoped to: ${scope}.` : "Analyzing the whole file."}
                {isRbxl ? " Use the Datamodel selector in the top bar to narrow what gets analyzed." : ""}
              </div>
              <button className="primary big" disabled={scanning} onClick={runScan}>
                Process
              </button>
            </>
          )}
        </div>
      )}

      {report && s && s.totalAssets === 0 && (
        <div className="placeholder">
          <div>No assets found in the selected file.</div>
        </div>
      )}

      {report && s && s.totalAssets > 0 && (
        <>
          <div className="summary-metric-toggle">
            <span className="hint">Break down by</span>
            {METRICS.map((m) => (
              <button
                key={m.key}
                className={"chip" + (m.key === metric ? " active" : "")}
                onClick={() => setMetric(m.key)}
              >
                {m.label}
              </button>
            ))}
          </div>

          <div className="summary-charts">
            <div className="summary-panel">
              <div className="report-heading small">Instance types</div>
              <PieChart slices={byInstanceType} />
            </div>

            <div className="summary-panel">
              <div className="report-heading small">{activeMetric.label} by asset type</div>
              <BarChart bars={byType} format={activeMetric.format} />
            </div>

            <div className="summary-panel">
              <div className="report-heading small">Heaviest assets · {activeMetric.label}</div>
              <BarChart bars={topAssets} format={activeMetric.format} />
            </div>
          </div>
        </>
      )}
    </div>
  );
}
