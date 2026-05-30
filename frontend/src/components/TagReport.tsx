import { useEffect, useState } from "react";
import { webapp } from "../../wailsjs/go/models";
import {
  GenerateTagReportPDF,
  GetAssignments,
  ListTagDefinitions,
} from "../../wailsjs/go/webapp/TagService";
import { EventsOff, EventsOn } from "../../wailsjs/runtime/runtime";

type Props = {
  // filePath is the shared "current file" set via the global Load file… bar.
  filePath: string;
  // onPickFile opens the shared file picker (updates the global current file).
  onPickFile: () => void;
};

export default function TagReport({ filePath, onPickFile }: Props) {
  const [defs, setDefs] = useState<webapp.TagDefinition[]>([]);
  const [counts, setCounts] = useState<Record<string, number>>({});
  const [totalTagged, setTotalTagged] = useState(0);
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

  async function load() {
    try {
      const [d, assignments] = await Promise.all([ListTagDefinitions(), GetAssignments()]);
      setDefs(d ?? []);
      const tally: Record<string, number> = {};
      const a = assignments ?? {};
      Object.values(a).forEach((tags) =>
        (tags ?? []).forEach((t) => (tally[t] = (tally[t] ?? 0) + 1))
      );
      setCounts(tally);
      setTotalTagged(Object.keys(a).length);
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
                return (
                  <tr key={name}>
                    <td>
                      <span className="tag-chip" style={{ borderColor: def?.color || "#888" }}>
                        <span className="tag-dot" style={{ background: def?.color || "#888" }} />
                        {name}
                      </span>
                      {def?.description ? <span className="hint"> — {def.description}</span> : null}
                    </td>
                    <td className="num">{counts[name] ?? 0}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </>
      )}
    </div>
  );
}
