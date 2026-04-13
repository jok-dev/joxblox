package app

import (
	"runtime"
	"sync"
	"sync/atomic"
)

func runResolveWorkers[T any, R any](
	items []T,
	keyFunc func(T) string,
	workerFunc func(T) R,
	workerMultiplier int,
	onProgress func(done int, total int),
	shouldCancel func() bool,
) map[string]R {
	if len(items) == 0 {
		return map[string]R{}
	}
	if workerMultiplier <= 0 {
		workerMultiplier = 2
	}

	jobs := make(chan T, len(items))
	for _, item := range items {
		jobs <- item
	}
	close(jobs)

	results := make(map[string]R, len(items))
	var resultsMutex sync.Mutex
	var completed atomic.Int64
	workerCount := min(runtime.NumCPU()*workerMultiplier, len(items))
	if workerCount <= 0 {
		workerCount = 1
	}

	var waitGroup sync.WaitGroup
	for range workerCount {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for item := range jobs {
				if shouldCancel != nil && shouldCancel() {
					return
				}
				key := keyFunc(item)
				result := workerFunc(item)
				resultsMutex.Lock()
				results[key] = result
				resultsMutex.Unlock()
				if onProgress != nil && (shouldCancel == nil || !shouldCancel()) {
					onProgress(int(completed.Add(1)), len(items))
				}
			}
		}()
	}
	waitGroup.Wait()
	return results
}
