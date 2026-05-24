import { useEffect, useState } from "react";
import Header from "./components/Header";
import Sidebar from "./components/Sidebar";
import SettingsModal from "./components/SettingsModal";
import SingleAsset from "./components/SingleAsset";
import Scan from "./components/Scan";
import { NAV_TITLES } from "./nav";
import { webapp } from "../wailsjs/go/models";
import { GetStatus } from "../wailsjs/go/webapp/AuthService";

export default function App() {
  const [active, setActive] = useState("report");
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [authStatus, setAuthStatus] = useState<webapp.AuthStatus | null>(null);

  useEffect(() => {
    GetStatus().then(setAuthStatus).catch(() => {});
  }, []);

  return (
    <div className="app">
      <Header
        authStatus={authStatus}
        placeFile=""
        onOpenSettings={() => setSettingsOpen(true)}
      />
      <div className="body">
        <Sidebar active={active} onSelect={setActive} />
        <main className="content">
          {active === "single-asset" ? (
            <SingleAsset />
          ) : active === "scan" ? (
            <Scan />
          ) : (
            <Placeholder title={NAV_TITLES[active] ?? active} />
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

function Placeholder({ title }: { title: string }) {
  return (
    <div className="placeholder">
      <h2>{title}</h2>
      <div>This tab is being ported from the Fyne UI to the new web frontend.</div>
      <div className="hint">
        The grouped navigation, header, and Settings (⚙, top-right) — including
        live .ROBLOSECURITY auth — are wired to the Go backend.
      </div>
    </div>
  );
}
