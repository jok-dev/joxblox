package renderdoc

import (
	"image"
	"testing"
)

func TestComputeImageDHashRejectsTinyImages(t *testing.T) {
	tiny := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	if _, err := computeImageDHash(tiny); err == nil {
		t.Errorf("expected error for 4x4 image, got nil")
	}
}

func TestComputeImageDHashStableForSameInput(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			pix := y*128 + x*4
			img.Pix[pix+0] = uint8(x * 8)
			img.Pix[pix+1] = uint8(y * 8)
			img.Pix[pix+2] = 128
			img.Pix[pix+3] = 255
		}
	}
	a, errA := computeImageDHash(img)
	b, errB := computeImageDHash(img)
	if errA != nil || errB != nil {
		t.Fatalf("unexpected errors: %v, %v", errA, errB)
	}
	if a != b {
		t.Errorf("hash not stable: %x vs %x", a, b)
	}
}

func TestComputeImageDHashDistinguishesDifferentContent(t *testing.T) {
	mkLeftToRight := func() image.Image {
		img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				pix := y*128 + x*4
				img.Pix[pix+0] = uint8(x * 8)
				img.Pix[pix+1] = uint8(x * 8)
				img.Pix[pix+2] = uint8(x * 8)
				img.Pix[pix+3] = 255
			}
		}
		return img
	}
	mkTopToBottom := func() image.Image {
		img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				pix := y*128 + x*4
				img.Pix[pix+0] = uint8(y * 8)
				img.Pix[pix+1] = uint8(y * 8)
				img.Pix[pix+2] = uint8(y * 8)
				img.Pix[pix+3] = 255
			}
		}
		return img
	}
	a, _ := computeImageDHash(mkLeftToRight())
	b, _ := computeImageDHash(mkTopToBottom())
	if a == b {
		t.Errorf("expected distinct hashes for differently-oriented gradients, both = %x", a)
	}
}
