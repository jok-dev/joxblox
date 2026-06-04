import { useMemo, useState } from "react";
import { webapp } from "../../wailsjs/go/models";
import { Compare, CopyInstance, PickFile } from "../../wailsjs/go/webapp/DiffService";

// Status → badge glyph + colour, mirroring the Fyne diff tab.
const STATUS_BADGE: Record<string, { glyph: string; color: string }> = {
  added: { glyph: "+", color: "#37a855" },
  removed: { glyph: "-", color: "#cc3d3d" },
  changed: { glyph: "~", color: "#cea02a" },
};

type Filter = {
  query: string;
  showAdded: boolean;
  showRemoved: boolean;
  showChanged: boolean;
};

const DEFAULT_FILTER: Filter = {
  query: "",
  showAdded: true,
  showRemoved: true,
  showChanged: true,
};

type Counts = { added: number; removed: number; changed: number };

// TreeNode is one node in the indented instance tree. Leaf nodes carry an
// entry; intermediate folder nodes exist only as ancestors of differing
// descendants (entry == null) but still roll up aggregate counts.
type TreeNode = {
  path: string;
  segment: string;
  entry: webapp.DiffEntry | null;
  children: string[];
  counts: Counts;
  matchSelf: boolean;
  matchSubtree: boolean;
};

function lastSegment(path: string): string {
  const i = path.lastIndexOf(".");
  return i >= 0 ? path.slice(i + 1) : path;
}

function parentPath(path: string): string {
  const i = path.lastIndexOf(".");
  return i >= 0 ? path.slice(0, i) : "";
}

// buildTree walks the flat entry list and produces one node per path (entries
// plus every ancestor segment), parent→children edges, and counts rolled up
// bottom-up so folders can show "+12 -3 ~5" without re-walking the subtree.
function buildTree(entries: webapp.DiffEntry[]): Map<string, TreeNode> {
  const nodes = new Map<string, TreeNode>();
  const ensure = (path: string): TreeNode => {
    let node = nodes.get(path);
    if (!node) {
      node = {
        path,
        segment: lastSegment(path),
        entry: null,
        children: [],
        counts: { added: 0, removed: 0, changed: 0 },
        matchSelf: false,
        matchSubtree: false,
      };
      nodes.set(path, node);
    }
    return node;
  };
  ensure("");

  for (const entry of entries) {
    const segments = entry.path ? entry.path.split(".") : [];
    let cumulative = "";
    for (const seg of segments) {
      cumulative = cumulative ? `${cumulative}.${seg}` : seg;
      ensure(cumulative);
    }
    if (entry.path) ensure(entry.path).entry = entry;
  }

  // Wire children, then sort each list once.
  for (const [path] of nodes) {
    if (path === "") continue;
    nodes.get(parentPath(path))!.children.push(path);
  }
  for (const node of nodes.values()) node.children.sort();

  // Roll up counts child-before-parent (longest paths first).
  const ordered = [...nodes.keys()].filter((p) => p !== "").sort((a, b) => b.length - a.length);
  for (const path of ordered) {
    const node = nodes.get(path)!;
    if (node.entry) {
      const bucket = node.entry.status as keyof Counts;
      if (bucket in node.counts) node.counts[bucket]++;
    }
    const parent = nodes.get(parentPath(path));
    if (parent) {
      parent.counts.added += node.counts.added;
      parent.counts.removed += node.counts.removed;
      parent.counts.changed += node.counts.changed;
    }
  }
  return nodes;
}

// applyFilter recomputes matchSelf / matchSubtree for every node from the
// current filter, using the same bottom-up ordering as the count roll-up.
function applyFilter(nodes: Map<string, TreeNode>, filter: Filter) {
  const query = filter.query.trim().toLowerCase();
  const entryMatches = (entry: webapp.DiffEntry): boolean => {
    if (entry.status === "added" && !filter.showAdded) return false;
    if (entry.status === "removed" && !filter.showRemoved) return false;
    if (entry.status === "changed" && !filter.showChanged) return false;
    if (query === "") return true;
    return (
      entry.path.toLowerCase().includes(query) ||
      entry.class.toLowerCase().includes(query) ||
      entry.name.toLowerCase().includes(query)
    );
  };

  for (const node of nodes.values()) {
    node.matchSelf = node.entry ? entryMatches(node.entry) : false;
    node.matchSubtree = false;
  }
  const ordered = [...nodes.keys()].filter((p) => p !== "").sort((a, b) => b.length - a.length);
  for (const path of ordered) {
    const node = nodes.get(path)!;
    if (node.matchSelf) {
      node.matchSubtree = true;
      continue;
    }
    node.matchSubtree = node.children.some((c) => nodes.get(c)!.matchSubtree);
  }
}

