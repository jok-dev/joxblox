import { Fragment, useEffect, useMemo, useState } from "react";
import { webapp } from "../../wailsjs/go/models";
import {
  GenerateTagReportPDF,
  GetAssignments,
  ListTagDefinitions,
} from "../../wailsjs/go/webapp/TagService";
import { GetCachedScan } from "../../wailsjs/go/webapp/ScanService";
import { EventsOff, EventsOn } from "../../wailsjs/runtime/runtime";

type Props = {
  // filePath is the shared "current file" set via the global Load file… bar.
  filePath: string;
  // scope is the global DataModel scope (comma-separated service names, empty =
  // whole file); used to find the matching cached scan for instance breakdowns.
  scope: string;
  // onPickFile opens the shared file picker (updates the global current file).
  onPickFile: () => void;
  // onOpenInSingleAsset switches to the Single Asset tab loaded with this asset.
  onOpenInSingleAsset: (assetId: number) => void;
};

export default function TagReport({ filePath, scope, onPickFile, onOpenInSingleAsset }: Props) {
  const [defs, setDefs] = useState<webapp.TagDefinition[]>([]);
  const [assignments, setAssignments] = useState<Record<string, string[]>>({});
  // rows from the current file's cached scan, so each tagged asset can show the
  // instances (paths + breakdown) it appears at. Empty when nothing is cached.
  const [scanRows, setScanRows] = useState<webapp.ScanRow[]>([]);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [embedImages, setEmbedImages] = useState(true);
  const [generating, setGenerating] = useState(false);
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    void load();
  }, []);

  useEffect(() => {
    EventsOn("tag-report:progress", (p: { done: number; total: number }) => setProgress(p));
    return () => EventsOff("tag-report:progress");
  }, []);

  // Pull the cached scan for the current file + scope so the dropdowns can show
  // which instances each tagged asset is used at. No scan is triggered here —
  // if nothing is cached the dropdowns fall back to asset ids only.
  useEffect(() => {
    setScanRows([]);
    if (!filePath) return;
    const opts = webapp.ScanOptions.createFrom({ pathFilter: scope, largeTextureThresholdMb: 0 });
    GetCachedScan(filePath, opts)
      .then((res) => setScanRows(res.found ? res.report.rows ?? [] : []))
      .catch(() => setScanRows([]));
  }, [filePath, scope]);

  async function load() {
    try {
      const [d, a] = await Promise.all([ListTagDefinitions(), GetAssignments()]);
      setDefs(d ?? []);
      setAssignments(a ?? {});
    } catch (e: any) {
      setError(String(e));
    }
  }

  async function generate() {
    if (!filePath) {
      setError("Load an .rbxl/.rbxm file first.");
      return;
    }
    setGenerating(true);
    setError("");
    setStatus("");
    setProgress(null);
    try {
      const saved = await GenerateTagReportPDF(filePath, embedImages);
      setStatus(saved ? "PDF saved." : "Export cancelled.");
    } catch (e: any) {
      setError(String(e));
    } finally {
      setGenerating(false);
      setProgress(null);
    }
  }

  // tag → sorted asset ids carrying it, plus per-tag counts and total tagged.
  const { idsByTag, counts, totalTagged } = useMemo(() => {
    const idsByTag: Record<string, number[]> = {};
    const counts: Record<string, number> = {};
    Object.entries(assignments).forEach(([assetKey, tags]) => {
      const id = Number(assetKey);
      (tags ?? []).forEach((t) => {
        (idsByTag[t] ??= []).push(id);
        counts[t] = (counts[t] ?? 0) + 1;
      });
    });
    Object.values(idsByTag).forEach((ids) => ids.sort((a, b) => a - b));
    return { idsByTag, counts, totalTagged: Object.keys(assignments).length };
  }, [assignments]);

  // assetId → its scan rows (an asset can be used at several instances).
  const rowsByAsset = useMemo(() => {
    const map: Record<string, webapp.ScanRow[]> = {};
    scanRows.forEach((r) => (map[String(r.assetId)] ??= []).push(r));
    return map;
  }, [scanRows]);

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

  // Tags with at least one tagged asset, in library order, plus any tagged-but-
  // unlisted names appended so the summary matches what the PDF will render.
  const orderedNames = defs.map((d) => d.name);
  const extraNames = Object.keys(counts).filter((n) => !orderedNames.includes(n)).sort();
  const sections = [...orderedNames, ...extraNames].filter((n) => (counts[n] ?? 0) > 0);

  function toggle(name: string) {
    setExpanded((prev) => ({ ...prev, [name]: !prev[name] }));
  }

  // One-line breakdown for a single instance row, reusing the same pre-formatted
  // display fields as the Asset Breakdown tab so sizes stay consistent.
  function breakdown(r: webapp.ScanRow): string {
    return [
      r.assetTypeName,
      r.sizeDisplay,
      r.gpuMemoryDisplay ? `${r.gpuMemoryDisplay} GPU` : "",
      r.meshTriangles ? `${r.meshTriangles.toLocaleString()} tris` : "",
    ]
      .filter((s) => s && s.trim() !== "")
      .join(" · ");
  }

  return (
    <div className="report-view tag-report">
      <div className="report-toolbar">
        <div className="report-heading">Tag Report</div>
        <span className="spacer" />
        <label className="report-field inline">
          <input
            type="checkbox"
            checked={embedImages}
            onChange={(e) => setEmbedImages(e.target.checked)}
          />
          Embed images
        </label>
        <button className="primary" disabled={generating || totalTagged === 0} onClick={generate}>
          {generating ? "Generating…" : "Generate PDF"}
        </button>
      </div>

      <div className="hint">
        Builds a PDF of every tagged instance found in the current file, grouped by tag. Tagging is
        global — assets you tagged in other files only appear here if they're also used in this one.
        Turn off “Embed images” for a much smaller text-only PDF.
      </div>

      {generating && (
        <div className="scan-progress">
          {progress && progress.total > 0
            ? `Resolving tagged assets… ${progress.done}/${progress.total}`
            : "Scanning file for tagged instances…"}
        </div>
      )}
      {error && <div className="status-line error">{error}</div>}
      {status && <div className="status-line ok">{status}</div>}

      {totalTagged === 0 ? (
        <div className="placeholder">
          <h2>No tagged instances</h2>
          <div>Right-click a row in Asset Breakdown and pick a tag to start building a report.</div>
        </div>
      ) : (
        <>
          <div className="stat-cards">
            <div className="stat-card accent">
              <div className="stat-value">{totalTagged}</div>
              <div className="stat-caption">Tagged assets (all files)</div>
            </div>
            <div className="stat-card">
              <div className="stat-value">{sections.length}</div>
              <div className="stat-caption">Tags in use</div>
            </div>
          </div>

          {scanRows.length === 0 && (
            <div className="hint">
              Process this file in the Asset Breakdown tab to see the exact instances each tag is
              applied to here. Until then the dropdowns list tagged asset ids only.
            </div>
          )}

          <table className="tag-report-table">
            <thead>
              <tr>
                <th>Tag</th>
                <th className="num">Tagged assets</th>
              </tr>
            </thead>
            <tbody>
              {sections.map((name) => {
                const def = defs.find((d) => d.name === name);
                const ids = idsByTag[name] ?? [];
                const isOpen = !!expanded[name];
                return (
                  <Fragment key={name}>
                    <tr
                      className="tag-report-row"
                      onClick={() => toggle(name)}
                      title={isOpen ? "Collapse" : "Show tagged instances"}
                    >
                      <td>
                        <span className="tag-report-caret">{isOpen ? "▼" : "▶"}</span>
                        <span className="tag-chip" style={{ borderColor: def?.color || "#888" }}>
                          <span className="tag-dot" style={{ background: def?.color || "#888" }} />
                          {name}
                        </span>
                        {def?.description ? <span className="hint"> — {def.description}</span> : null}
                      </td>
                      <td className="num">{counts[name] ?? 0}</td>
                    </tr>
                    {isOpen && (
                      <tr className="tag-report-detail">
                        <td colSpan={2}>
                          <div className="tagged-asset-list">
                            {ids.map((id) => {
                              const rows = rowsByAsset[String(id)] ?? [];
                              return (
                                <div key={id} className="tagged-asset">
                                  <div className="tagged-asset-head">
                                    <span className="tagged-asset-id">Asset {id}</span>
                                    <button
                                      className="tagged-asset-open"
                                      title="Open this asset in the Single Asset tab"
                                      onClick={(e) => {
                                        e.stopPropagation();
                                        onOpenInSingleAsset(id);
                                      }}
                                    >
                                      Open in Single Asset ↗
                                    </button>
                                  </div>
                                  {rows.length === 0 ? (
                                    <div className="hint tagged-instance-empty">
                                      Not used in the current file (tagged elsewhere).
                                    </div>
                                  ) : (
                                    rows.map((r, i) => (
                                      <div key={i} className="tagged-instance">
                                        <span className="tagged-instance-path" title={r.instancePath}>
                                          {r.instancePath || "—"}
                                        </span>
                                        <span className="tagged-instance-meta">{breakdown(r)}</span>
                                      </div>
                                    ))
                                  )}
                                </div>
                              );
                            })}
                          </div>
                        </td>
                      </tr>
                    )}
                  </Fragment>
                );
              })}
            </tbody>
          </table>
        </>
      )}
    </div>
  );
}
