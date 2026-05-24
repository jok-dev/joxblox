import { useState } from "react";
import { webapp } from "../../wailsjs/go/models";
import { FetchAsset } from "../../wailsjs/go/webapp/AssetService";

type MetaRow = { label: string; value: string };

function metaRows(d: webapp.AssetDetails): MetaRow[] {
  const rows: MetaRow[] = [
    { label: "Asset Type", value: d.assetTypeDisplay },
    { label: d.dimensionsLabel || "Dimensions", value: d.dimensionsDisplay },
    { label: "Format", value: d.formatDisplay },
    { label: "Content Type", value: d.contentTypeDisplay },
    { label: "Self Size", value: d.selfSizeDisplay },
    { label: "Total Size", value: d.totalSizeDisplay },
    { label: "Referenced Assets", value: d.referencedDisplay },
    { label: "Source", value: d.sourceDisplay },
    { label: "State", value: d.stateDescription },
    { label: "SHA-256", value: d.fileSha256Display },
  ];
  return rows.filter((r) => r.value && r.value.trim() !== "");
}

type JsonSection = { title: string; json: string };

function jsonSections(d: webapp.AssetDetails): JsonSection[] {
  return [
    { title: "AssetDelivery", json: d.assetDeliveryJson },
    { title: "Thumbnail", json: d.thumbnailJson },
    { title: "Economy", json: d.economyJson },
    { title: "Rusty Asset Tool", json: d.rustyAssetToolJson },
  ].filter((s) => s.json && s.json.trim() !== "");
}

export default function SingleAsset() {
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState("Enter an asset ID or rbxthumb URL and click Go.");
  const [error, setError] = useState("");
  const [details, setDetails] = useState<webapp.AssetDetails | null>(null);

  async function load() {
    if (busy || input.trim() === "") return;
    setBusy(true);
    setError("");
    setStatus("Loading…");
    try {
      const d = await FetchAsset(input);
      setDetails(d);
      setStatus(d.statusMessage || "Loaded.");
    } catch (e: any) {
      setDetails(null);
      setStatus("");
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="asset-view">
      <div className="asset-input-row">
        <input
          type="text"
          value={input}
          placeholder="Paste an asset ID, rbxassetid URL, or rbxthumb URL"
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && load()}
        />
        <button className="primary" disabled={busy} onClick={load}>
          {busy ? "Loading…" : "Go"}
        </button>
      </div>

      {status && <div className="status-line">{status}</div>}
      {error && <div className="status-line error">{error}</div>}
      {details?.warningMessage && (
        <div className="status-line warn">⚠ {details.warningMessage}</div>
      )}

      {details && (
        <div className="asset-body">
          <div className="asset-preview">
            {details.previewDataUrl ? (
              <img src={details.previewDataUrl} alt={`Asset ${details.assetId}`} />
            ) : (
              <div className="asset-preview-empty">No preview image</div>
            )}
          </div>

          <div className="asset-meta">
            <table>
              <tbody>
                {metaRows(details).map((r) => (
                  <tr key={r.label}>
                    <td className="meta-label">{r.label}</td>
                    <td className="meta-value">{r.value}</td>
                  </tr>
                ))}
              </tbody>
            </table>

            {details.referencedAssetIds?.length > 0 && (
              <div className="referenced-ids">
                <div className="section-title">
                  Referenced Asset IDs ({details.referencedAssetIds.length})
                </div>
                <div className="id-chips">
                  {details.referencedAssetIds.map((id) => (
                    <button
                      key={id}
                      className="id-chip"
                      onClick={() => {
                        setInput(String(id));
                        setTimeout(load, 0);
                      }}
                    >
                      {id}
                    </button>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {details &&
        jsonSections(details).map((s) => (
          <details key={s.title} className="json-section">
            <summary>{s.title} JSON</summary>
            <pre>{s.json}</pre>
          </details>
        ))}
    </div>
  );
}
