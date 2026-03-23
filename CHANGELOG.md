# Changelog

## v1.0.0 - 2026-03-23

Initial public release of `Joxblox`.

### Added

- `Single Asset` tab for loading Roblox asset IDs with preview, metadata, hierarchy browsing, and raw API/ extractor JSON views
- `Scan` tab with `RBXL` and `Folders` sources, each supporting `Single` and `Diff` modes
- Sortable scan table with search, duplicate filtering, asset type filtering, instance type filtering, and property name filtering
- Scan stats for rows, shown rows, failed rows, duplicate count, duplicate size, and shown size
- Reference metadata display for instance type, property name, and instance path
- JSON import/export for scan tables and full scan workspaces
- Drag-and-drop JSON import support
- Recent file history for imported results
- Preview variant selection with displayed size and percentage change from the original asset
- Expanded preview window with zoom and pan support
- `Image Generator` tab for creating PNG files for manual Roblox upload workflows
- Optional `.ROBLOSECURITY` authentication with OS keychain / credential-store persistence
- Global `File` menu actions for saving, loading, and clearing scan results
- Global `Help` menu support for viewing the changelog, about info, and license details

### Changed

- Improved UI responsiveness by moving preview variant generation off the UI thread
- Reduced scan redraw churn with debounced searching and throttled refresh behavior
- Improved image rendering responsiveness using faster Fyne scaling settings
- Added determinate JSON save/load progress dialogs

### Notes

- Release builds package the changelog, license text, and RBXL extractor support directly into the shipped app binary
