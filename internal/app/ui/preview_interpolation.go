package ui

import xdraw "golang.org/x/image/draw"

const (
	sampleModeNearestNeighbor = "Nearest Neighbor"
	sampleModeBilinear        = "Bilinear"
	sampleModeCatmullRom      = "Catmull-Rom"
	defaultSampleMode         = sampleModeCatmullRom
)

var sampleModeOptions = []string{
	sampleModeNearestNeighbor,
	sampleModeBilinear,
	sampleModeCatmullRom,
}

func sampleModeInterpolator(mode string) xdraw.Interpolator {
	switch mode {
	case sampleModeNearestNeighbor:
		return xdraw.NearestNeighbor
	case sampleModeBilinear:
		return xdraw.BiLinear
	default:
		return xdraw.CatmullRom
	}
}
