package ui

import xdraw "golang.org/x/image/draw"

const (
	SampleModeNearestNeighbor = "Nearest Neighbor"
	SampleModeBilinear        = "Bilinear"
	SampleModeCatmullRom      = "Catmull-Rom"
	DefaultSampleMode         = SampleModeCatmullRom
)

var SampleModeOptions = []string{
	SampleModeNearestNeighbor,
	SampleModeBilinear,
	SampleModeCatmullRom,
}

func SampleModeInterpolator(mode string) xdraw.Interpolator {
	switch mode {
	case SampleModeNearestNeighbor:
		return xdraw.NearestNeighbor
	case SampleModeBilinear:
		return xdraw.BiLinear
	default:
		return xdraw.CatmullRom
	}
}
