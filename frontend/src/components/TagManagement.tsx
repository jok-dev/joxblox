import { useEffect, useState } from "react";
import {
  GetAssignments,
  ListTagDefinitions,
  RenameTag,
  SaveTagDefinitions,
} from "../../wailsjs/go/webapp/TagService";

// Internal editable row. id is a stable local handle so renames can be detected
// across reorders; originalName is the name as loaded (undefined for rows the
// user just added) and drives the RenameTag-vs-prune decision on save.
type Row = {
  id: number;
  name: string;
  description: string;
  color: string;
  originalName?: string;
};

const NEW_TAG_COLORS = ["#e06c75", "#e5c07b", "#61afef", "#98c379", "#c678dd", "#56b6c2"];

// Suggested optimization labels offered as one-click adds when missing. Mirrors
// the backend presets so the user can rebuild them after deleting.
const SUGGESTIONS: { name: string; description: string; color: string }[] = [
  { name: "Decimate", description: "Reduce mesh triangle count", color: "#e06c75" },
  { name: "Reduce", description: "Downscale texture resolution", color: "#e5c07b" },
  { name: "Duplicate", description: "Redundant copy — dedupe", color: "#61afef" },
  { name: "Atlas", description: "Combine into a texture atlas", color: "#98c379" },
  { name: "Remove Alpha", description: "Drop unused alpha channel", color: "#c678dd" },
  { name: "Remove", description: "Delete / unused asset", color: "#abb2bf" },
];

let nextId = 1;

export default function TagManagement() {
  const [rows, setRows] = useState<Row[]>([]);
  const [counts, setCounts] = useState<Record<string, number>>({});
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    void load();
  }, []);

  async function load() {
    try {
      const defs = await ListTagDefinitions();
      setRows(
        (defs ?? []).map((d) => ({
          id: nextId++,
          name: d.name,
          description: d.description ?? "",
          color: d.color || "#888888",
          originalName: d.name,
        }))
      );
      const assignments = await GetAssignments();
      const tally: Record<string, number> = {};
      Object.values(assignments ?? {}).forEach((tags) =>
        (tags ?? []).forEach((t) => (tally[t] = (tally[t] ?? 0) + 1))
      );
      setCounts(tally);
    } catch (e: any) {
      setError(String(e));
    }
  }

  function update(id: number, patch: Partial<Row>) {
    setRows((prev) => prev.map((r) => (r.id === id ? { ...r, ...patch } : r)));
    setStatus("");
  }

  function remove(id: number) {
    setRows((prev) => prev.filter((r) => r.id !== id));
    setStatus("");
  }

  function move(id: number, dir: -1 | 1) {
    setRows((prev) => {
      const i = prev.findIndex((r) => r.id === id);
      const j = i + dir;
      if (i < 0 || j < 0 || j >= prev.length) return prev;
      const copy = [...prev];
      [copy[i], copy[j]] = [copy[j], copy[i]];
      return copy;
    });
    setStatus("");
  }

  function addRow(seed?: { name: string; description: string; color: string }) {
    setRows((prev) => [
      ...prev,
      {
        id: nextId++,
        name: seed?.name ?? "",
        description: seed?.description ?? "",
        color: seed?.color ?? NEW_TAG_COLORS[prev.length % NEW_TAG_COLORS.length],
      },
    ]);
    setStatus("");
  }

  async function save() {
    setSaving(true);
    setError("");
    setStatus("");
    try {
      // Migrate assignments for any in-place rename first, so renamed tags keep
      // their tagged instances (a bare SaveTagDefinitions would prune them).
      for (const r of rows) {
        const name = r.name.trim();
        if (r.originalName && name && r.originalName !== name) {
          await RenameTag(r.originalName, name);
        }
      }
      const defs = rows
        .filter((r) => r.name.trim() !== "")
        .map((r) => ({ name: r.name.trim(), description: r.description.trim(), color: r.color }));
      await SaveTagDefinitions(defs);
      await load();
      setStatus("Saved.");
    } catch (e: any) {
      setError(String(e));
    } finally {
      setSaving(false);
    }
  }

  const existingNames = new Set(rows.map((r) => r.name.trim().toLowerCase()));
  const missingSuggestions = SUGGESTIONS.filter((s) => !existingNames.has(s.name.toLowerCase()));

  return (
    <div className="tag-mgmt">
      <div className="report-toolbar">
        <div className="report-heading">Tag Management</div>
        <span className="spacer" />
        <button onClick={() => addRow()}>+ Add tag</button>
        <button className="primary" disabled={saving} onClick={save}>
          {saving ? "Saving…" : "Save changes"}
        </button>
      </div>

      <div className="hint">
        Tags are the labels you apply to instances in Asset Breakdown (right-click a row). Order
        here sets the section order in the Tag Report. Deleting a tag and saving removes it from
        every tagged instance.
      </div>

      {error && <div className="status-line error">{error}</div>}
      {status && <div className="status-line ok">{status}</div>}

      <table className="tag-mgmt-table">
        <thead>
          <tr>
            <th className="tag-col-color">Color</th>
            <th className="tag-col-name">Name</th>
            <th>Suggestion / how to optimize</th>
            <th className="tag-col-count num">Tagged</th>
            <th className="tag-col-actions" />
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={r.id}>
              <td className="tag-col-color">
                <input
                  type="color"
                  value={r.color}
                  onChange={(e) => update(r.id, { color: e.target.value })}
                />
              </td>
              <td className="tag-col-name">
                <input
                  type="text"
                  className="field"
                  placeholder="Tag name"
                  value={r.name}
                  onChange={(e) => update(r.id, { name: e.target.value })}
                />
              </td>
              <td>
                <input
                  type="text"
                  className="field"
                  placeholder="Optional hint shown in suggestions"
                  value={r.description}
                  onChange={(e) => update(r.id, { description: e.target.value })}
                />
              </td>
              <td className="num">{r.originalName ? counts[r.originalName] ?? 0 : 0}</td>
              <td className="tag-col-actions">
                <button className="icon" title="Move up" disabled={i === 0} onClick={() => move(r.id, -1)}>
                  ↑
                </button>
                <button
                  className="icon"
                  title="Move down"
                  disabled={i === rows.length - 1}
                  onClick={() => move(r.id, 1)}
                >
                  ↓
                </button>
                <button className="icon danger" title="Delete tag" onClick={() => remove(r.id)}>
                  ✕
                </button>
              </td>
            </tr>
          ))}
          {rows.length === 0 && (
            <tr>
              <td colSpan={5} className="hint">
                No tags yet. Add one above, or pick a suggestion below.
              </td>
            </tr>
          )}
        </tbody>
      </table>

      {missingSuggestions.length > 0 && (
        <div className="tag-suggestions">
          <div className="section-title">Suggestions</div>
          <div className="tag-suggestion-chips">
            {missingSuggestions.map((s) => (
              <button
                key={s.name}
                className="tag-suggestion"
                title={s.description}
                onClick={() => addRow(s)}
              >
                <span className="tag-dot" style={{ background: s.color }} />+ {s.name}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
