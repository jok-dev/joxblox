import { useState } from "react";

export type TagDef = { name: string; color: string; description: string };

// A default palette cycled through when the user creates a tag inline (so new
// tags get a distinct color without a color picker). Tag Management offers full
// control; this just avoids everything-grey.
const NEW_TAG_COLORS = ["#e06c75", "#e5c07b", "#61afef", "#98c379", "#c678dd", "#56b6c2"];

type Props = {
  // Screen position of the right-click that opened the picker.
  x: number;
  y: number;
  // Tag library to quick-select from.
  defs: TagDef[];
  // Tags currently applied to the target instance.
  applied: string[];
  // Free-form custom note currently saved on the target instance.
  note: string;
  // Toggle a known tag on/off for the instance.
  onToggle: (tag: string) => void;
  // Create a new library tag (with a suggested color) and apply it.
  onCreateAndApply: (def: TagDef) => void;
  // Persist the custom note for the instance (blank clears it).
  onSaveNote: (note: string) => void;
  onClose: () => void;
};

export default function TagPicker({
  x,
  y,
  defs,
  applied,
  note,
  onToggle,
  onCreateAndApply,
  onSaveNote,
  onClose,
}: Props) {
  const [draft, setDraft] = useState("");
  const [noteDraft, setNoteDraft] = useState(note);
  const appliedSet = new Set(applied);

  // Persist the note when it actually changed, so blurring without edits is a
  // no-op (avoids a needless disk write / refetch).
  function commitNote() {
    if (noteDraft.trim() !== note.trim()) onSaveNote(noteDraft);
  }

  function addDraft() {
    const name = draft.trim();
    if (!name) return;
    const existing = defs.find((d) => d.name.toLowerCase() === name.toLowerCase());
    if (existing) {
      // Typing an existing tag name just applies it rather than duplicating.
      if (!appliedSet.has(existing.name)) onToggle(existing.name);
    } else {
      const color = NEW_TAG_COLORS[defs.length % NEW_TAG_COLORS.length];
      onCreateAndApply({ name, color, description: "" });
    }
    setDraft("");
  }

  // Clamp so the menu doesn't spill off the right/bottom edges.
  const left = Math.min(x, window.innerWidth - 240);
  const top = Math.min(y, window.innerHeight - 420);

  return (
    <div className="context-menu-backdrop" onMouseDown={onClose}>
      <div
        className="context-menu tag-picker"
        style={{ left, top }}
        onMouseDown={(e) => e.stopPropagation()}
      >
        <div className="context-menu-title">Tag instance</div>
        <div className="tag-picker-list">
          {defs.length === 0 && (
            <div className="hint">No tags yet — create one below or in Tag Management.</div>
          )}
          {defs.map((d) => {
            const on = appliedSet.has(d.name);
            return (
              <button
                key={d.name}
                className={"tag-option" + (on ? " on" : "")}
                onClick={() => onToggle(d.name)}
              >
                <span className="tag-dot" style={{ background: d.color || "#888" }} />
                <span className="tag-option-name">{d.name}</span>
                <span className="tag-option-check">{on ? "✓" : ""}</span>
              </button>
            );
          })}
        </div>
        <div className="tag-picker-add">
          <input
            type="text"
            className="field"
            placeholder="New tag…"
            value={draft}
            autoFocus
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") addDraft();
              if (e.key === "Escape") onClose();
            }}
          />
          <button className="primary" disabled={!draft.trim()} onClick={addDraft}>
            Add
          </button>
        </div>
        <div className="tag-picker-note">
          <div className="context-menu-title">Custom note</div>
          <textarea
            className="field"
            placeholder="Add a custom note for this instance…"
            value={noteDraft}
            rows={3}
            onChange={(e) => setNoteDraft(e.target.value)}
            onBlur={commitNote}
            onKeyDown={(e) => {
              // Save + close on Cmd/Ctrl+Enter; Escape closes without saving.
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                commitNote();
                onClose();
              }
              if (e.key === "Escape") onClose();
            }}
          />
        </div>
      </div>
    </div>
  );
}
