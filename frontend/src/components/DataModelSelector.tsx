import { useEffect, useState } from "react";
import { webapp } from "../../wailsjs/go/models";
import { GetDataModelTree } from "../../wailsjs/go/webapp/ScanService";

type Props = {
  // filePath is the shared "current file"; the selector only renders for .rbxl
  // place files (models have no DataModel service hierarchy to scope).
  filePath: string;
  // scope is the active comma-separated service filter (empty = whole file),
  // owned by the parent so every file-consuming tab reads the same value.
  scope: string;
  onScopeChange: (scope: string) => void;
};

// MAIN_SERVICES limits the picker to the well-known top-level services that
// actually carry analyzable content, hiding the editor-only / tooling children
// that clutter a place file's root. Matched case-insensitively by name.
const MAIN_SERVICES = new Set(
  [
    "Workspace",
    "Lighting",
    "Players",
    "ReplicatedStorage",
    "ReplicatedFirst",
    "ServerStorage",
    "ServerScriptService",
    "StarterGui",
    "StarterPack",
    "StarterPlayer",
    "Teams",
    "SoundService",
    "MaterialService",
    "Chat",
    "TextChatService",
    "LocalizationService",
  ].map((name) => name.toLowerCase())
);

// parsePrefixes mirrors the backend's split on commas/semicolons/newlines.
function parsePrefixes(raw: string): string[] {
  return raw
    .split(/[\n,;]/)
    .map((s) => s.trim().toLowerCase())
    .filter((s) => s !== "");
}

export default function DataModelSelector({ filePath, scope, onScopeChange }: Props) {
  const [services, setServices] = useState<webapp.DataModelService[] | null>(null);
  const [treeLoading, setTreeLoading] = useState(false);
  const [treeError, setTreeError] = useState("");
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const isRbxl = filePath.toLowerCase().endsWith(".rbxl");

  // When the global file changes, drop the old tree and load the new one.
  useEffect(() => {
    setServices(null);
    setTreeError("");
    setOpen(false);
    setSelected(new Set());
    if (!filePath || !filePath.toLowerCase().endsWith(".rbxl")) return;
    void loadTree();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filePath]);

  async function loadTree() {
    setTreeLoading(true);
    setTreeError("");
    try {
      const list = await GetDataModelTree(filePath);
      // Show only the main services; fall back to the full list if a place's
      // root has none of them (unusual, e.g. a model saved as .rbxl).
      const main = list.filter((s) => MAIN_SERVICES.has(s.name.toLowerCase()));
      const shown = main.length > 0 ? main : list;
      setServices(shown);
      setSelected(new Set(shown.map((s) => s.name)));
    } catch (e: any) {
      setTreeError(String(e));
      setServices(null);
    } finally {
      setTreeLoading(false);
    }
  }

  function openDialog() {
    if (services) {
      const prefixes = parsePrefixes(scope);
      if (prefixes.length === 0) {
        setSelected(new Set(services.map((s) => s.name)));
      } else {
        setSelected(
          new Set(
            services
              .filter((s) => prefixes.some((p) => s.name.toLowerCase().startsWith(p)))
              .map((s) => s.name)
          )
        );
      }
    }
    setOpen(true);
  }

  function toggleService(name: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }

  function apply() {
    setOpen(false);
    let pf = "";
    // Only emit a filter when a strict subset is chosen; "all selected" means
    // analyze the whole file (empty scope).
    if (services && selected.size > 0 && selected.size < services.length) {
      pf = services
        .filter((s) => selected.has(s.name))
        .map((s) => s.name)
        .join(",");
    }
    onScopeChange(pf);
  }

  if (!isRbxl) return null;

  return (
    <div className="datamodel-control">
      <button
        className="dm-trigger"
        onClick={openDialog}
        disabled={treeLoading || !services}
        title={scope ? `Scoped to: ${scope}` : "Choose which DataModel services to analyze"}
      >
        {treeLoading ? "Loading datamodel…" : "Datamodel ▾"}
      </button>
      {scope && (
        <span className="scope-chip" title={scope}>
          {scope}
        </span>
      )}

      {open && (
        <div className="modal-backdrop" onMouseDown={() => setOpen(false)}>
          <div className="modal" onMouseDown={(e) => e.stopPropagation()}>
            <h2>Select DataModel parts to analyze</h2>
            <div className="hint">
              Checked services are included across every tab that analyzes this file. Uncheck the
              ones you don't need to speed up processing and cut noise.
            </div>
            {treeError && <div className="status-line error">{treeError}</div>}
            <div className="dm-actions-row">
              <button onClick={() => setSelected(new Set(services?.map((x) => x.name)))}>
                Select all
              </button>
              <button onClick={() => setSelected(new Set())}>Clear</button>
            </div>
            <div className="dm-list">
              {services?.map((svc) => (
                <label key={svc.name} className="dm-item">
                  <input
                    type="checkbox"
                    checked={selected.has(svc.name)}
                    onChange={() => toggleService(svc.name)}
                  />
                  <span className="dm-name">{svc.name}</span>
                  <span className="dm-class hint">{svc.className}</span>
                  <span className="dm-count hint">
                    {svc.instanceCount.toLocaleString()} instances
                  </span>
                </label>
              ))}
            </div>
            <div className="modal-actions">
              <button onClick={() => setOpen(false)}>Cancel</button>
              <button className="primary" disabled={selected.size === 0} onClick={apply}>
                Apply
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