function formatCounts(c: Counts): string {
  const parts: string[] = [];
  if (c.added > 0) parts.push(`+${c.added}`);
  if (c.removed > 0) parts.push(`-${c.removed}`);
  if (c.changed > 0) parts.push(`~${c.changed}`);
  return parts.join(" ");
}

function baseName(path: string): string {
  return path.split(/[\\/]/).pop() ?? path;
}

type VisibleRow = { node: TreeNode; depth: number; isBranch: boolean; isOpen: boolean };

// A node menu target: either a real diff entry or an intermediate folder that
// exists in both files (so both-sides copy is offered).
type CopyTarget = { path: string; status: string | null; x: number; y: number };

export default function Diff() {
  const [fileA, setFileA] = useState("");
  const [fileB, setFileB] = useState("");
  const [ignoreScripts, setIgnoreScripts] = useState(true);

  const [comparing, setComparing] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [result, setResult] = useState<webapp.DiffResultPayload | null>(null);

  const [filter, setFilter] = useState<Filter>(DEFAULT_FILTER);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selected, setSelected] = useState<webapp.DiffEntry | null>(null);
  const [menu, setMenu] = useState<CopyTarget | null>(null);

  const searchActive = filter.query.trim() !== "";

  // Rebuild the tree + filter whenever the result or filter changes.
  const nodes = useMemo(() => {
    if (!result) return null;
    const built = buildTree(result.entries ?? []);
    applyFilter(built, filter);
    return built;
  }, [result, filter]);

  // Flatten the tree into the currently-visible rows. A branch is open when the
  // user expanded it, or automatically while a search is active.
  const visibleRows = useMemo<VisibleRow[]>(() => {
    if (!nodes) return [];
    const rows: VisibleRow[] = [];
    const walk = (path: string, depth: number) => {
      const node = nodes.get(path);
      if (!node) return;
      for (const childPath of node.children) {
        const child = nodes.get(childPath)!;
        if (!child.matchSubtree) continue;
        const isBranch = child.children.some((c) => nodes.get(c)!.matchSubtree);
        const isOpen = isBranch && (expanded.has(childPath) || (searchActive && child.matchSubtree));
        rows.push({ node: child, depth, isBranch, isOpen });
        if (isOpen) walk(childPath, depth + 1);
      }
    };
    walk("", 0);
    return rows;
  }, [nodes, expanded, searchActive]);

  async function pick(side: "a" | "b") {
    try {
      const path = await PickFile();
      if (!path) return;
      if (side === "a") setFileA(path);
      else setFileB(path);
    } catch {
      // Picker cancelled / unavailable — leave the current selection.
    }
  }

  async function runCompare() {
    if (comparing) return;
    if (!fileA || !fileB) {
      setError("Select both File A and File B first.");
      return;
    }
    setComparing(true);
    setError("");
    setNotice("");
    setResult(null);
    setSelected(null);
    setExpanded(new Set());
    try {
      const payload = await Compare(fileA, fileB, ignoreScripts);
      setResult(payload);
    } catch (e: any) {
      setError(String(e));
    } finally {
      setComparing(false);
    }
  }

  function toggle(path: string) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  }

  function onRowClick(row: VisibleRow) {
    if (row.node.entry) setSelected(row.node.entry);
    else if (row.isBranch) toggle(row.node.path);
  }

  async function copyInstance(side: "a" | "b", path: string) {
    setMenu(null);
    setNotice("");
    try {
      await CopyInstance(side, path);
      setNotice(`Copied ${path} from ${side === "a" ? baseName(fileA) : baseName(fileB)} to clipboard.`);
    } catch (e: any) {
      setError(String(e));
    }
  }

  // The copy menu offers the side(s) where the instance actually exists.
  function copyButtons(target: CopyTarget) {
    const sides: ("a" | "b")[] =
      target.status === "added"
        ? ["b"]
        : target.status === "removed"
        ? ["a"]
        : ["a", "b"]; // changed or intermediate folder → both sides exist
    return sides.map((side) => (
      <button
        key={side}
        className="tag-option"
        onClick={() => copyInstance(side, target.path)}
      >
        Copy from {side === "a" ? baseName(fileA) : baseName(fileB)}
      </button>
    ));
  }

  const labelA = result?.fileA || baseName(fileA) || "A";
  const labelB = result?.fileB || baseName(fileB) || "B";

  return (
    <div className="diff-view">
      <div className="diff-toolbar">
        <div className="diff-pickers">
          <div className="diff-picker">
            <button onClick={() => pick("a")}>Select File A…</button>
            <span className="diff-picker-name" title={fileA}>
              {fileA ? baseName(fileA) : "(none)"}
            </span>
          </div>
          <div className="diff-picker">
            <button onClick={() => pick("b")}>Select File B…</button>
            <span className="diff-picker-name" title={fileB}>
              {fileB ? baseName(fileB) : "(none)"}
            </span>
          </div>
        </div>
        <div className="diff-actions">
          <label className="diff-check">
            <input
              type="checkbox"
              checked={ignoreScripts}
              onChange={(e) => setIgnoreScripts(e.target.checked)}
            />
            Ignore script files
          </label>
          <button className="primary" disabled={comparing} onClick={runCompare}>
            {comparing ? "Diffing…" : "Compare"}
          </button>
          <span className="diff-summary">
            {comparing
              ? "Diffing…"
              : result
              ? `+${result.added} added · -${result.removed} removed · ${result.changed} changed`
              : "No diff yet"}
          </span>
        </div>
      </div>

      {error && <div className="status-line error">{error}</div>}
      {notice && <div className="status-line ok">{notice}</div>}

      {result && (
        <div className="filter-bar diff-filter">
          <input
            type="text"
            className="field"
            placeholder="Filter by path / class / name…"
            value={filter.query}
            onChange={(e) => setFilter((f) => ({ ...f, query: e.target.value }))}
          />
          <label className="diff-check">
            <input
              type="checkbox"
              checked={filter.showAdded}
              onChange={(e) => setFilter((f) => ({ ...f, showAdded: e.target.checked }))}
            />
            Added
          </label>
          <label className="diff-check">
            <input
              type="checkbox"
              checked={filter.showRemoved}
              onChange={(e) => setFilter((f) => ({ ...f, showRemoved: e.target.checked }))}
            />
            Removed
          </label>
          <label className="diff-check">
            <input
              type="checkbox"
              checked={filter.showChanged}
              onChange={(e) => setFilter((f) => ({ ...f, showChanged: e.target.checked }))}
            />
            Changed
          </label>
        </div>
      )}

      {!result && !comparing && (
        <div className="scan-empty">
          <div className="scan-empty-title">Compare two files</div>
          <div className="hint">
            Pick a baseline (File A) and a target (File B) .rbxl/.rbxm file, then Compare to
            see added, removed, and changed instances.
          </div>
        </div>
      )}

      {result && nodes && (
        <div className="diff-split">
          <div className="diff-tree">
            {visibleRows.length === 0 ? (
              <div className="hint diff-empty-tree">No instances match the current filter.</div>
            ) : (
              visibleRows.map((row) => {
                const { node } = row;
                const badge = node.entry ? STATUS_BADGE[node.entry.status] : null;
                const isSelected = selected != null && node.entry === selected;
                return (
                  <div
                    key={node.path}
                    className={"diff-row" + (isSelected ? " diff-row-selected" : "")}
                    style={{ paddingLeft: 6 + row.depth * 14 }}
                    onClick={() => onRowClick(row)}
                    onContextMenu={(e) => {
                      e.preventDefault();
                      setMenu({
                        path: node.path,
                        status: node.entry ? node.entry.status : null,
                        x: e.clientX,
                        y: e.clientY,
                      });
                    }}
                  >
                    <span
                      className="diff-caret"
                      onClick={(e) => {
                        if (row.isBranch) {
                          e.stopPropagation();
                          toggle(node.path);
                        }
                      }}
                    >
                      {row.isBranch ? (row.isOpen ? "▾" : "▸") : ""}
                    </span>
                    <span className="diff-badge" style={{ color: badge?.color }}>
                      {badge?.glyph ?? ""}
                    </span>
                    <span className="diff-name">
                      {node.entry && node.entry.name && node.entry.name !== node.segment
                        ? node.entry.name
                        : node.segment}
                    </span>
                    {node.entry?.class && <span className="diff-class">({node.entry.class})</span>}
                    {formatCounts(node.counts) && (
                      <span className="diff-counts">{formatCounts(node.counts)}</span>
                    )}
                  </div>
                );
              })
            )}
          </div>

          <div className="diff-detail">
            <PropertyPane entry={selected} labelA={labelA} labelB={labelB} />
          </div>
        </div>
      )}

      {menu && (
        <>
          <div className="context-menu-backdrop" onClick={() => setMenu(null)} />
          <div className="context-menu" style={{ left: menu.x, top: menu.y }}>
            <div className="context-menu-title">{baseName(menu.path)}</div>
            <div className="tag-picker-list">{copyButtons(menu)}</div>
          </div>
        </>
      )}
    </div>
  );
}

