// Package provides a io.Reader implementation on top of http
package httpreader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/ristretto/v2"
)

const cacheLifeTime = 4 * time.Hour

type Conf struct {
	Token      string
	Client     *http.Client
	Headers    *http.Header
	MaxRetries int
	ChunkSize  uint64
	CacheSize  uint64
}

type Request struct {
	FileURL    string
	ObjectSize uint64
}

const traceLevel = -12

// TODO: Make something better that does TTLing

// type cacheKey struct {
// 	fileURL string
// 	offset  uint64
// }

var cache *ristretto.Cache[string, *CacheBlock]

var prefetches map[string][]uint64
var prefetchLock sync.Mutex

var idLock sync.Mutex
var nextID uint64
var once sync.Once

// CacheBlock is used to keep track of cached data
type CacheBlock struct {
	key    *string
	start  uint64
	length uint64
	data   []byte
}

func (r *HTTPReader) init() {
	once.Do(func() {
		var err error
		// Make the map if not done yet
		config := ristretto.Config[string, *CacheBlock]{
			NumCounters: int64(r.conf.CacheSize / r.prefetchSize() * 10),
			MaxCost:     int64(r.conf.CacheSize),
			BufferItems: 64,
		}
		cache, err = ristretto.NewCache(&config)

		if err != nil {
			slog.Error("Couldn't set up cache, running without, this will "+
				"impact performance impact",
				"error", err)
		}
		prefetches = make(map[string][]uint64)
	})

	prefetchLock.Lock()
	_, found := prefetches[r.fileURL]
	if !found {
		prefetches[r.fileURL] = make([]uint64, 0, 32)
	}
	prefetchLock.Unlock()
}

// HTTPReader is the vehicle to keep track of needed state for the reader
type HTTPReader struct {
	conf          *Conf
	currentOffset uint64
	fileURL       string
	objectSize    uint64
	lock          sync.Mutex
	id            uint64
}

// NewHTTPReader allocates a unique id and returns a HTTPReader from the
// configuration struct (expected to be common) and the request
func NewHTTPReader(conf *Conf, request *Request,
) (*HTTPReader, error) {
	idLock.Lock()
	slog.Info("Creating reader", "url", request.FileURL, "objectsize", request.ObjectSize, "id", nextID)
	reader := &HTTPReader{
		conf:       conf,
		fileURL:    request.FileURL,
		objectSize: request.ObjectSize,
		id:         nextID,
	}
	nextID += 1
	idLock.Unlock()

	reader.init()

	return reader, nil
}

// Close is a no-op
func (r *HTTPReader) Close() (err error) {
	slog.Log(context.Background(),
		traceLevel,
		"Close",
		"url", r.fileURL,
		"id", r.id)

	return nil
}

// prefetchSzie returns the prefetch size to use, sanity chcked
func (r *HTTPReader) prefetchSize() uint64 {
	if r.conf.ChunkSize < 64 {
		return 64 * 1024
	}
	return uint64(r.conf.ChunkSize * 1024)
}

