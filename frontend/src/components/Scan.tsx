import { useEffect, useMemo, useState } from "react";
import { webapp } from "../../wailsjs/go/models";
import { ScanFile } from "../../wailsjs/go/webapp/ScanService";
import { FetchAsset } from "../../wailsjs/go/webapp/AssetService";
import { EventsOff, EventsOn } from "../../wailsjs/runtime/runtime";

type Props = {
  // filePath is the shared "current file" set via the global Load file… bar.
  filePath: string;
  // onPickFile opens the shared file picker (updates the global current file).
  onPickFile: () => void;
  // onOpenInSingleAsset switches to the Single Asset tab loaded with this asset.
  onOpenInSingleAsset: (assetId: number) => void;
};

type SortField =
  | "assetId"
  | "assetTypeName"
  | "useCount"
  | "pixels"
  | "bytesSize"
  | "gpuMemoryBytes"
  | "meshTriangles";

const COLUMNS: { field: SortField; label: string; numeric: boolean }[] = [
  { field: "assetId", label: "Asset ID", numeric: true },
  { field: "assetTypeName", label: "Type", numeric: false },
  { field: "useCount", label: "Uses", numeric: true },
  { field: "pixels", label: "Dimensions", numeric: true },
  { field: "bytesSize", label: "Size", numeric: true },
  { field: "gpuMemoryBytes", label: "GPU Memory", numeric: true },
  { field: "meshTriangles", label: "Tris", numeric: true },
];

function sortValue(row: webapp.ScanRow, field: SortField): number | string {
  if (field === "pixels") return row.width * row.height;
  if (field === "assetTypeName") return row.assetTypeName ?? "";
  return (row as any)[field] ?? 0;
}

function Card({ value, caption, accent }: { value: string; caption: string; accent?: boolean }) {
  return (
    <div className={"stat-card" + (accent ? " accent" : "")}>
      <div className="stat-value">{value}</div>
      <div className="stat-caption">{caption}</div>
    </div>
  );
}

