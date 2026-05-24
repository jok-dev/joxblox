import { webapp } from "../../wailsjs/go/models";

type Props = {
  authStatus: webapp.AuthStatus | null;
  placeFile: string;
  onLoadFile: () => void;
  onOpenSettings: () => void;
};

function authLabel(status: webapp.AuthStatus | null): string {
  if (!status) return "Auth: …";
  if (status.message) return `Auth: ${status.message}`;
  return status.enabled ? (status.saved ? "Auth: Saved" : "Auth: Enabled") : "Auth: Disabled";
}

function fileName(path: string): string {
  return path.split(/[\\/]/).pop() || path;
}

export default function Header({ authStatus, placeFile, onLoadFile, onOpenSettings }: Props) {
  const authOn = !!authStatus?.enabled;
  return (
    <header className="header">
      <div className="brand">joxblox</div>
      <div className="file-bar" title={placeFile || "No place file loaded"}>
        <span className="name">{placeFile ? fileName(placeFile) : "No place file loaded"}</span>
        <button onClick={onLoadFile} title="Load an .rbxl/.rbxm file for all tabs">
          Load file…
        </button>
      </div>
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
