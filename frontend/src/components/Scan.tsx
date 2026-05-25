import { useEffect, useMemo, useState } from "react";
import { webapp } from "../../wailsjs/go/models";
import { GetCachedScan, ScanFile } from "../../wailsjs/go/webapp/ScanService";
import { FetchAsset } from "../../wailsjs/go/webapp/AssetService";
import { EventsOff, EventsOn } from "../../wailsjs/runtime/runtime";

type Props = {
  // filePath is the shared "current file" set via the global Load file… bar.
  filePath: string;
  // pathFilter is the global DataModel scope (comma-separated service names,
  // empty = whole file) chosen via the header selector.
  pathFilter: string;
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
  | "meshTriangles"
  | "instancePath"
  | "state";

const COLUMNS: { field: SortField; label: string; numeric: boolean }[] = [
  { field: "assetId", label: "Asset ID", numeric: true },
  { field: "assetTypeName", label: "Type", numeric: false },
  { field: "useCount", label: "Uses", numeric: true },
  { field: "pixels", label: "Dimensions", numeric: true },
  { field: "bytesSize", label: "Size", numeric: true },
  { field: "gpuMemoryBytes", label: "GPU Memory", numeric: true },
  { field: "meshTriangles", label: "Tris", numeric: true },
  { field: "instancePath", label: "Instance Path", numeric: false },
  { field: "state", label: "State", numeric: false },
];

function sortValue(row: webapp.ScanRow, field: SortField): number | string {
  if (field === "pixels") return row.width * row.height;
  if (field === "assetTypeName") return (row.assetTypeName ?? "").toLowerCase();
  if (field === "instancePath") return (row.instancePath ?? "").toLowerCase();
  if (field === "state") return (row.state || row.source || "").toLowerCase();
  return (row as any)[field] ?? 0;
}

// ---- Search query mini-language ----------------------------------------
// The search bar accepts three kinds of space/comma-separated tokens:
//   • free text            — matches asset ID / type / instance path / property
//   • numeric comparison   — `field<op>value`, e.g. `size>2mb`, `tris>5000`
//   • text category        — `field:value`,   e.g. `path:Lighting.Example`, `type:Image`
// Free text and numeric filters AND together. Text-category values for the
// same field OR together (so `type:Image,type:BasePart` matches either), while
// different fields AND together.

type Comparator = ">=" | "<=" | ">" | "<" | "=";

type FieldFilter = { field: string; op: Comparator; value: number };

type TextFilter = { field: string; value: string };

// Fields whose value is a byte count and whose query value takes a size suffix.
const SIZE_FIELDS = new Set(["size", "gpu"]);

// Text-category fields and the row text they match against (contains).
const TEXT_FIELDS = new Set(["path", "type"]);

const COMPARE_RE = /^([a-z]+)(>=|<=|>|<|=)(.+)$/i;
const TEXT_FIELD_RE = /^([a-z]+):(.+)$/i;

function rowFieldValue(row: webapp.ScanRow, field: string): number | null {
  switch (field) {
    case "size":
      return Math.max(row.bytesSize ?? 0, row.totalBytesSize ?? 0);
    case "gpu":
      return row.gpuMemoryBytes ?? 0;
    case "tris":
    case "triangles":
      return row.meshTriangles ?? 0;
    case "uses":
      return row.useCount ?? 0;
    case "width":
      return row.width ?? 0;
    case "height":
      return row.height ?? 0;
    case "pixels":
      return (row.width ?? 0) * (row.height ?? 0);
    default:
      return null;
  }
}

// parseSize turns "2mb" / "500kb" / "100" into a byte count. Bare numbers
// default to MB to match the large-texture threshold this replaces.
function parseSize(raw: string): number | null {
  const m = raw.trim().toLowerCase().match(/^([\d.]+)\s*(b|kb|mb|gb)?$/);
  if (!m) return null;
  const n = parseFloat(m[1]);
  if (Number.isNaN(n)) return null;
  const unit = m[2] ?? "mb";
  const mult = unit === "b" ? 1 : unit === "kb" ? 1024 : unit === "mb" ? 1024 ** 2 : 1024 ** 3;
  return n * mult;
}

function parseFieldValue(field: string, raw: string): number | null {
  if (SIZE_FIELDS.has(field)) return parseSize(raw);
  const n = parseFloat(raw);
  return Number.isNaN(n) ? null : n;
}

function parseQuery(query: string): {
  textTerms: string[];
  filters: FieldFilter[];
  textFilters: TextFilter[];
} {
  const textTerms: string[] = [];
  const filters: FieldFilter[] = [];
  const textFilters: TextFilter[] = [];
  for (const token of query.trim().split(/[\s,]+/).filter(Boolean)) {
    const cmp = token.match(COMPARE_RE);
    if (cmp) {
      const field = cmp[1].toLowerCase();
      const value = parseFieldValue(field, cmp[3]);
      // Only treat it as a filter if the field is known and the value parses;
      // otherwise fall through to free-text matching.
      if (value !== null && rowFieldValue({} as webapp.ScanRow, field) !== null) {
        filters.push({ field, op: cmp[2] as Comparator, value });
        continue;
      }
    }
    const txt = token.match(TEXT_FIELD_RE);
    if (txt && TEXT_FIELDS.has(txt[1].toLowerCase())) {
      textFilters.push({ field: txt[1].toLowerCase(), value: txt[2].toLowerCase() });
      continue;
    }
    textTerms.push(token.toLowerCase());
  }
  return { textTerms, filters, textFilters };
}

function compare(a: number, op: Comparator, b: number): boolean {
  switch (op) {
    case ">":
      return a > b;
    case "<":
      return a < b;
    case ">=":
      return a >= b;
    case "<=":
      return a <= b;
    case "=":
      return a === b;
  }
}

function matchesText(row: webapp.ScanRow, term: string): boolean {
  return (
    String(row.assetId).includes(term) ||
    (row.assetTypeName?.toLowerCase().includes(term) ?? false) ||
    (row.instancePath?.toLowerCase().includes(term) ?? false) ||
    (row.propertyName?.toLowerCase().includes(term) ?? false)
  );
}

// matchesTextField applies a `field:value` category filter (contains match).
// `type` matches either the asset type or the instance class so both
// `type:Image` and `type:BasePart` work.
function matchesTextField(row: webapp.ScanRow, field: string, value: string): boolean {
  switch (field) {
    case "path":
      return row.instancePath?.toLowerCase().includes(value) ?? false;
    case "type":
      return (
        (row.assetTypeName?.toLowerCase().includes(value) ?? false) ||
        (row.instanceType?.toLowerCase().includes(value) ?? false)
      );
    default:
      return false;
  }
}

function Card({ value, caption, accent }: { value: string; caption: string; accent?: boolean }) {
  return (
    <div className={"stat-card" + (accent ? " accent" : "")}>
      <div className="stat-value">{value}</div>
      <div className="stat-caption">{caption}</div>
    </div>
  );
}

export default function Scan({ filePath, pathFilter, onPickFile, onOpenInSingleAsset }: Props) {
  const [scanning, setScanning] = useState(false);
  const [checking, setChecking] = useState(false);
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
  const [report, setReport] = useState<webapp.ScanReport | null>(null);
  const [error, setError] = useState("");

  // Filters / sort (client-side over the loaded rows).
  const [query, setQuery] = useState("");
  const [typeFilter, setTypeFilter] = useState("All");
  const [sortField, setSortField] = useState<SortField>("gpuMemoryBytes");
  const [sortDir, setSortDir] = useState<"asc" | "desc">("desc");

  // Row selection / preview.
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [detail, setDetail] = useState<webapp.AssetDetails | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState("");

  const isRbxl = filePath.toLowerCase().endsWith(".rbxl");

  useEffect(() => {
    EventsOn("scan:progress", (p: { done: number; total: number }) => setProgress(p));
    return () => EventsOff("scan:progress");
  }, []);

  // When the global file or DataModel scope changes, reset state and look up
  // any cached result for the new file + scope.
  useEffect(() => {
    setReport(null);
    setSelectedId(null);
    setDetail(null);
    setError("");
    setQuery("");
    setTypeFilter("All");
    if (!filePath) return;
    void checkCache(pathFilter);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filePath, pathFilter]);

  // checkCache shows a previously-processed report for this scope, or leaves
  // the view on the "Process" prompt when nothing is cached.
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
      const opts = webapp.ScanOptions.createFrom({
        pathFilter: pathFilter,
        largeTextureThresholdMb: 0,
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
    const { textTerms, filters, textFilters } = parseQuery(query);
    for (const term of textTerms) {
      out = out.filter((r) => matchesText(r, term));
    }
    for (const f of filters) {
      out = out.filter((r) => {
        const v = rowFieldValue(r, f.field);
        return v !== null && compare(v, f.op, f.value);
      });
    }
    // Group text-category filters by field: OR within a field, AND across fields.
    const valuesByField = new Map<string, string[]>();
    for (const tf of textFilters) {
      const list = valuesByField.get(tf.field) ?? [];
      list.push(tf.value);
      valuesByField.set(tf.field, list);
    }
    for (const [field, values] of valuesByField) {
      out = out.filter((r) => values.some((v) => matchesTextField(r, field, v)));
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
                {pathFilter
                  ? `Scoped to: ${pathFilter}.`
                  : "Analyzing the whole file."}
                {isRbxl ? " Use the Datamodel selector in the top bar to narrow what gets scanned." : ""}
              </div>
              <button className="primary big" disabled={scanning} onClick={runScan}>
                Process
              </button>
            </>
          )}
        </div>
      )}

      {report && (
        <>
          <div className="filter-bar">
            <input
              type="text"
              className="field"
              placeholder="Search — text, path:Lighting.Example, type:Image, size>2mb, tris>5000"
              title={
                "Free text matches asset ID, type, instance path, property.\n" +
                "Numeric filters: field<op>value with op one of > < >= <= =.\n" +
                "  Numeric fields: size, gpu (accept b/kb/mb/gb, default MB), tris, uses, width, height, pixels.\n" +
                "Category filters: field:value (contains).\n" +
                "  Category fields: path, type (type matches asset type or instance class).\n" +
                "Separate tokens with spaces or commas. Different tokens AND together; repeating\n" +
                "the same category field ORs its values, e.g. `type:Image, type:BasePart`."
              }
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
          </div>

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
