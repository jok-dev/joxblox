package loader

import (
	"strconv"
	"strings"
)

type MetadataGroup int

const (
	MetadataGroupIdentification MetadataGroup = iota
	MetadataGroupSizing
	MetadataGroupSource
	MetadataGroupReference
)

func (group MetadataGroup) Label() string {
	switch group {
	case MetadataGroupIdentification:
		return "Identification"
	case MetadataGroupSizing:
		return "Sizing"
	case MetadataGroupSource:
		return "Source"
	case MetadataGroupReference:
		return "Reference"
	}
	return ""
}

type MetadataKind int

const (
	MetadataKindText MetadataKind = iota
	MetadataKindSize
	MetadataKindNumber
	MetadataKindSHA
)

// MetadataSpec describes one row of asset metadata. The same registry drives
// both the UI renderer (via ViewExtract) and the scan DSL (via ScanExtract).
type MetadataSpec struct {
	Key         string
	Label       string
	Group       MetadataGroup
	Kind        MetadataKind
	ViewExtract func(AssetViewData) string
	ScanExtract func(ScanResult) (text string, numeric float64, present bool)
	Aliases     []string
	// FileScoped marks rows that only appear when the view has file context
	// (scan-tab preview). Single-asset tab hides these.
	FileScoped bool
}

