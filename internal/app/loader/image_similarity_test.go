package loader

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %s", err)
	}
	return buf.Bytes()
}

// makeNormalMapXVarying returns a 32×32 image with a horizontal RGB
// step pattern: left half has high R + medium G + uniform B (a normal
// pointing right, encoded), right half has low R. G stays at 128 so
// the only XY-encoding change is in R (X deflection). Step boundaries
// give dHash actual bits to compare; uniform ramps would just produce
// all-zero hashes.
func makeNormalMapXVarying(t *testing.T) image.Image {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			r := uint8(196)
			if x >= 16 {
				r = 0
			}
			img.SetRGBA(x, y, color.RGBA{R: r, G: 128, B: 255, A: 255})
		}
	}
	return img
}

// makeNormalMapYVaryingButLuminanceMatchesA constructs a step pattern
// where R is constant and G carries the step, and the magnitudes are
// tuned so that the LUMINANCE step (0.299·ΔR + 0.587·ΔG) ends up the
// same as makeNormalMapXVarying's. That makes the standard luminance
// dHash see two near-identical inputs even though the underlying
// surface (R vs G channel) is completely different. The dual R+G
// normal-map hash should still tell them apart because R and G are
// hashed independently.
//
// Tuning: A varies R by 196 → ΔY ≈ 0.299·196 ≈ 58.6.
// To match, B should vary G by ≈100 → ΔY ≈ 0.587·100 ≈ 58.7.
func makeNormalMapYVaryingButLuminanceMatchesA(t *testing.T) image.Image {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			g := uint8(178)
			if x >= 16 {
				g = 78
			}
			img.SetRGBA(x, y, color.RGBA{R: 128, G: g, B: 255, A: 255})
		}
	}
	return img
}

// TestComputeImageDHashesForNormalMap_DiscriminatesXvsYDeflection asserts
// that two normal maps differing only in WHICH channel ramps (X-only vs
// Y-only deflection) are distinguishable in the dual R+G hash. The
// luminance dHash collapses them — both produce the same horizontal
// brightness ramp — but the dual hash sees that R differs in one case
// and G differs in the other, summing those into a non-zero distance.
func TestComputeImageDHashesForNormalMap_DiscriminatesXvsYDeflection(t *testing.T) {
	a := encodePNG(t, makeNormalMapXVarying(t))
	b := encodePNG(t, makeNormalMapYVaryingButLuminanceMatchesA(t))

	hashALum, err := ComputeImageDHash(a)
	if err != nil {
		t.Fatalf("ComputeImageDHash(a): %s", err)
	}
	hashBLum, err := ComputeImageDHash(b)
	if err != nil {
		t.Fatalf("ComputeImageDHash(b): %s", err)
	}
	luminanceDist := dHashHammingDistance(hashALum, hashBLum)

	hashesA, err := ComputeImageDHashesForNormalMap(a)
	if err != nil {
		t.Fatalf("ComputeImageDHashesForNormalMap(a): %s", err)
	}
	hashesB, err := ComputeImageDHashesForNormalMap(b)
	if err != nil {
		t.Fatalf("ComputeImageDHashesForNormalMap(b): %s", err)
	}
	normalDist := NormalMapHammingDistance(hashesA, hashesB)

	// Both ramps produce identical luminance gradients (R*0.299 vs
	// G*0.587 both go up left-to-right) so luminance can't tell them
	// apart at all.
	if luminanceDist != 0 {
		t.Logf("luminance distance non-zero (%d) — synthetic test still expected normal-mode > luminance though", luminanceDist)
	}
	if normalDist <= luminanceDist {
		t.Errorf("expected normal-map dual hash to discriminate X-only vs Y-only deflection: lumDist=%d normalDist=%d", luminanceDist, normalDist)
	}
}

// TestComputeImageDHashesForNormalMap_IdenticalImagesProduceIdenticalHashes
// guards against drift in the helper — same input must always yield the
// same pair of hashes.
func TestComputeImageDHashesForNormalMap_IdenticalImagesProduceIdenticalHashes(t *testing.T) {
	src := encodePNG(t, makeNormalMapXVarying(t))
	a, err := ComputeImageDHashesForNormalMap(src)
	if err != nil {
		t.Fatalf("first hash: %s", err)
	}
	b, err := ComputeImageDHashesForNormalMap(src)
	if err != nil {
		t.Fatalf("second hash: %s", err)
	}
	if a != b {
		t.Errorf("identical input gave different hashes: %+v vs %+v", a, b)
	}
	if NormalMapHammingDistance(a, b) != 0 {
		t.Errorf("identical input should have zero normal-map distance, got %d", NormalMapHammingDistance(a, b))
	}
}
