import { webapp } from "../../wailsjs/go/models";
import AuthPanel from "./AuthPanel";

type Props = {
  onAuthChanged: (status: webapp.AuthStatus) => void;
};

// AuthGate blocks all features until a valid .ROBLOSECURITY cookie is applied.
// It's a non-dismissible front door rendered in place of the main UI whenever
// authentication is not enabled.
export default function AuthGate({ onAuthChanged }: Props) {
  return (
    <div className="auth-gate">
      <div className="modal">
        <div className="brand">joxblox</div>
        <h2>Sign in to continue</h2>
        <div className="hint">
          joxblox needs your Roblox account cookie to download and analyze
          assets. Add it below to unlock every feature.
        </div>
        <AuthPanel onAuthChanged={onAuthChanged} showClear={false} />
      </div>
    </div>
  );
}