function PropertyPane({
  entry,
  labelA,
  labelB,
}: {
  entry: webapp.DiffEntry | null;
  labelA: string;
  labelB: string;
}) {
  if (!entry) {
    return <div className="diff-detail-empty hint">Select a row to view properties.</div>;
  }

  if (entry.status === "changed") {
    return (
      <>
        <div className="diff-detail-header" title={entry.path}>
          Changed: {entry.path} ({entry.class})
        </div>
        <table className="diff-prop-table">
          <thead>
            <tr>
              <th>Property</th>
              <th>{labelA}</th>
              <th>{labelB}</th>
            </tr>
          </thead>
          <tbody>
            {entry.properties.length === 0 ? (
              <tr>
                <td colSpan={3} className="hint">
                  (no property changes)
                </td>
              </tr>
            ) : (
              entry.properties.map((p, i) => (
                <tr key={`${p.name}-${i}`} className="diff-prop-changed">
                  <td className="diff-prop-name">{p.name}</td>
                  <td>
                    <ValueCell value={p.a} />
                  </td>
                  <td>
                    <ValueCell value={p.b} />
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </>
    );
  }

  // Added / removed: single-sided. Added shows File B's value, removed File A's.
  const valueLabel = entry.status === "removed" ? labelA : labelB;
  const label = entry.status === "removed" ? "Removed" : "Added";
  return (
    <>
      <div className="diff-detail-header" title={entry.path}>
        {label} in {valueLabel}: {entry.path} ({entry.class})
      </div>
      <table className="diff-prop-table">
        <thead>
          <tr>
            <th>Property</th>
            <th>{valueLabel}</th>
          </tr>
        </thead>
        <tbody>
          {entry.properties.length === 0 ? (
            <tr>
              <td colSpan={2} className="hint">
                (no properties)
              </td>
            </tr>
          ) : (
            entry.properties.map((p, i) => (
              <tr key={`${p.name}-${i}`}>
                <td className="diff-prop-name">{p.name}</td>
                <td>
                  <ValueCell value={p.a} />
                </td>
              </tr>
            ))
          )}
        </tbody>
      </table>
    </>
  );
}

function ValueCell({ value }: { value: webapp.DiffValue }) {
  if (value.absent) return <span className="diff-value-absent">(absent)</span>;
  if (value.colorHex) {
    return (
      <span className="diff-value-color">
        <span className="diff-swatch" style={{ background: value.colorHex }} />
        <span className="diff-value-mono">{value.display}</span>
      </span>
    );
  }
  return <span className="diff-value-mono">{value.display}</span>;
}