// doFetch fetches the specified range of the url from the HTTPReader
func (r *HTTPReader) doFetch(rangeSpec string) ([]byte, error) {
	slog.Log(context.Background(),
		traceLevel,
		"Making fetch request",
		"url", r.fileURL,
		"range", rangeSpec,
		"id", r.id)

	useURL := r.fileURL

	if rangeSpec != "" {
		// Archive being broken regarding ranges for now,
		// use query parameters as well to trigger partial file delivery

		byteRange := strings.TrimPrefix(rangeSpec, "bytes=")
		byteRanges := strings.Split(byteRange, "-")

		if byteRanges[0] == "" {
			useURL += "?startCoordinate=0"
		} else {
			useURL += "?startCoordinate=" + byteRanges[0]
		}

		if byteRanges[1] == "" {
			useURL += "&endCoordinate=" + fmt.Sprintf("%d", r.objectSize)
		} else {
			useURL += "&endCoordinate=" + byteRanges[1]
		}
	}

	req, err := http.NewRequest("GET", useURL, nil)
	if err != nil {
		return nil, fmt.Errorf(
			"Couldn't make request for %s: %v",
			r.fileURL, err)
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.conf.Token))

	if rangeSpec != "" {
		req.Header.Add("Range", rangeSpec)
	}

	if r.conf.Headers != nil {
		for h := range *r.conf.Headers {
			for _, v := range r.conf.Headers.Values(h) {
				req.Header.Add(h, v)
			}
		}
	}

	beforeRequest := time.Now()
	resp, err := r.conf.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf(
			"Error while making request for %s: %v",
			r.fileURL, err)
	}
	duration := time.Since(beforeRequest)

	slog.Log(context.Background(),
		traceLevel,
		"Fetch returned",
		"url", r.fileURL,
		"id", r.id,
		"range", rangeSpec,
		"status", resp.StatusCode,
		"duration", duration,
	)

	if resp.StatusCode != http.StatusOK {
		errData, err := io.ReadAll(resp.Body)

		retErr := fmt.Errorf(
			"Unexpected status code for request for %s: %d (%s)",
			r.fileURL, resp.StatusCode, string(errData))

		if err != nil {
			retErr = fmt.Errorf(
				"Unexpected status code for request for %s: %d "+
					"(no more information, got %v while trying to read)",
				r.fileURL, resp.StatusCode, err)
		}

		_ = resp.Body.Close()
		return nil, retErr
	}

	dat, err := io.ReadAll(resp.Body)

	return dat, err
}

func (r *HTTPReader) getCacheKey(offset uint64) string {
	return fmt.Sprintf("%s%c%d",
		r.fileURL,
		'\x00',
		offset/r.prefetchSize())
}

func (r *HTTPReader) isInCache(offset uint64) bool {
	key := r.getCacheKey(offset)

	slog.Log(context.Background(),
		traceLevel,
		"Checking if offset is in cache",
		"url", r.fileURL,
		"id", r.id,
		"offset", offset,
		"key", key)

	cache.Wait()

	entry, found := cache.Get(key)

	if found && *entry.key == key && offset >= entry.start && offset < (entry.start+entry.length) {

		slog.Log(context.Background(),
			traceLevel,
			"Offset found in cache",
			"url", r.fileURL,
			"id", r.id,
			"offset", offset,
			"key", key)
		return true
	}

	slog.Log(context.Background(),
		traceLevel,
		"Offset not found in cache",
		"url", r.fileURL,
		"offset", offset,
		"key", key)

	return false
}

func (r *HTTPReader) prefetchAt(waitBefore time.Duration, offset uint64) {
	slog.Log(context.Background(),
		traceLevel,
		"Doing prefetch",
		"url", r.fileURL,
		"offset", offset)

	if waitBefore > 0 {
		time.Sleep(waitBefore)
	}

	if offset >= r.objectSize {
		// Don't try to prefetch what doesn't exist
		return
	}

	if r.isInCache(offset) {
		return
	}

	// Not found in cache, we should fetch the data

	prefetchSize := r.prefetchSize()

	if r.isPrefetching(offset) {
		// We're already fetching this
		return
	}

	r.addToOutstanding(offset)

	wantedRange := fmt.Sprintf("bytes=%d-%d", offset, min(offset+prefetchSize-1, r.objectSize-1))

	prefetchedData, err := r.doFetch(wantedRange)

	r.removeFromOutstanding(offset)

	if err != nil {

		if waitBefore == 0 {
			waitBefore = 1 * time.Second
		} else {
			waitBefore = 2 * waitBefore
		}

		if waitBefore < time.Duration(math.Pow(2, float64(r.conf.MaxRetries)))*time.Second {
			r.prefetchAt(waitBefore, offset)
		}

		return
	}

	r.addToCache(&CacheBlock{start: offset,
		length: uint64(len(prefetchedData)),
		data:   prefetchedData})
}

