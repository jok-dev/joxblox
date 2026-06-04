import { useEffect, useRef, useState } from "react";
import { webapp } from "../../wailsjs/go/models";
import {
  ApplyCookie,
  ClearCookie,
  GetCookie,
} from "../../wailsjs/go/webapp/AuthService";

type Props = {
  onAuthChanged: (status: webapp.AuthStatus) => void;
  // The auth gate hides Clear (nothing to clear before you're in); Settings
  // keeps it for an explicit sign-out.
  showClear?: boolean;
};

// How long to wait after the last edit before validating, so we don't fire a
// network validation on every keystroke while a cookie is being pasted/typed.
const APPLY_DEBOUNCE_MS = 600;

// AuthPanel is the shared .ROBLOSECURITY cookie form used both by the Settings
// modal and the startup AuthGate, so both surfaces validate/persist identically.
// There's no apply button — editing the cookie (or toggling keychain) auto-
// validates after a short debounce.
export default function AuthPanel({ onAuthChanged, showClear = true }: Props) {
  const [cookie, setCookie] = useState("");
  const [remember, setRemember] = useState(false);
  const [authBusy, setAuthBusy] = useState(false);
  const [authMsg, setAuthMsg] = useState<{ text: string; kind: "ok" | "error" | "" }>({ text: "", kind: "" });
  const [cookieHelpOpen, setCookieHelpOpen] = useState(false);

  // Skip auto-apply for values we set programmatically (the prefill from the
  // backend on mount, and the clear-button reset) — those are already applied,
  // so re-validating them would be a wasted call.
  const skipNextApply = useRef(true);
  const debounceRef = useRef<number | null>(null);

  useEffect(() => {
    GetCookie().then((c) => {
      skipNextApply.current = true;
      setCookie(c);
      setRemember(!!c);
    });
  }, []);

  async function applyAuth(raw: string, save: boolean) {
    setAuthBusy(true);
    setAuthMsg({ text: "Validating…", kind: "" });
    try {
      const status = await ApplyCookie(raw, save);
      onAuthChanged(status);
      setAuthMsg({ text: status.enabled ? "Applied." : "Cleared.", kind: "ok" });
    } catch (e: any) {
      setAuthMsg({ text: String(e), kind: "error" });
    } finally {
      setAuthBusy(false);
    }
  }

  // Debounced auto-apply: whenever the user edits the cookie or toggles the
  // keychain option, validate and apply after a short pause.
  useEffect(() => {
    if (skipNextApply.current) {
      skipNextApply.current = false;
      return;
    }
    if (debounceRef.current) window.clearTimeout(debounceRef.current);
    debounceRef.current = window.setTimeout(() => applyAuth(cookie, remember), APPLY_DEBOUNCE_MS);
    return () => {
      if (debounceRef.current) window.clearTimeout(debounceRef.current);
    };
  }, [cookie, remember]);

  async function clearAuth() {
    if (debounceRef.current) window.clearTimeout(debounceRef.current);
    setAuthBusy(true);
    setAuthMsg({ text: "", kind: "" });
    try {
      const status = await ClearCookie();
      onAuthChanged(status);
      skipNextApply.current = true;
      setCookie("");
      setRemember(false);
      setAuthMsg({ text: "Cleared.", kind: "ok" });
    } catch (e: any) {
      setAuthMsg({ text: String(e), kind: "error" });
    } finally {
      setAuthBusy(false);
    }
  }

  return (
    <div className="section">
      <div className="section-title">
        Authentication (.ROBLOSECURITY)
        <button
          className="info-btn"
          title="How to get the .ROBLOSECURITY cookie"
          aria-expanded={cookieHelpOpen}
          onClick={() => setCookieHelpOpen((v) => !v)}
        >
          ⓘ
        </button>
      </div>

      {cookieHelpOpen && (
        <div className="help-box">
          <div className="help-title">Get your .ROBLOSECURITY cookie from Chrome</div>
          <ol>
            <li>Sign in to <code>roblox.com</code> in Chrome.</li>
            <li>Open DevTools: <code>⌥⌘I</code> (Mac) or <code>F12</code> (Windows), or right-click the page → <em>Inspect</em>.</li>
            <li>Go to the <strong>Application</strong> tab. In the left sidebar, expand <strong>Storage → Cookies</strong> and select <code>https://www.roblox.com</code>.</li>
            <li>Find the cookie named <code>.ROBLOSECURITY</code> and double-click its <strong>Value</strong>.</li>
            <li>Copy the entire value (it starts with <code>_|WARNING:-DO-NOT-SHARE-THIS…</code>) and paste it below.</li>
          </ol>
          <div className="hint">
            ⚠ This value logs in as your account. Never share it or paste it
            anywhere else. Roblox rotates it when you log out, so you may need
            to re-copy it later.
          </div>
        </div>
      )}

      <div className="row">
        <label>Auth Cookie</label>
        <input
          type="password"
          value={cookie}
          placeholder=".ROBLOSECURITY cookie value"
          onChange={(e) => setCookie(e.target.value)}
        />
      </div>
      <div className="row">
        <label>Save to keychain</label>
        <input
          type="checkbox"
          checked={remember}
          onChange={(e) => setRemember(e.target.checked)}
        />
      </div>
      {showClear && (
        <div className="row">
          <button disabled={authBusy} onClick={clearAuth}>Clear Auth</button>
        </div>
      )}
      <div className="hint">
        This cookie grants account access — treat it like a password. With
        “Save to keychain” it’s stored in your OS secure credential store,
        never in plaintext files.
      </div>
      <div className={"status-line " + authMsg.kind}>{authMsg.text}</div>
    </div>
  );
}
