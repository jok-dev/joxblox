import { ReactNode, useEffect, useRef, useState } from "react";
import { webapp } from "../../wailsjs/go/models";

type Props = {
  authStatus: webapp.AuthStatus | null;
  placeFile: string;
  recentFiles: string[];
  onLoadFile: () => void;
  onSelectRecent: (path: string) => void;
  onOpenSettings: () => void;
  // datamodelControl is the global DataModel scope selector, rendered next to
  // the file bar. Null when the current file isn't a scopable .rbxl place.
  datamodelControl?: ReactNode;
};

function authLabel(status: webapp.AuthStatus | null): string {
  if (!status) return "Auth: …";
  if (status.message) return `Auth: ${status.message}`;
  return status.enabled ? (status.saved ? "Auth: Saved" : "Auth: Enabled") : "Auth: Disabled";
}

function fileName(path: string): string {
  return path.split(/[\\/]/).pop() || path;
}

export default function Header({
  authStatus,
  placeFile,
  recentFiles,
  onLoadFile,
  onSelectRecent,
  onOpenSettings,
  datamodelControl,
}: Props) {
  const authOn = !!authStatus?.enabled;
  const [open, setOpen] = useState(false);
  const fileBarRef = useRef<HTMLDivElement>(null);

  // Close the recents dropdown on outside click or Escape.
  useEffect(() => {
    if (!open) return;
    function onPointerDown(e: MouseEvent) {
      if (!fileBarRef.current?.contains(e.target as Node)) setOpen(false);
    }
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  const hasRecents = recentFiles.length > 0;

  return (
    <header className="header">
      <div className="brand">joxblox</div>
      <div className="file-bar" ref={fileBarRef} title={placeFile || "No file selected"}>
        <button
          className="file-pick"
          disabled={!hasRecents}
          onClick={() => hasRecents && setOpen((v) => !v)}
          title={hasRecents ? "Pick a recently analyzed file" : "No previously analyzed files"}
        >
          <span className="name">{placeFile ? fileName(placeFile) : "No file selected"}</span>
          {hasRecents && <span className="caret">▾</span>}
        </button>
        <button onClick={onLoadFile} title="Add a Roblox place or model file for all tabs">
          Add file…
        </button>
        {open && (
          <ul className="file-menu" role="listbox">
            <li className="file-menu-title">Recently analyzed</li>
            {recentFiles.map((path) => (
              <li
                key={path}
                role="option"
                aria-selected={path === placeFile}
                className={"file-menu-item" + (path === placeFile ? " active" : "")}
                title={path}
                onClick={() => {
                  setOpen(false);
                  if (path !== placeFile) onSelectRecent(path);
                }}
              >
                <span className="file-menu-name">{fileName(path)}</span>
                <span className="file-menu-path">{path}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
      {datamodelControl}
      <div className="spacer" />
      <div className="right">
        <div className="auth-status">
          <span className={"dot" + (authOn ? " on" : "")} />
          <span>{authLabel(authStatus)}</span>
        </div>
        <button className="icon" title="Settings" onClick={onOpenSettings}>
          ⚙
        </button>
      </div>
    </header>
  );
}
