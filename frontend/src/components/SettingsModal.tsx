import { useEffect, useState } from "react";
import { webapp } from "../../wailsjs/go/models";
import AuthPanel from "./AuthPanel";
import {
  GetSettings,
  SaveSettings,
} from "../../wailsjs/go/webapp/SettingsService";

type Props = {
  onClose: () => void;
  onAuthChanged: (status: webapp.AuthStatus) => void;
};

export default function SettingsModal({ onClose, onAuthChanged }: Props) {
  const [settings, setSettings] = useState<webapp.Settings | null>(null);
  const [settingsMsg, setSettingsMsg] = useState("");

  useEffect(() => {
    GetSettings().then(setSettings);
  }, []);

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

        <AuthPanel onAuthChanged={onAuthChanged} />

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