// AssetMetadataSchema returns the canonical set of asset-view metadata rows,
// ordered by Group then intended display order.
func AssetMetadataSchema() []MetadataSpec {
	return []MetadataSpec{
		// ---- Identification ----
		{
			Key:     "assetid",
			Label:   "Asset ID",
			Group:   MetadataGroupIdentification,
			Kind:    MetadataKindNumber,
			Aliases: []string{"id"},
			ViewExtract: func(d AssetViewData) string {
				if d.AssetID <= 0 {
					return ""
				}
				return strconv.FormatInt(d.AssetID, 10)
			},
			ScanExtract: func(r ScanResult) (string, float64, bool) {
				if r.AssetID <= 0 {
					return "", 0, false
				}
				return strconv.FormatInt(r.AssetID, 10), float64(r.AssetID), true
			},
		},
		{
			Key:     "assettype",
			Label:   "Asset Type",
			Group:   MetadataGroupIdentification,
			Kind:    MetadataKindText,
			Aliases: []string{},
			ViewExtract: func(d AssetViewData) string {
				return d.AssetTypeDisplay
			},
			ScanExtract: func(r ScanResult) (string, float64, bool) {
				if strings.TrimSpace(r.AssetTypeName) == "" {
					return "", 0, false
				}
				return r.AssetTypeName, 0, true
			},
		},
		{
			Key:   "dimensions",
			Label: "Dimensions",
			Group: MetadataGroupIdentification,
			Kind:  MetadataKindText,
			ViewExtract: func(d AssetViewData) string {
				return d.DimensionsDisplay
			},
		},
		{
			Key:   "format",
			Label: "Format",
			Group: MetadataGroupIdentification,
			Kind:  MetadataKindText,
			ViewExtract: func(d AssetViewData) string {
				return d.FormatDisplay
			},
		},
		{
			Key:     "contenttype",
			Label:   "Content-Type",
			Group:   MetadataGroupIdentification,
			Kind:    MetadataKindText,
			Aliases: []string{"content"},
			ViewExtract: func(d AssetViewData) string {
				return d.ContentTypeDisplay
			},
		},

		// ---- Sizing ----
		{
			Key:     "selfsize",
			Label:   "Self Size",
			Group:   MetadataGroupSizing,
			Kind:    MetadataKindSize,
			Aliases: []string{},
			ViewExtract: func(d AssetViewData) string {
				return d.SelfSizeDisplay
			},
			ScanExtract: func(r ScanResult) (string, float64, bool) {
				if r.BytesSize <= 0 {
					return "", 0, false
				}
				return "", float64(r.BytesSize), true
			},
		},
		{
			Key:   "totalsize",
			Label: "Total Size",
			Group: MetadataGroupSizing,
			Kind:  MetadataKindSize,
			ViewExtract: func(d AssetViewData) string {
				return d.TotalSizeDisplay
			},
			ScanExtract: func(r ScanResult) (string, float64, bool) {
				if r.TotalBytesSize <= 0 {
					return "", 0, false
				}
				return "", float64(r.TotalBytesSize), true
			},
		},
		{
			Key:   "ingamesize",
			Label: "In-Game Size",
			Group: MetadataGroupSizing,
			Kind:  MetadataKindText,
			ViewExtract: func(d AssetViewData) string {
				return d.InGameSizeDisplay
			},
		},

		// ---- Source ----
		{
			Key:     "imagesource",
			Label:   "Image Source",
			Group:   MetadataGroupSource,
			Kind:    MetadataKindText,
			Aliases: []string{"imgsource"},
			ViewExtract: func(d AssetViewData) string {
				return d.SourceDisplay
			},
		},
		{
			Key:   "usecount",
			Label: "Use Count",
			Group: MetadataGroupSource,
			Kind:  MetadataKindNumber,
			ViewExtract: func(d AssetViewData) string {
				return d.UseCountDisplay
			},
			ScanExtract: func(r ScanResult) (string, float64, bool) {
				if r.UseCount <= 0 {
					return "", 0, false
				}
				return strconv.Itoa(r.UseCount), float64(r.UseCount), true
			},
		},
		{
			Key:   "failurereason",
			Label: "Failure Reason",
			Group: MetadataGroupSource,
			Kind:  MetadataKindText,
			ViewExtract: func(d AssetViewData) string {
				return d.FailureReasonText
			},
			ScanExtract: func(r ScanResult) (string, float64, bool) {
				if strings.TrimSpace(r.WarningCause) == "" {
					return "", 0, false
				}
				return r.WarningCause, 0, true
			},
		},
		{
			Key:        "file",
			Label:      "File",
			Group:      MetadataGroupSource,
			Kind:       MetadataKindText,
			FileScoped: true,
			Aliases:    []string{"filepath"},
			ViewExtract: func(d AssetViewData) string {
				return d.FileDisplay
			},
			ScanExtract: func(r ScanResult) (string, float64, bool) {
				if strings.TrimSpace(r.FilePath) == "" {
					return "", 0, false
				}
				return r.FilePath, 0, true
			},
		},
		{
			Key:        "downloadsha",
			Label:      "Downloaded SHA256",
			Group:      MetadataGroupSource,
			Kind:       MetadataKindSHA,
			FileScoped: true,
			Aliases:    []string{"filesha256"},
			ViewExtract: func(d AssetViewData) string {
				return d.FileSHA256Display
			},
			ScanExtract: func(r ScanResult) (string, float64, bool) {
				if strings.TrimSpace(r.FileSHA256) == "" {
					return "", 0, false
				}
				return r.FileSHA256, 0, true
			},
		},

		// ---- Reference ----
		{
			Key:   "referencedassets",
			Label: "Referenced Assets",
			Group: MetadataGroupReference,
			Kind:  MetadataKindNumber,
			ViewExtract: func(d AssetViewData) string {
				return d.ReferencedDisplay
			},
		},
		{
			Key:     "refinstancetype",
			Label:   "Reference Instance Type",
			Group:   MetadataGroupReference,
			Kind:    MetadataKindText,
			Aliases: []string{"refitype"},
			ViewExtract: func(d AssetViewData) string {
				return d.ReferenceInstanceType
			},
			ScanExtract: func(r ScanResult) (string, float64, bool) {
				if strings.TrimSpace(r.InstanceType) == "" {
					return "", 0, false
				}
				return r.InstanceType, 0, true
			},
		},
		{
			Key:     "refproperty",
			Label:   "Reference Property Name",
			Group:   MetadataGroupReference,
			Kind:    MetadataKindText,
			Aliases: []string{"refprop"},
			ViewExtract: func(d AssetViewData) string {
				return d.ReferencePropertyName
			},
			ScanExtract: func(r ScanResult) (string, float64, bool) {
				if strings.TrimSpace(r.PropertyName) == "" {
					return "", 0, false
				}
				return r.PropertyName, 0, true
			},
		},
		{
			Key:     "refinstancepath",
			Label:   "Reference Instance Path",
			Group:   MetadataGroupReference,
			Kind:    MetadataKindText,
			Aliases: []string{"refipath"},
			ViewExtract: func(d AssetViewData) string {
				return d.ReferenceInstancePath
			},
			ScanExtract: func(r ScanResult) (string, float64, bool) {
				if strings.TrimSpace(r.InstancePath) == "" {
					return "", 0, false
				}
				return r.InstancePath, 0, true
			},
		},
	}
}

// MetadataGroupsInOrder returns the groups in display order.
func MetadataGroupsInOrder() []MetadataGroup {
	return []MetadataGroup{
		MetadataGroupIdentification,
		MetadataGroupSizing,
		MetadataGroupSource,
		MetadataGroupReference,
	}
}
