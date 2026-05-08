package loader

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math/bits"
	"sort"

	"joxblox/internal/roblox"

	xdraw "golang.org/x/image/draw"
)

const (
	dHashWidth              = 9
	dHashHeight             = 8
	similarityTopResultsCap = 100
)

type SimilarityMatch struct {
	ResultIndex int
	Distance    int
	ExactMatch  bool
}

func ComputeImageDHash(imageBytes []byte) (uint64, error) {
	src, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return 0, err
	}

	resized := image.NewGray(image.Rect(0, 0, dHashWidth, dHashHeight))
	xdraw.BiLinear.Scale(resized, resized.Bounds(), src, src.Bounds(), xdraw.Over, nil)

	var hash uint64
	bitIndex := 0
	for y := 0; y < dHashHeight; y++ {
		for x := 0; x < dHashWidth-1; x++ {
			left := resized.GrayAt(x, y).Y
			right := resized.GrayAt(x+1, y).Y
			if left > right {
				hash |= 1 << bitIndex
			}
			bitIndex++
		}
	}
	return hash, nil
}

// NormalMapDHashes carries a dHash built from R alone and one built from G
// alone — see ComputeImageDHashesForNormalMap for why two separate hashes
// beat a single combined hash on normal maps. Each is 56 bits; the
// caller compares with NormalMapHammingDistance to get a 0-112 distance
// where lower means more similar.
type NormalMapDHashes struct {
	R uint64
	G uint64
}

// NormalMapHammingDistance returns the combined R+G Hamming distance
// between two NormalMapDHashes results. Range is [0, 112] — twice a
// single-channel hash distance — so callers should scale the percentage
// display accordingly (pct = 100 - dist*100/112).
func NormalMapHammingDistance(a, b NormalMapDHashes) int {
	return dHashHammingDistance(a.R, b.R) + dHashHammingDistance(a.G, b.G)
}

// ComputeImageDHashesForNormalMap returns separate dHashes over the R and
// G channels (X and Y components of the encoded surface normal). Hashing
// R and G independently and summing their Hamming distances catches XY
// surface variation that the standard luminance dHash can't see —
// luminance Y mostly tracks B (the Z component), which is near-uniform
// across normal maps, leaving only weak silhouette information to
// discriminate two perceptually different surface details.
//
// Hashing them as a combined "(R+G)/2 grayscale" had a degenerate case:
// pixels where R and G swap relative magnitudes produce identical mixes
// even though the underlying surface direction is different. Two
// independent hashes avoid that — a position where R goes up and G goes
// down still flips bits in both R and G hashes.
func ComputeImageDHashesForNormalMap(imageBytes []byte) (NormalMapDHashes, error) {
	src, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return NormalMapDHashes{}, err
	}

	resized := image.NewRGBA(image.Rect(0, 0, dHashWidth, dHashHeight))
	xdraw.BiLinear.Scale(resized, resized.Bounds(), src, src.Bounds(), xdraw.Over, nil)

	channelAt := func(x, y, channel int) uint8 {
		return resized.Pix[resized.PixOffset(x, y)+channel]
	}

	hashChannel := func(channel int) uint64 {
		var hash uint64
		bitIndex := 0
		for y := 0; y < dHashHeight; y++ {
			for x := 0; x < dHashWidth-1; x++ {
				if channelAt(x, y, channel) > channelAt(x+1, y, channel) {
					hash |= 1 << bitIndex
				}
				bitIndex++
			}
		}
		return hash
	}

	return NormalMapDHashes{R: hashChannel(0), G: hashChannel(1)}, nil
}

func dHashHammingDistance(h1, h2 uint64) int {
	return bits.OnesCount64(h1 ^ h2)
}

type SimilarityRowSorter struct {
	Results  []ScanResult
	Indices  []int
	MatchSet map[int]int
}

func (s SimilarityRowSorter) Len() int { return len(s.Results) }

func (s SimilarityRowSorter) Less(a, b int) bool {
	distA := s.MatchSet[s.Indices[a]]
	distB := s.MatchSet[s.Indices[b]]
	if distA != distB {
		return distA < distB
	}
	return s.Results[a].AssetID < s.Results[b].AssetID
}

func (s SimilarityRowSorter) Swap(a, b int) {
	s.Results[a], s.Results[b] = s.Results[b], s.Results[a]
	s.Indices[a], s.Indices[b] = s.Indices[b], s.Indices[a]
}

func scanResultImageBytes(result ScanResult) []byte {
	if len(result.DownloadBytes) > 0 {
		return result.DownloadBytes
	}
	if result.Resource != nil {
		if content := result.Resource.Content(); len(content) > 0 {
			return content
		}
	}
	return nil
}

func ComputeSimilarityScores(queryHash uint64, querySHA256 string, results []ScanResult) []SimilarityMatch {
	matches := make([]SimilarityMatch, 0, len(results))
	for i, result := range results {
		if result.AssetTypeID != roblox.AssetTypeImage {
			continue
		}
		imgBytes := scanResultImageBytes(result)
		if len(imgBytes) == 0 {
			continue
		}

		if querySHA256 != "" && result.FileSHA256 == querySHA256 {
			matches = append(matches, SimilarityMatch{
				ResultIndex: i,
				Distance:    0,
				ExactMatch:  true,
			})
			continue
		}

		resultHash, err := ComputeImageDHash(imgBytes)
		if err != nil {
			continue
		}

		matches = append(matches, SimilarityMatch{
			ResultIndex: i,
			Distance:    dHashHammingDistance(queryHash, resultHash),
			ExactMatch:  false,
		})
	}
	sort.Slice(matches, func(a, b int) bool {
		if matches[a].Distance != matches[b].Distance {
			return matches[a].Distance < matches[b].Distance
		}
		return matches[a].ResultIndex < matches[b].ResultIndex
	})
	if len(matches) > similarityTopResultsCap {
		matches = matches[:similarityTopResultsCap]
	}
	return matches
}
