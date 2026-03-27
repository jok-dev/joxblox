package app

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math/bits"
	"sort"

	xdraw "golang.org/x/image/draw"
)

const (
	dHashWidth              = 9
	dHashHeight             = 8
	similarityTopResultsCap = 100
)

type similarityMatch struct {
	ResultIndex int
	Distance    int
	ExactMatch  bool
}

func computeImageDHash(imageBytes []byte) (uint64, error) {
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

func dHashHammingDistance(h1, h2 uint64) int {
	return bits.OnesCount64(h1 ^ h2)
}

type similaritySorter struct {
	results  []scanResult
	indices  []int
	matchSet map[int]int
}

func (s similaritySorter) Len() int { return len(s.results) }

func (s similaritySorter) Less(a, b int) bool {
	distA := s.matchSet[s.indices[a]]
	distB := s.matchSet[s.indices[b]]
	if distA != distB {
		return distA < distB
	}
	return s.results[a].AssetID < s.results[b].AssetID
}

func (s similaritySorter) Swap(a, b int) {
	s.results[a], s.results[b] = s.results[b], s.results[a]
	s.indices[a], s.indices[b] = s.indices[b], s.indices[a]
}

func scanResultImageBytes(result scanResult) []byte {
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

func computeSimilarityScores(queryHash uint64, querySHA256 string, results []scanResult) []similarityMatch {
	matches := make([]similarityMatch, 0, len(results))
	for i, result := range results {
		if result.AssetTypeID != assetTypeImage {
			continue
		}
		imgBytes := scanResultImageBytes(result)
		if len(imgBytes) == 0 {
			continue
		}

		if querySHA256 != "" && result.FileSHA256 == querySHA256 {
			matches = append(matches, similarityMatch{
				ResultIndex: i,
				Distance:    0,
				ExactMatch:  true,
			})
			continue
		}

		resultHash, err := computeImageDHash(imgBytes)
		if err != nil {
			continue
		}

		matches = append(matches, similarityMatch{
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