func (r *HTTPReader) Seek(offset int64, whence int) (int64, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	iCurrent := int64(r.currentOffset) // #nosec G115

	switch whence {
	case io.SeekStart:
		if offset < 0 {
			return iCurrent, fmt.Errorf("Invalid offset %v- can't be negative when seeking from start", offset)
		}
		// #nosec G115
		if offset > int64(r.objectSize) {
			return iCurrent, fmt.Errorf("Invalid offset %v - beyond end of object (size %v)", offset, r.objectSize)
		}

		r.currentOffset = uint64(offset)
		go r.prefetchAt(0*time.Second, r.currentOffset)

		return offset, nil

	case io.SeekCurrent:
		if iCurrent+offset < 0 {
			return iCurrent, fmt.Errorf("Invalid offset %v from %v would be be before start", offset, r.currentOffset)
		}
		// #nosec G115
		if offset > int64(r.objectSize) {
			return iCurrent, fmt.Errorf("Invalid offset - %v from %v would end up beyond of object %v", offset, r.currentOffset, r.objectSize)
		}

		r.currentOffset = uint64(int64(r.currentOffset) + offset) // #nosec G115
		go r.prefetchAt(0*time.Second, r.currentOffset)

		return int64(r.currentOffset), nil // #nosec G115

	case io.SeekEnd:
		// #nosec G115
		if int64(r.objectSize)+offset < 0 {
			return iCurrent, fmt.Errorf("Invalid offset %v from end in %v bytes object, would be before file start", offset, r.objectSize)
		}

		// #nosec G115
		if int64(r.objectSize)+offset > int64(r.objectSize) {
			return iCurrent, fmt.Errorf("Invalid offset %v from end in %v bytes object", offset, r.objectSize)
		}

		r.currentOffset = uint64(int64(r.objectSize) + offset) // #nosec G115
		go r.prefetchAt(0*time.Second, r.currentOffset)

		return int64(r.currentOffset), nil // #nosec G115
	}

	return iCurrent, fmt.Errorf("Bad whence")
}

// addToCache adds a prefetch to the list of outstanding prefetches once it's no longer active
func (r *HTTPReader) addToCache(cacheBlock *CacheBlock) {
	slog.Log(context.Background(),
		traceLevel,
		"Adding to cache",
		"url", r.fileURL,
		"offset", cacheBlock.start,
		"length", cacheBlock.length,
		"key", r.getCacheKey(cacheBlock.start))

	key := r.getCacheKey(cacheBlock.start)
	cacheBlock.key = &key

	cache.SetWithTTL(key, cacheBlock, int64(cacheBlock.length), cacheLifeTime)
}

// addToOutstanding adds a prefetch to the list of outstanding prefetches once it's no longer active
func (r *HTTPReader) addToOutstanding(toAdd uint64) {
	slog.Log(context.Background(),
		traceLevel,
		"Adding to outstanding",
		"url", r.fileURL,
		"offset", toAdd)
	prefetchLock.Lock()
	prefetches[r.fileURL] = append(prefetches[r.fileURL], toAdd)
	prefetchLock.Unlock()
}

// removeFromOutstanding removes a prefetch from the list of outstanding prefetches once it's no longer active
func (r *HTTPReader) removeFromOutstanding(toRemove uint64) {
	slog.Log(context.Background(),
		traceLevel,
		"Removing from outstanding",
		"url", r.fileURL,
		"offset", toRemove)

	prefetchLock.Lock()
	defer prefetchLock.Unlock()

	switch len(prefetches[r.fileURL]) {
	case 0:
		// Nothing to do
	case 1:
		// Check if it's the one we should remove
		if prefetches[r.fileURL][0] == toRemove {
			prefetches[r.fileURL] = prefetches[r.fileURL][:0]
		}

	default:
		remove := 0
		found := false
		for i, j := range prefetches[r.fileURL] {
			if j == toRemove {
				remove = i
				found = true
			}
		}
		if found {
			prefetches[r.fileURL][remove] = prefetches[r.fileURL][len(prefetches[r.fileURL])-1]
			prefetches[r.fileURL] = prefetches[r.fileURL][:len(prefetches[r.fileURL])-1]
		}
	}
}

