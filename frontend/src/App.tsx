import { useEffect, useState } from "react";
import Header from "./components/Header";
import Sidebar from "./components/Sidebar";
import SettingsModal from "./components/SettingsModal";
import AuthGate from "./components/AuthGate";
import SingleAsset from "./components/SingleAsset";
import Scan from "./components/Scan";
import Report from "./components/Report";
import TagManagement from "./components/TagManagement";
import TagReport from "./components/TagReport";
import DataModelSelector from "./components/DataModelSelector";
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

  // Recently analyzed files, persisted across sessions so the header file bar
  // can offer a quick-pick dropdown of previously loaded places.
  const [recentFiles, setRecentFiles] = useState<string[]>(loadRecentFiles);

  // Cross-tab navigation target: when set, the Single Asset tab auto-loads this
  // asset ID / URL. Cleared once consumed so re-selecting the same asset works.
  const [singleAssetTarget, setSingleAssetTarget] = useState("");

  // Global DataModel scope: a comma-separated list of top-level services to
  // analyze (empty = whole file), chosen via the header selector and applied by
  // every file-consuming tab. Reset whenever the file changes.
  const [scope, setScope] = useState("");

  useEffect(() => {
    GetStatus().then(setAuthStatus).catch(() => {});
  }, []);

  // Set the current file and bump it to the front of the recent-files list.
  function selectFile(path: string) {
    setCurrentFile(path);
    setScope("");
    setRecentFiles((prev) => {
      const next = [path, ...prev.filter((p) => p !== path)].slice(0, MAX_RECENT_FILES);
      saveRecentFiles(next);
      return next;
    });
  }

  async function loadFile() {
    try {
      const path = await PickRBXLFile();
      if (path) selectFile(path);
    } catch {
      // Picker cancelled or unavailable — leave the current file untouched.
    }
  }

  function openInSingleAsset(target: string | number) {
    setSingleAssetTarget(String(target));
    setActive("single-asset");
  }

  // Auth is required before any feature is reachable. While the initial status
  // is loading, render nothing; once known, gate the whole app behind AuthGate
  // until a valid cookie enables authentication.
  if (authStatus === null) {
    return <div className="app" />;
  }
  if (!authStatus.enabled) {
    return <AuthGate onAuthChanged={setAuthStatus} />;
  }

  return (
    <div className="app">
      <Header
        authStatus={authStatus}
        placeFile={currentFile}
        recentFiles={recentFiles}
        onLoadFile={loadFile}
        onSelectRecent={selectFile}
        onOpenSettings={() => setSettingsOpen(true)}
        datamodelControl={
          <DataModelSelector filePath={currentFile} scope={scope} onScopeChange={setScope} />
        }
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
              pathFilter={scope}
              onPickFile={loadFile}
              onOpenInSingleAsset={openInSingleAsset}
            />
          ) : active === "report" ? (
            <Report filePath={currentFile} scope={scope} onPickFile={loadFile} />
          ) : active === "tag-management" ? (
            <TagManagement />
          ) : active === "tag-report" ? (
            <TagReport filePath={currentFile} onPickFile={loadFile} />
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

const RECENT_FILES_KEY = "joxblox.recentFiles";
const MAX_RECENT_FILES = 10;

function loadRecentFiles(): string[] {
  try {
    const raw = localStorage.getItem(RECENT_FILES_KEY);
    const parsed = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed) ? parsed.filter((p) => typeof p === "string") : [];
  } catch {
    return [];
  }
}

function saveRecentFiles(files: string[]) {
  try {
    localStorage.setItem(RECENT_FILES_KEY, JSON.stringify(files));
  } catch {
    // Storage unavailable (private mode / quota) — recents just won't persist.
  }
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
