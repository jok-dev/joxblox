import { webapp } from "../../wailsjs/go/models";

type Props = {
  authStatus: webapp.AuthStatus | null;
  placeFile: string;
  onOpenSettings: () => void;
};

function authLabel(status: webapp.AuthStatus | null): string {
  if (!status) return "Auth: …";
  if (status.message) return `Auth: ${status.message}`;
  return status.enabled ? (status.saved ? "Auth: Saved" : "Auth: Enabled") : "Auth: Disabled";
}

export default function Header({ authStatus, placeFile, onOpenSettings }: Props) {
  const authOn = !!authStatus?.enabled;
  return (
    <header className="header">
      <div className="brand">joxblox</div>
      <div className="file-bar" title="Shared place file (loading hookup in progress)">
        <span className="name">{placeFile || "No place file loaded"}</span>
        <button disabled title="Coming soon — file loading binding is being ported">
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