// isPrefetching checks if the data is already being fetched
func (r *HTTPReader) isPrefetching(offset uint64) bool {
	prefetchLock.Lock()
	defer prefetchLock.Unlock()

	// Walk through the outstanding prefetches
	for _, p := range prefetches[r.fileURL] {
		if offset >= p && offset < p+r.prefetchSize() {
			// At least some of this read is already being fetched

			return true
		}
	}

	return false
}

func (r *HTTPReader) Read(dst []byte) (n int, err error) {

	r.lock.Lock()
	start := r.currentOffset
	slog.Log(context.Background(),
		traceLevel,
		"Read entered",
		"url", r.fileURL,
		"id", r.id,
		"offset", start,
		"maxlength", len(dst))

	r.lock.Unlock()

	if start >= r.objectSize {
		// For reading when there is no more data, just return EOF
		return 0, io.EOF
	}

	// Check if we're already fetching this data
	for r.isPrefetching(start) {
		// Prefetch in progress, wait a while and see if it's finished

		// TODO: runtime.Gosched() instead?
		time.Sleep(1 * time.Millisecond)
	}

	key := r.getCacheKey(start)
	slog.Log(context.Background(),
		traceLevel,
		"Checking cache",
		"url", r.fileURL,
		"id", r.id,
		"offset", start,
		"key", key)

	// Worth waiting for quiescence if we can skip a fetch
	cache.Wait()
	p, found := cache.Get(key)
	if found && *p.key == key &&
		(start >= p.start && start < p.start+p.length) {
		// At least part of the data is here

		slog.Log(context.Background(),
			traceLevel,
			"Hit in cache",
			"url", r.fileURL,
			"id", r.id,
			"offset", start,
			"key", key,
			"cachestart", p.start,
			"cacheend", p.start+p.length)

		offsetInBlock := start - p.start

		// Pull out wanted data (as much as we have)
		n = copy(dst, p.data[offsetInBlock:])
		r.lock.Lock()
		r.currentOffset = start + uint64(n) // #nosec G115
		r.lock.Unlock()

		// Prefetch the next bit
		go r.prefetchAt(0*time.Second, r.currentOffset)

		return n, nil
	}

	slog.Log(context.Background(),
		traceLevel,
		"Not found in cache, need to read",
		"url", r.fileURL,
		"id", r.id,
		"offset", start,
		"key", key)

	// Not found in cache, need to fetch data
	wantedRange := fmt.Sprintf("bytes=%d-%d", r.currentOffset, min(r.currentOffset+r.prefetchSize()-1, r.objectSize-1))

	var data []byte
	var wait time.Duration = 1

	for tries := 0; tries <= r.conf.MaxRetries; tries++ {
		r.addToOutstanding(start)
		data, err = r.doFetch(wantedRange)
		r.removeFromOutstanding(start)

		if err == nil {
			break
		}

		// Something went wrong, wait and retry
		time.Sleep(wait * time.Second)
		wait *= 2
	}

	if err != nil {
		return 0, err
	}

	// Add to cache
	cacheBytes := bytes.Clone(data)
	r.addToCache(&CacheBlock{start: start,
		length: uint64(len(cacheBytes)),
		data:   cacheBytes})

	b := bytes.NewBuffer(data)
	n, err = b.Read(dst)

	r.lock.Lock()
	r.currentOffset = start + uint64(n) // #nosec G115
	r.lock.Unlock()

	go r.prefetchAt(0*time.Second, r.currentOffset)

	return n, err
}
