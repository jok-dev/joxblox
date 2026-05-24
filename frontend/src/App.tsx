import { useEffect, useState } from "react";
import Header from "./components/Header";
import Sidebar from "./components/Sidebar";
import SettingsModal from "./components/SettingsModal";
import SingleAsset from "./components/SingleAsset";
import Scan from "./components/Scan";
import { NAV_TITLES } from "./nav";
import { webapp } from "../wailsjs/go/models";
import { GetStatus } from "../wailsjs/go/webapp/AuthService";
import { PickRBXLFile } from "../wailsjs/go/webapp/ScanService";

export default function App() {
  const [active, setActive] = useState("report");
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [authStatus, setAuthStatus] = useState<webapp.AuthStatus | null>(null);

  // Shared "current file" the file-consuming tabs (Asset Breakdown, Report,
  // Heatmap, Diff) read from, set via the global Load file… bar in the header.
  const [currentFile, setCurrentFile] = useState("");

  // Cross-tab navigation target: when set, the Single Asset tab auto-loads this
  // asset ID / URL. Cleared once consumed so re-selecting the same asset works.
  const [singleAssetTarget, setSingleAssetTarget] = useState("");

  useEffect(() => {
    GetStatus().then(setAuthStatus).catch(() => {});
  }, []);

  async function loadFile() {
    try {
      const path = await PickRBXLFile();
      if (path) setCurrentFile(path);
    } catch {
      // Picker cancelled or unavailable — leave the current file untouched.
    }
  }

  function openInSingleAsset(target: string | number) {
    setSingleAssetTarget(String(target));
    setActive("single-asset");
  }

  return (
    <div className="app">
      <Header
        authStatus={authStatus}
        placeFile={currentFile}
        onLoadFile={loadFile}
        onOpenSettings={() => setSettingsOpen(true)}
      />
      <div className="body">
        <Sidebar active={active} onSelect={setActive} />
        <main className="content">
          {active === "single-asset" ? (
            <SingleAsset
              target={singleAssetTarget}
              onTargetConsumed={() => setSingleAssetTarget("")}
            />
          ) : active === "scan" ? (
            <Scan
              filePath={currentFile}
              onPickFile={loadFile}
              onOpenInSingleAsset={openInSingleAsset}
            />
          ) : (
            <Placeholder title={NAV_TITLES[active] ?? active} currentFile={currentFile} />
          )}
        </main>
      </div>
      {settingsOpen && (
        <SettingsModal
          onClose={() => setSettingsOpen(false)}
          onAuthChanged={setAuthStatus}
        />
      )}
    </div>
  );
}

function Placeholder({ title, currentFile }: { title: string; currentFile: string }) {
  return (
    <div className="placeholder">
      <h2>{title}</h2>
      <div>This tab is being ported from the Fyne UI to the new web frontend.</div>
      {currentFile && (
        <div className="hint">
          Current file: {currentFile.split(/[\\/]/).pop()}
        </div>
      )}
      <div className="hint">
        The grouped navigation, header, and Settings (⚙, top-right) — including
        live .ROBLOSECURITY auth — are wired to the Go backend.
      </div>
    </div>
  );
}
