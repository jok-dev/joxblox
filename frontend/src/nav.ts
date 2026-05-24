export type NavItem = { key: string; title: string };
export type NavGroup = { title: string; items: NavItem[] };

// Mirrors the grouped sidebar from the redesign. Keys are stable identifiers
// used to select the active view.
export const NAV_GROUPS: NavGroup[] = [
  {
    title: "Core Analytics",
    items: [
      { key: "report", title: "Report Generation" },
      { key: "scan", title: "Asset Breakdown" },
      { key: "heatmap", title: "Heatmap" },
      { key: "heatmap3d", title: "3D Heatmap" },
    ],
  },
  {
    title: "Asset Management",
    items: [
      { key: "single-asset", title: "Single Asset" },
      { key: "lod", title: "LOD Viewer" },
      { key: "optimize", title: "Optimize Assets" },
    ],
  },
  {
    title: "Utilities",
    items: [
      { key: "image-gen", title: "Image Generator" },
      { key: "renderdoc", title: "RenderDoc" },
      { key: "diff", title: "Diff" },
    ],
  },
];

export const NAV_TITLES: Record<string, string> = Object.fromEntries(
  NAV_GROUPS.flatMap((g) => g.items.map((i) => [i.key, i.title]))
);
