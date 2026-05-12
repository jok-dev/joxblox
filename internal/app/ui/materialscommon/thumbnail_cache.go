package materialscommon

import (
	"image"
	"sync"

	"fyne.io/fyne/v2"
	xdraw "golang.org/x/image/draw"
)

// ThumbnailMaxDim is the largest dimension we keep cached preview images
// at. Decoding a 4K BC texture yields a 4096×4096 NRGBA buffer (~64 MB);
// caching that as-is means every table cell render and every preview
// update would rescale 4K→32px on the UI thread, which freezes Fyne for
// noticeable beats. Downsampling once at decode time turns each cache
// hit into a near-zero-cost paint.
const ThumbnailMaxDim = 256

// DecodeFunc returns the decoded image for a key. Runs on a background
// goroutine, so it must not touch fyne UI state.
type DecodeFunc func() (image.Image, error)

// ThumbnailCache is a keyed image cache with per-key decode coalescing.
// Each key is decoded at most once at a time; concurrent RequestDecode
// calls for the same key share the in-flight goroutine — only the first
// caller's onCached fires, but every cell waiting on the cache will pick
// up the cached image when the table is refreshed (which the caller
// typically does inside onCached).
type ThumbnailCache struct {
	mu       sync.Mutex
	cache    map[string]image.Image
	inFlight map[string]bool
}

func NewThumbnailCache() *ThumbnailCache {
	return &ThumbnailCache{
		cache:    map[string]image.Image{},
		inFlight: map[string]bool{},
	}
}

// Get returns the cached image for key, or (nil, false).
func (c *ThumbnailCache) Get(key string) (image.Image, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	img, ok := c.cache[key]
	return img, ok
}

// RequestDecode kicks off a background decode for key if one isn't
// already in flight. onCached fires on the UI thread once the cache has
// been populated. If the key is already cached, onCached fires
// immediately. If decode is already in flight, this is a no-op.
//
// On decode error the cache is unchanged and onCached does NOT fire.
func (c *ThumbnailCache) RequestDecode(key string, decode DecodeFunc, onCached func()) {
	c.mu.Lock()
	if _, ok := c.cache[key]; ok {
		c.mu.Unlock()
		if onCached != nil {
			onCached()
		}
		return
	}
	if c.inFlight[key] {
		c.mu.Unlock()
		return
	}
	c.inFlight[key] = true
	c.mu.Unlock()
	go func() {
		decoded, err := decode()
		var thumbnail image.Image
		if err == nil && decoded != nil {
			thumbnail = Downsample(decoded, ThumbnailMaxDim)
		}
		fyne.Do(func() {
			c.mu.Lock()
			delete(c.inFlight, key)
			if thumbnail != nil {
				c.cache[key] = thumbnail
			}
			c.mu.Unlock()
			if thumbnail != nil && onCached != nil {
				onCached()
			}
		})
	}()
}

// Reset clears every cached entry. In-flight decodes are not cancelled
// but their result will land in the post-Reset cache. Callers wanting a
// hard cutoff (e.g. "discard everything tied to the previous file load")
// should construct a fresh ThumbnailCache instead.
func (c *ThumbnailCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = map[string]image.Image{}
	c.inFlight = map[string]bool{}
}

// Downsample produces an RGBA copy of src capped at maxDim on its
// longest edge. Aspect ratio preserved. Source images smaller than the
// cap are returned unchanged. Safe to call from a background goroutine.
func Downsample(src image.Image, maxDim int) image.Image {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= maxDim && h <= maxDim {
		return src
	}
	scale := float64(maxDim) / float64(w)
	if h > w {
		scale = float64(maxDim) / float64(h)
	}
	dstW := int(float64(w) * scale)
	dstH := int(float64(h) * scale)
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), src, bounds, xdraw.Src, nil)
	return dst
}
