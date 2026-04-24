package report

import "testing"

func TestEstimateGPUTextureBytesExact_BC1_512Square(t *testing.T) {
	// 512x512 BC1 full mip chain:
	//   512² = 131072, 256² = 32768, 128² = 8192, 64² = 2048, 32² = 512,
	//   16² = 128, 8² = 32, 4² = 8, 2² padded to 4x4 = 8, 1x1 padded = 8
	// Sum = 174776 bytes.
	const want int64 = 174776
	got := EstimateGPUTextureBytesExact(512, 512, false)
	if got != want {
		t.Errorf("EstimateGPUTextureBytesExact(512, 512, BC1) = %d, want %d", got, want)
	}
}

func TestEstimateGPUTextureBytesExact_BC3_512Square(t *testing.T) {
	// 512x512 BC3 full mip chain (16 B/block, two blocks at minimum 4x4
	// store an extra 16 B each for 2² and 1x1 mips):
	//   262144 + 65536 + 16384 + 4096 + 1024 + 256 + 64 + 16 + 16 + 16 = 349552
	const want int64 = 349552
	got := EstimateGPUTextureBytesExact(512, 512, true)
	if got != want {
		t.Errorf("EstimateGPUTextureBytesExact(512, 512, BC3) = %d, want %d", got, want)
	}
}

func TestEstimateGPUTextureBytesExact_ApproximationErrorUnderTenthOfPercent(t *testing.T) {
	// Sanity check: the exact answer should be within 0.1% of the 4/3
	// approximation for a standard 1024x1024 BC1 texture. Approx < exact
	// (approximation undercounts because it ignores block padding).
	approx := EstimateGPUTextureBytes(1024*1024, 0)
	exact := EstimateGPUTextureBytesExact(1024, 1024, false)
	if exact <= approx {
		t.Errorf("exact (%d) should exceed approx (%d) due to block padding", exact, approx)
	}
	ratio := float64(exact-approx) / float64(exact)
	if ratio > 0.001 {
		t.Errorf("exact-vs-approx ratio = %.6f, expected < 0.001", ratio)
	}
}

func TestEstimateGPUTextureBytesExact_NonSquareRectangle(t *testing.T) {
	// 64x16 BC1:
	//   mip 0: 64*16 = 1024 → 16x4 blocks = 64 blocks = 512 B
	//   mip 1: 32*8   → 8x2 blocks = 16 blocks = 128 B
	//   mip 2: 16*4   → 4x1 blocks = 4 blocks = 32 B
	//   mip 3: 8*2    → 2x1 (padded)    = 2 blocks = 16 B
	//   mip 4: 4*1    → 1x1 (padded)    = 1 block  = 8 B
	//   mip 5: 2*1    → 1x1 (padded)    = 1 block  = 8 B
	//   mip 6: 1*1    → 1x1 (padded)    = 1 block  = 8 B
	// Sum = 712 bytes.
	const want int64 = 712
	got := EstimateGPUTextureBytesExact(64, 16, false)
	if got != want {
		t.Errorf("EstimateGPUTextureBytesExact(64, 16, BC1) = %d, want %d", got, want)
	}
}

func TestEstimateGPUTextureBytesExact_ZeroDimensionsReturnZero(t *testing.T) {
	if got := EstimateGPUTextureBytesExact(0, 512, false); got != 0 {
		t.Errorf("width=0 got %d, want 0", got)
	}
	if got := EstimateGPUTextureBytesExact(512, 0, true); got != 0 {
		t.Errorf("height=0 got %d, want 0", got)
	}
	if got := EstimateGPUTextureBytesExact(-1, 512, false); got != 0 {
		t.Errorf("negative width got %d, want 0", got)
	}
}

func TestEstimateGPUTextureBytesExact_1x1MinimumBlock(t *testing.T) {
	// A 1x1 texture still allocates a full 4x4 block: 8 B for BC1, 16 B for BC3.
	if got := EstimateGPUTextureBytesExact(1, 1, false); got != 8 {
		t.Errorf("1x1 BC1 got %d, want 8", got)
	}
	if got := EstimateGPUTextureBytesExact(1, 1, true); got != 16 {
		t.Errorf("1x1 BC3 got %d, want 16", got)
	}
}