export default function Scan({ filePath, onPickFile, onOpenInSingleAsset }: Props) {
  const [scanning, setScanning] = useState(false);
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
  const [report, setReport] = useState<webapp.ScanReport | null>(null);
  const [error, setError] = useState("");

  // Filters / sort (client-side over the loaded rows).
  const [query, setQuery] = useState("");
  const [typeFilter, setTypeFilter] = useState("All");
  const [sortField, setSortField] = useState<SortField>("gpuMemoryBytes");
  const [sortDir, setSortDir] = useState<"asc" | "desc">("desc");
  const [advancedOpen, setAdvancedOpen] = useState(false);

  // Scan-time options (require a re-scan to take effect).
  const [pathFilter, setPathFilter] = useState("");
  const [largeThresholdMB, setLargeThresholdMB] = useState(0);

  // Row selection / preview.
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [detail, setDetail] = useState<webapp.AssetDetails | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState("");

  useEffect(() => {
    EventsOn("scan:progress", (p: { done: number; total: number }) => setProgress(p));
    return () => EventsOff("scan:progress");
  }, []);

  async function runScan() {
    if (scanning || !filePath) return;
    setScanning(true);
    setError("");
    setReport(null);
    setProgress({ done: 0, total: 0 });
    try {
      const opts = webapp.ScanOptions.createFrom({
        pathFilter: pathFilter,
        largeTextureThresholdMb: largeThresholdMB,
      });
      const r = await ScanFile(filePath, opts);
      setReport(r);
    } catch (e: any) {
      setError(String(e));
    } finally {
      setScanning(false);
      setProgress(null);
    }
  }

  const types = useMemo(() => {
    const set = new Set<string>();
    report?.rows.forEach((r) => r.assetTypeName && set.add(r.assetTypeName));
    return ["All", ...Array.from(set).sort()];
  }, [report]);

  const rows = useMemo(() => {
    let out = report?.rows ?? [];
    const q = query.trim().toLowerCase();
    if (q) {
      out = out.filter(
        (r) =>
          String(r.assetId).includes(q) ||
          r.assetTypeName?.toLowerCase().includes(q) ||
          r.instancePath?.toLowerCase().includes(q) ||
          r.propertyName?.toLowerCase().includes(q)
      );
    }
    if (typeFilter !== "All") out = out.filter((r) => r.assetTypeName === typeFilter);
    const dir = sortDir === "asc" ? 1 : -1;
    return [...out].sort((a, b) => {
      const av = sortValue(a, sortField);
      const bv = sortValue(b, sortField);
      if (av < bv) return -1 * dir;
      if (av > bv) return 1 * dir;
      return 0;
    });
  }, [report, query, typeFilter, sortField, sortDir]);

  async function selectRow(assetId: number) {
    setSelectedId(assetId);
    setDetail(null);
    setDetailError("");
    setDetailLoading(true);
    try {
      setDetail(await FetchAsset(String(assetId)));
    } catch (e: any) {
      setDetailError(String(e));
    } finally {
      setDetailLoading(false);
    }
  }

  function toggleSort(field: SortField) {
    if (field === sortField) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortField(field);
      setSortDir("desc");
    }
  }

  const s = report?.summary;
  const pct = progress && progress.total > 0 ? Math.round((progress.done / progress.total) * 100) : 0;

  return (
    <div className="scan-view">
      <div className="file-row">
        <button onClick={onPickFile}>Select File…</button>
        <span className="file-path" title={filePath}>
          {filePath || "No .rbxl/.rbxm file selected. Use Select File… or the header Load file… bar."}
        </span>
        <button className="primary" disabled={scanning || !filePath} onClick={runScan}>
          {scanning ? "Scanning…" : "Scan"}
        </button>
      </div>

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
          <Card value={String(s.largeTextureCount)} caption="Large Textures" accent={s.largeTextureCount > 0} />
          <Card value={String(s.failedCount)} caption="Failed" accent={s.failedCount > 0} />
        </div>
      )}

      {report && (
        <>
          <div className="filter-bar">
            <input
              type="text"
              className="field"
              placeholder="Filter by asset ID, type, instance path…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            <select value={typeFilter} onChange={(e) => setTypeFilter(e.target.value)}>
              {types.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
            <button onClick={() => setAdvancedOpen((v) => !v)}>
              {advancedOpen ? "▾ Advanced" : "▸ Advanced"}
            </button>
          </div>

          {advancedOpen && (
            <div className="advanced-filters">
              <div className="row">
                <label>Path filter (prefixes)</label>
                <input
                  type="text"
                  className="field"
                  placeholder="game.Workspace.Map, …  (comma-separated, re-scans)"
                  value={pathFilter}
                  onChange={(e) => setPathFilter(e.target.value)}
                />
              </div>
              <div className="row">
                <label>Large-texture threshold (MB)</label>
                <input
                  type="number"
                  className="field"
                  min={0}
                  step={1}
                  value={largeThresholdMB}
                  onChange={(e) => setLargeThresholdMB(parseFloat(e.target.value) || 0)}
                />
                <button disabled={scanning || !filePath} onClick={runScan}>
                  Apply &amp; Re-scan
                </button>
              </div>
            </div>
          )}

          <div className="result-count hint">
            Showing {rows.length} of {report.rows.length} assets
          </div>

          <div className="scan-split">
          <div className="scan-table-wrap">
            <table className="scan-table">
              <thead>
                <tr>
                  {COLUMNS.map((c) => (
                    <th
                      key={c.field}
                      className={c.numeric ? "num" : ""}
                      onClick={() => toggleSort(c.field)}
                    >
                      {c.label}
                      {sortField === c.field ? (sortDir === "asc" ? " ▲" : " ▼") : ""}
                    </th>
                  ))}
                  <th>Instance Path</th>
                  <th>State</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r, i) => (
                  <tr
                    key={`${r.assetId}-${i}`}
                    onClick={() => selectRow(r.assetId)}
                    className={
                      "row-clickable" +
                      (r.state === "Failed" ? " row-failed" : "") +
                      (r.isLarge ? " row-large" : "") +
                      (r.assetId === selectedId ? " row-selected" : "")
                    }
                  >
                    <td className="num">{r.assetId}</td>
                    <td>{r.assetTypeName || "—"}</td>
                    <td className="num">{r.useCount}</td>
                    <td className="num">{r.dimensionsDisplay || "—"}</td>
                    <td className="num">{r.sizeDisplay || "—"}</td>
                    <td className="num">
                      {r.gpuMemoryDisplay || "—"}
                      {r.gpuFormatLabel && <span className="fmt-chip">{r.gpuFormatLabel}</span>}
                    </td>
                    <td className="num">{r.meshTriangles ? r.meshTriangles.toLocaleString() : "—"}</td>
                    <td className="path" title={r.instancePath}>
                      {r.instancePath || "—"}
                    </td>
                    <td className={r.state === "Failed" ? "state-failed" : ""} title={r.warningCause}>
                      {r.state || r.source}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {selectedId !== null && (
            <aside className="asset-detail-panel">
              <div className="detail-header">
                <span>Asset {selectedId}</span>
                <div className="detail-header-actions">
                  <button
                    title="Open this asset in the Single Asset tab"
                    onClick={() => onOpenInSingleAsset(selectedId)}
                  >
                    Open in Single Asset ↗
                  </button>
                  <button className="icon" title="Close" onClick={() => setSelectedId(null)}>
                    ✕
                  </button>
                </div>
              </div>

              {detailLoading && <div className="hint">Loading preview…</div>}
              {detailError && <div className="status-line error">{detailError}</div>}

              {detail && (
                <>
                  <div className="detail-preview">
                    {detail.previewDataUrl ? (
                      <img src={detail.previewDataUrl} alt={`Asset ${selectedId}`} />
                    ) : (
                      <div className="asset-preview-empty">No preview image</div>
                    )}
                  </div>
                  {detail.statusMessage && <div className="hint">{detail.statusMessage}</div>}
                  <table className="detail-meta">
                    <tbody>
                      {[
                        { label: "Type", value: detail.assetTypeDisplay },
                        { label: detail.dimensionsLabel || "Dimensions", value: detail.dimensionsDisplay },
                        { label: "Format", value: detail.formatDisplay },
                        { label: "Self Size", value: detail.selfSizeDisplay },
                        { label: "Total Size", value: detail.totalSizeDisplay },
                        { label: "Referenced", value: detail.referencedDisplay },
                        { label: "Source", value: detail.sourceDisplay },
                        { label: "State", value: detail.stateDescription },
                      ]
                        .filter((m) => m.value && m.value.trim() !== "")
                        .map((m) => (
                          <tr key={m.label}>
                            <td className="meta-label">{m.label}</td>
                            <td className="meta-value">{m.value}</td>
                          </tr>
                        ))}
                    </tbody>
                  </table>
                  {detail.warningMessage && (
                    <div className="status-line warn">⚠ {detail.warningMessage}</div>
                  )}
                </>
              )}
            </aside>
          )}
          </div>
        </>
      )}
    </div>
  );
}
