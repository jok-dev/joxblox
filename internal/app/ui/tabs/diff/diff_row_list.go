package diff

import (
	"image/color"
	"sort"
	"strings"

	"joxblox/internal/extractor"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type DiffRowStatus int

const (
	DiffRowAdded DiffRowStatus = iota
	DiffRowRemoved
	DiffRowChanged
)

type DiffRow struct {
	Status     DiffRowStatus
	Path       string
	Class      string
	Name       string
	AddedRef   *extractor.DiffInstance
	RemovedRef *extractor.DiffInstance
	ChangedRef *extractor.DiffChangedInstance
}

var (
	colorAdded   = color.NRGBA{R: 0x37, G: 0xA8, B: 0x55, A: 0xFF}
	colorRemoved = color.NRGBA{R: 0xCC, G: 0x3D, B: 0x3D, A: 0xFF}
	colorChanged = color.NRGBA{R: 0xCE, G: 0xA0, B: 0x2A, A: 0xFF}
)

func BuildDiffRows(result extractor.DiffResult) []DiffRow {
	total := len(result.Added) + len(result.Removed) + len(result.Changed)
	rows := make([]DiffRow, 0, total)
	for index := range result.Added {
		instance := &result.Added[index]
		rows = append(rows, DiffRow{
			Status:   DiffRowAdded,
			Path:     instance.Path,
			Class:    instance.Class,
			Name:     instance.Name,
			AddedRef: instance,
		})
	}
	for index := range result.Removed {
		instance := &result.Removed[index]
		rows = append(rows, DiffRow{
			Status:     DiffRowRemoved,
			Path:       instance.Path,
			Class:      instance.Class,
			Name:       instance.Name,
			RemovedRef: instance,
		})
	}
	for index := range result.Changed {
		instance := &result.Changed[index]
		rows = append(rows, DiffRow{
			Status:     DiffRowChanged,
			Path:       instance.Path,
			Class:      instance.Class,
			Name:       instance.Name,
			ChangedRef: instance,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Path != rows[j].Path {
			return rows[i].Path < rows[j].Path
		}
		return rows[i].Status < rows[j].Status
	})
	return rows
}

func badgeForStatus(status DiffRowStatus) (string, color.Color) {
	switch status {
	case DiffRowAdded:
		return "+", colorAdded
	case DiffRowRemoved:
		return "-", colorRemoved
	default:
		return "~", colorChanged
	}
}

type diffCounts struct {
	Added, Removed, Changed int
}

func (c diffCounts) total() int { return c.Added + c.Removed + c.Changed }

func (c diffCounts) format() string {
	if c.total() == 0 {
		return ""
	}
	var parts []string
	if c.Added > 0 {
		parts = append(parts, "+"+itoa(c.Added))
	}
	if c.Removed > 0 {
		parts = append(parts, "-"+itoa(c.Removed))
	}
	if c.Changed > 0 {
		parts = append(parts, "~"+itoa(c.Changed))
	}
	return strings.Join(parts, " ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}

type diffTreeNode struct {
	path     string
	segment  string // last path segment, used as the display name
	row      *DiffRow
	children []string // sorted child UIDs
	counts   diffCounts
	// matchSelf and matchSubtree are derived from the current filter; cached
	// here so ChildUIDs/IsBranch don't redo work per visible row.
	matchSelf    bool
	matchSubtree bool
}

func splitPath(path string) []string {
	if path == "" {
		return nil
	}
	return strings.Split(path, ".")
}

func lastSegment(path string) string {
	if idx := strings.LastIndex(path, "."); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

func parentPath(path string) string {
	if idx := strings.LastIndex(path, "."); idx >= 0 {
		return path[:idx]
	}
	return ""
}

// buildTreeIndex walks the flat row list and produces an index with one
// node per path (rows + every ancestor segment), parent->children edges,
// and rolled-up counts so intermediate nodes can display aggregate
// "+12 -3 ~5" without re-walking the subtree on every render.
func buildTreeIndex(rows []DiffRow) map[string]*diffTreeNode {
	nodes := map[string]*diffTreeNode{
		"": {path: "", segment: ""},
	}
	for i := range rows {
		row := rows[i]
		// Ensure every ancestor node exists.
		segments := splitPath(row.Path)
		cumulative := ""
		for _, seg := range segments {
			if cumulative == "" {
				cumulative = seg
			} else {
				cumulative = cumulative + "." + seg
			}
			if _, ok := nodes[cumulative]; !ok {
				nodes[cumulative] = &diffTreeNode{
					path:    cumulative,
					segment: lastSegment(cumulative),
				}
			}
		}
		// Attach the row to its leaf node (a row's path always lands on
		// an existing node by virtue of the loop above).
		rowCopy := row
		nodes[row.Path].row = &rowCopy
	}

	// Wire children: append each non-root node to its parent's children
	// list, then sort each list once.
	for path, node := range nodes {
		if path == "" {
			continue
		}
		parent := nodes[parentPath(path)]
		parent.children = append(parent.children, path)
		_ = node
	}
	for _, node := range nodes {
		sort.Strings(node.children)
	}

	// Roll up counts bottom-up. Sort all node paths by descending length
	// so a child is always processed before its parent.
	allPaths := make([]string, 0, len(nodes))
	for path := range nodes {
		if path == "" {
			continue
		}
		allPaths = append(allPaths, path)
	}
	sort.Slice(allPaths, func(i, j int) bool {
		return len(allPaths[i]) > len(allPaths[j])
	})
	for _, path := range allPaths {
		node := nodes[path]
		if node.row != nil {
			switch node.row.Status {
			case DiffRowAdded:
				node.counts.Added++
			case DiffRowRemoved:
				node.counts.Removed++
			case DiffRowChanged:
				node.counts.Changed++
			}
		}
		if parent := nodes[parentPath(path)]; parent != nil {
			parent.counts.Added += node.counts.Added
			parent.counts.Removed += node.counts.Removed
			parent.counts.Changed += node.counts.Changed
		}
	}
	return nodes
}

// rowWidgets carries the typed pointers for a single recycled template
// row. Stored in DiffRowList.rowWidgets keyed by the template widget
// pointer so updateNode can mutate text without depending on struct
// embedding (Fyne's tree renderer pulls our content out via the
// fyne.CanvasObject interface; passing back a custom embedded type and
// type-asserting it in updateNode is fragile across Fyne versions).
type rowWidgets struct {
	badge  *canvas.Text
	name   *widget.Label
	class  *widget.Label
	counts *widget.Label
}

// secondaryTapRow is the custom row widget. Fyne's event dispatcher
// picks the deepest object satisfying Tappable OR SecondaryTappable
// and routes events to that single object — so implementing only
// SecondaryTappable swallows primary clicks. We implement both:
// primary clicks forward to the row-list's onPrimary callback (which
// calls tree.Select) so selection still works, and right-clicks fire
// onSecondary to pop the copy menu.
type secondaryTapRow struct {
	widget.BaseWidget
	content     *fyne.Container
	currentUID  widget.TreeNodeID
	onPrimary   func(uid widget.TreeNodeID)
	onSecondary func(uid widget.TreeNodeID, pos fyne.Position)
}

func newSecondaryTapRow(
	content *fyne.Container,
	onPrimary func(widget.TreeNodeID),
	onSecondary func(widget.TreeNodeID, fyne.Position),
) *secondaryTapRow {
	row := &secondaryTapRow{content: content, onPrimary: onPrimary, onSecondary: onSecondary}
	row.ExtendBaseWidget(row)
	return row
}

func (r *secondaryTapRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(r.content)
}

func (r *secondaryTapRow) Tapped(event *fyne.PointEvent) {
	if r.onPrimary != nil && r.currentUID != "" {
		r.onPrimary(r.currentUID)
	}
}

func (r *secondaryTapRow) TappedSecondary(event *fyne.PointEvent) {
	if r.onSecondary != nil && r.currentUID != "" {
		r.onSecondary(r.currentUID, event.AbsolutePosition)
	}
}

// DiffRowTarget describes what the user right-clicked. For nodes that
// appear as a diff entry (Added / Removed / Changed), HasRow is true
// and Row carries the full record. For intermediate folder nodes that
// are only in the tree because some descendant differs, HasRow is
// false — the path still exists in both files, so callers can still
// offer "Copy from A" / "Copy from B".
type DiffRowTarget struct {
	Path   string
	HasRow bool
	Row    DiffRow
}

type DiffRowList struct {
	tree          *widget.Tree
	nodes         map[string]*diffTreeNode
	bottomUpOrder []string // node paths sorted child-before-parent; cached for applyFilter
	filter        DiffFilterState
	onSelect      func(DiffRow)
	onSecondary   func(target DiffRowTarget, pos fyne.Position)
	rowWidgets    map[fyne.CanvasObject]*rowWidgets
}

func NewDiffRowList(onSelect func(DiffRow)) *DiffRowList {
	list := &DiffRowList{
		nodes: map[string]*diffTreeNode{
			"": {path: "", segment: ""},
		},
		filter: DiffFilterState{
			ShowAdded: true, ShowRemoved: true, ShowChanged: true,
		},
		onSelect:   onSelect,
		rowWidgets: map[fyne.CanvasObject]*rowWidgets{},
	}
	list.tree = widget.NewTree(
		list.childUIDs,
		list.isBranch,
		list.createNode,
		list.updateNode,
	)
	list.tree.OnSelected = list.onTreeSelected
	return list
}

func (l *DiffRowList) CanvasObject() fyne.CanvasObject {
	return l.tree
}

func (l *DiffRowList) SetRows(rows []DiffRow) {
	l.nodes = buildTreeIndex(rows)
	l.bottomUpOrder = l.bottomUpOrder[:0]
	for path := range l.nodes {
		if path == "" {
			continue
		}
		l.bottomUpOrder = append(l.bottomUpOrder, path)
	}
	sort.Slice(l.bottomUpOrder, func(i, j int) bool {
		return len(l.bottomUpOrder[i]) > len(l.bottomUpOrder[j])
	})
	l.applyFilter()
	l.tree.Refresh()
	l.tree.UnselectAll()
}

func (l *DiffRowList) SetFilter(filter DiffFilterState) {
	l.filter = filter
	l.applyFilter()
	l.tree.Refresh()
	if l.filter.HasSearch() {
		l.expandMatches()
	}
}

// applyFilter recomputes matchSelf / matchSubtree for every node based on
// the current filter. Done once per filter change so the per-row tree
// callbacks stay O(1).
func (l *DiffRowList) applyFilter() {
	// First pass: matchSelf for nodes that carry a row.
	for _, node := range l.nodes {
		node.matchSelf = false
		node.matchSubtree = false
		if node.row == nil {
			continue
		}
		if l.filter.Matches(node.row.Status, node.row.Path, node.row.Class, node.row.Name) {
			node.matchSelf = true
		}
	}
	// Second pass: matchSubtree is true if this node or any descendant
	// has matchSelf. Uses the cached child-before-parent ordering so
	// every keystroke is O(N) with no per-call allocation.
	for _, path := range l.bottomUpOrder {
		node := l.nodes[path]
		if node.matchSelf {
			node.matchSubtree = true
			continue
		}
		for _, childPath := range node.children {
			if l.nodes[childPath].matchSubtree {
				node.matchSubtree = true
				break
			}
		}
	}
}

func (l *DiffRowList) childUIDs(uid widget.TreeNodeID) []widget.TreeNodeID {
	node, ok := l.nodes[string(uid)]
	if !ok {
		return nil
	}
	// Avoid allocating if every child passes.
	allMatch := true
	for _, childPath := range node.children {
		if !l.nodes[childPath].matchSubtree {
			allMatch = false
			break
		}
	}
	if allMatch {
		out := make([]widget.TreeNodeID, len(node.children))
		for i, c := range node.children {
			out[i] = widget.TreeNodeID(c)
		}
		return out
	}
	out := make([]widget.TreeNodeID, 0, len(node.children))
	for _, childPath := range node.children {
		if l.nodes[childPath].matchSubtree {
			out = append(out, widget.TreeNodeID(childPath))
		}
	}
	return out
}

func (l *DiffRowList) isBranch(uid widget.TreeNodeID) bool {
	node, ok := l.nodes[string(uid)]
	if !ok {
		return false
	}
	// A node is a branch in the tree's eyes whenever it has at least one
	// currently-visible child. Intermediate nodes with no row of their
	// own are always branches when their subtree matches.
	for _, childPath := range node.children {
		if l.nodes[childPath].matchSubtree {
			return true
		}
	}
	return false
}

func (l *DiffRowList) createNode(branch bool) fyne.CanvasObject {
	_ = branch
	// Labels in an HBox with Truncation set report a minimum size equal
	// to the ellipsis width, so the HBox packs them to "..." even when
	// the row is wide. Use a Border where `name` is the content child —
	// Border gives the content all the slack space — and the right
	// cluster (class + counts) lives in the right slot.
	badge := canvas.NewText("?", color.Transparent)
	badge.TextStyle = fyne.TextStyle{Bold: true}
	name := widget.NewLabel("?")
	name.Truncation = fyne.TextTruncateEllipsis
	class := widget.NewLabel("")
	class.Importance = widget.LowImportance
	counts := widget.NewLabel("")
	counts.Importance = widget.LowImportance
	rightCluster := container.NewHBox(class, counts)
	body := container.NewBorder(nil, nil, badge, rightCluster, name)
	row := newSecondaryTapRow(body, l.handlePrimaryTap, l.handleSecondaryTap)
	l.rowWidgets[row] = &rowWidgets{badge: badge, name: name, class: class, counts: counts}
	return row
}

// handlePrimaryTap is the bridge from the per-row widget back to the
// tree's selection state. Without this, our custom row widget would
// swallow primary clicks (see secondaryTapRow comment).
func (l *DiffRowList) handlePrimaryTap(uid widget.TreeNodeID) {
	l.tree.Select(uid)
}

// SetOnSecondary registers the right-click handler. The handler
// receives a DiffRowTarget describing the clicked node (which may be
// an intermediate folder, not a diff entry) and the absolute click
// position so the caller can pop a context menu at the cursor.
func (l *DiffRowList) SetOnSecondary(handler func(target DiffRowTarget, pos fyne.Position)) {
	l.onSecondary = handler
}

// handleSecondaryTap is the bridge from the per-row widget back to the
// list-level callback. Both diff entries and intermediate folder nodes
// are forwarded — intermediates have HasRow=false so the caller can
// still offer both-sides copy.
func (l *DiffRowList) handleSecondaryTap(uid widget.TreeNodeID, pos fyne.Position) {
	if l.onSecondary == nil {
		return
	}
	node, ok := l.nodes[string(uid)]
	if !ok || node.path == "" {
		return
	}
	target := DiffRowTarget{Path: node.path}
	if node.row != nil {
		target.HasRow = true
		target.Row = *node.row
	}
	l.onSecondary(target, pos)
}

func (l *DiffRowList) updateNode(uid widget.TreeNodeID, branch bool, node fyne.CanvasObject) {
	_ = branch
	if row, ok := node.(*secondaryTapRow); ok {
		row.currentUID = uid
	}
	fields, ok := l.rowWidgets[node]
	if !ok {
		return
	}
	n, exists := l.nodes[string(uid)]
	if !exists {
		fields.badge.Text = " "
		fields.badge.Color = color.Transparent
		fields.name.SetText("")
		fields.class.SetText("")
		fields.counts.SetText("")
		return
	}

	if n.row != nil {
		badgeText, badgeColor := badgeForStatus(n.row.Status)
		fields.badge.Text = badgeText
		fields.badge.Color = badgeColor
		fields.name.SetText(displayName(n))
		fields.class.SetText(classLabel(n.row.Class))
	} else {
		fields.badge.Text = "  "
		fields.badge.Color = color.Transparent
		fields.name.SetText(n.segment)
		fields.class.SetText("")
	}
	fields.counts.SetText(n.counts.format())
	fields.badge.Refresh()
}

func displayName(n *diffTreeNode) string {
	if n.row != nil && n.row.Name != "" && n.row.Name != n.segment {
		return n.row.Name
	}
	return n.segment
}

func classLabel(class string) string {
	if class == "" {
		return ""
	}
	return "(" + class + ")"
}

func (l *DiffRowList) onTreeSelected(uid widget.TreeNodeID) {
	n, ok := l.nodes[string(uid)]
	if !ok {
		return
	}
	if n.row == nil {
		// Intermediate node — leave selection visual but don't fire
		// onSelect (no row to display).
		return
	}
	if l.onSelect != nil {
		l.onSelect(*n.row)
	}
}

// expandMatches opens every branch that has a matching descendant. Only
// called when a search term is active so the user doesn't have to
// drill down manually to find hits.
func (l *DiffRowList) expandMatches() {
	for path, node := range l.nodes {
		if path == "" {
			continue
		}
		if node.matchSubtree && len(node.children) > 0 {
			l.tree.OpenBranch(widget.TreeNodeID(path))
		}
	}
}
