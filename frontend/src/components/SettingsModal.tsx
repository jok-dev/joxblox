import { useEffect, useState } from "react";
import { webapp } from "../../wailsjs/go/models";
import {
  ApplyCookie,
  ClearCookie,
  GetCookie,
} from "../../wailsjs/go/webapp/AuthService";
import {
  GetSettings,
  SaveSettings,
} from "../../wailsjs/go/webapp/SettingsService";

type Props = {
  onClose: () => void;
  onAuthChanged: (status: webapp.AuthStatus) => void;
};

export default function SettingsModal({ onClose, onAuthChanged }: Props) {
  const [cookie, setCookie] = useState("");
  const [remember, setRemember] = useState(false);
  const [authBusy, setAuthBusy] = useState(false);
  const [authMsg, setAuthMsg] = useState<{ text: string; kind: "ok" | "error" | "" }>({ text: "", kind: "" });

  const [settings, setSettings] = useState<webapp.Settings | null>(null);
  const [settingsMsg, setSettingsMsg] = useState("");

  const [cookieHelpOpen, setCookieHelpOpen] = useState(false);

  useEffect(() => {
    GetCookie().then((c) => {
      setCookie(c);
      setRemember(!!c);
    });
    GetSettings().then(setSettings);
  }, []);

  async function applyAuth() {
    setAuthBusy(true);
    setAuthMsg({ text: "", kind: "" });
    try {
      const status = await ApplyCookie(cookie, remember);
      onAuthChanged(status);
      setAuthMsg({ text: status.enabled ? "Applied." : "Cleared.", kind: "ok" });
    } catch (e: any) {
      setAuthMsg({ text: String(e), kind: "error" });
    } finally {
      setAuthBusy(false);
    }
  }

  async function clearAuth() {
    setAuthBusy(true);
    setAuthMsg({ text: "", kind: "" });
    try {
      const status = await ClearCookie();
      onAuthChanged(status);
      setCookie("");
      setRemember(false);
      setAuthMsg({ text: "Cleared.", kind: "ok" });
    } catch (e: any) {
      setAuthMsg({ text: String(e), kind: "error" });
    } finally {
      setAuthBusy(false);
    }
  }

  async function saveSettings() {
    if (!settings) return;
    setSettingsMsg("");
    try {
      await SaveSettings(settings);
      setSettingsMsg("Settings saved.");
    } catch (e: any) {
      setSettingsMsg(String(e));
    }
  }

  function patch(p: Partial<webapp.Settings>) {
    setSettings((s) => (s ? webapp.Settings.createFrom({ ...s, ...p }) : s));
  }

  return (
    <div className="modal-backdrop" onMouseDown={onClose}>
      <div className="modal" onMouseDown={(e) => e.stopPropagation()}>
        <h2>Settings</h2>

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
              placeholder="Optional .ROBLOSECURITY cookie value"
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
          <div className="row">
            <button className="primary" disabled={authBusy} onClick={applyAuth}>
              {authBusy ? "Validating…" : "Apply Auth"}
            </button>
            <button disabled={authBusy} onClick={clearAuth}>Clear Auth</button>
          </div>
          <div className="hint">
            This cookie grants account access — treat it like a password. With
            “Save to keychain” it’s stored in your OS secure credential store,
            never in plaintext files.
          </div>
          <div className={"status-line " + authMsg.kind}>{authMsg.text}</div>
        </div>

        <hr className="divider" />

        <div className="section">
          <div className="section-title">General</div>
          {settings && (
            <>
              <div className="row">
                <label>Asset download cache</label>
                <input
                  type="checkbox"
                  checked={settings.cacheEnabled}
                  onChange={(e) => patch({ cacheEnabled: e.target.checked })}
                />
              </div>
              <div className="row">
                <label>Cache folder</label>
                <input
                  type="text"
                  value={settings.cacheFolder}
                  placeholder="Folder for cached asset bytes"
                  onChange={(e) => patch({ cacheFolder: e.target.value })}
                />
              </div>
              <div className="row">
                <label>Roblox Studio path</label>
                <input
                  type="text"
                  value={settings.studioPath}
                  placeholder="Path to RobloxStudioBeta.exe"
                  onChange={(e) => patch({ studioPath: e.target.value })}
                />
              </div>
              <div className="row">
                <label>3D look sensitivity</label>
                <input
                  type="range"
                  min={0.05}
                  max={1}
                  step={0.01}
                  value={settings.meshLookSensitivity}
                  onChange={(e) => patch({ meshLookSensitivity: parseFloat(e.target.value) })}
                />
                <span className="hint">{settings.meshLookSensitivity.toFixed(2)}</span>
              </div>
              <div className="status-line ok">{settingsMsg}</div>
            </>
          )}
        </div>

        <div className="modal-actions">
          <button onClick={saveSettings}>Save General</button>
          <button className="primary" onClick={onClose}>Close</button>
        </div>
      </div>
    </div>
  );
}
