package httpreader

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"slices"
	"testing"

	"github.com/jarcoal/httpmock"

	"github.com/stretchr/testify/assert"
)

type testBytesBuffer struct {
	bytes.Buffer
}

func (*testBytesBuffer) Close() error {
	return nil
}

func testFailResponder(r *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("This failed")
}

func testDataResponder(r *http.Request) (*http.Response, error) {

	data := make([]byte, 14000)

	key := r.Header.Get("Client-Public-Key")
	if key == "" {
		// Fail if we don't get a good header
		resp := http.Response{StatusCode: http.StatusInternalServerError}
		return &resp, nil
	}

	resp := http.Response{StatusCode: http.StatusOK}

	m := r.URL.Query()

	startC, startSpec := m["startCoordinate"]
	endC, endSpec := m["endCoordinate"]

	// rangeHeader, rangeSet := r.Header["Range"]

	if !startSpec && !endSpec {

		resp.Body = io.NopCloser(bytes.NewBuffer(data))
		return &resp, nil
	}

	var start, end int
	_, _ = fmt.Sscanf(startC[0], "%d", &start)
	_, _ = fmt.Sscanf(endC[0], "%d", &end)

	resp.Body = io.NopCloser(bytes.NewBuffer(data[start : end+1]))

	return &resp, nil
}

func clientPublicKeyHeader() *http.Header {
	h := make(http.Header)
	h.Add("Client-Public-Key", "thisisset")
	return &h
}

func TestHttpReader(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	url := "https://my.sda.local/url"
	httpmock.RegisterResponder("GET", url,
		testDataResponder)

	reader, err := NewHttpReader(url, "", 14000, http.DefaultClient, clientPublicKeyHeader())
	assert.Nil(t, err, "Backend failed")

	if reader == nil {
		t.Error("reader that should be usable is not, bailing out")
		return
	}

	var readBackBuffer [4096]byte
	seeker := reader

	_, err = seeker.Read(readBackBuffer[0:4096])

	// POSIX is more allowing
	_, err = seeker.Seek(95000, io.SeekStart)
	assert.NotNil(t, err, "Seek didn't fail when it should")

	_, err = seeker.Seek(-95000, io.SeekStart)
	assert.NotNil(t, err, "Seek didn't fail when it should")

	_, err = seeker.Seek(-95000, io.SeekCurrent)
	assert.NotNil(t, err, "Seek didn't fail when it should")

	_, err = seeker.Seek(95000, io.SeekCurrent)
	assert.NotNil(t, err, "Seek didn't fail when it should")

	_, err = seeker.Seek(95000, io.SeekEnd)
	assert.NotNil(t, err, "Seek didn't fail when it should")

	_, err = seeker.Seek(-95000, io.SeekEnd)
	assert.NotNil(t, err, "Seek didn't fail when it should")

	_, err = seeker.Seek(0, 4)
	assert.NotNil(t, err, "Seek didn't fail when it should")

	offset, err := seeker.Seek(15, io.SeekStart)
	assert.Nil(t, err, "Seek failed when it shouldn't")
	assert.Equal(t, int64(15), offset, "Seek did not return expected offset")

	offset, err = seeker.Seek(5, io.SeekCurrent)
	assert.Nil(t, err, "Seek failed when it shouldn't")
	assert.Equal(t, int64(20), offset, "Seek did not return expected offset")

	offset, err = seeker.Seek(-5, io.SeekEnd)
	assert.Nil(t, err, "Seek failed when it shouldn't")
	assert.Equal(t, int64(13995), offset, "Seek did not return expected offset")

	n, err := seeker.Read(readBackBuffer[0:4096])
	assert.Equal(t, 5, n, "Unexpected amount of read bytes")
	assert.Nil(t, err, "Read failed when it shouldn't")

	n, err = seeker.Read(readBackBuffer[0:4096])

	assert.Equal(t, io.EOF, err, "Expected EOF")
	assert.Equal(t, 0, n, "Unexpected amount of read bytes")

	offset, err = seeker.Seek(0, io.SeekEnd)
	assert.Nil(t, err, "Seek failed when it shouldn't")
	assert.Equal(t, int64(14000), offset, "Seek did not return expected offset")

	n, err = seeker.Read(readBackBuffer[0:4096])
	assert.Equal(t, 0, n, "Unexpected amount of read bytes")
	assert.Equal(t, io.EOF, err, "Read returned unexpected error when EOF")

	offset, err = seeker.Seek(6302, io.SeekStart)
	assert.Nil(t, err, "Seek failed")
	assert.Equal(t, int64(6302), offset, "Seek did not return expected offset")

	n = 0
	for i := 0; i < 500000 && n == 0 && err == nil; i++ {
		// Allow 0 sizes while waiting for prefetch
		n, err = seeker.Read(readBackBuffer[0:4096])
	}

	assert.Equal(t, 4096, n, "Read did not return expected amounts of bytes for %v", seeker)
	// assert.Equal(t, writeData[2:], readBackBuffer[:12], "did not read back data as expected")
	assert.Nil(t, err, "unexpected error when reading back data")

	offset, err = seeker.Seek(6302, io.SeekStart)
	assert.Nil(t, err, "unexpected error when seeking to read back data")
	assert.Equal(t, int64(6302), offset, "returned offset wasn't expected")

	largeBuf := make([]byte, 65536)
	readLen, err := seeker.Read(largeBuf)
	assert.Equal(t, 7698, readLen, "did not read back expected amount of data")
	assert.Nil(t, err, "unexpected error when reading back data")

}

func TestHttpReaderPrefetches(t *testing.T) {
	// Some special tests here, messing with internals to expose behaviour

	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	url := "https://my.sda.local/url"
	failurl := "https://fail.sda.local/url"

	httpmock.RegisterResponder("GET", url,
		testDataResponder)

	httpmock.RegisterResponder("GET", failurl,
		testFailResponder)

	var readBackBuffer [4096]byte
	seeker, err := NewHttpReader(url, "token", 14000, http.DefaultClient, clientPublicKeyHeader())

	_, err = seeker.Read(readBackBuffer[0:4096])
	// assert.Equal(t, writeData, readBackBuffer[:14], "did not read back data as expected")
	assert.Nil(t, err, "read returned unexpected error")

	err = seeker.Close()
	assert.Nil(t, err, "unexpected error when closing")

	reader, err := NewHttpReader(url, "token", 14000, http.DefaultClient,
		clientPublicKeyHeader())
	assert.Nil(t, err, "unexpected error when creating reader")
	assert.NotNil(t, reader, "unexpected error when creating reader")

	s := reader

	s.prefetchAt(0)
	assert.Equal(t, 1, len(cache[url]), "nothing cached after prefetch")
	// Clear cache
	cache[url] = cache[url][:0]

	prefetches[url] = []int64{}
	t.Logf("Cache %v, outstanding %v", cache[url], prefetches[url])

	for i := 0; i < 30; i++ {
		cache[url] = append(cache[url], HttpReaderCacheBlock{90000000, int64(0), nil})
	}
	s.prefetchAt(0)
	assert.Equal(t, 9, len(cache[url]), "unexpected length of cache after prefetch")

	prefetches[url] = []int64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	s.removeFromOutstanding(9)
	assert.Equal(t, prefetches[url], []int64{0, 1, 2, 3, 4, 5, 6, 7, 8}, "unexpected outstanding prefetches after remove")
	s.removeFromOutstanding(19)
	assert.Equal(t, prefetches[url], []int64{0, 1, 2, 3, 4, 5, 6, 7, 8}, "unexpected outstanding prefetches after remove")
	s.removeFromOutstanding(5)
	// We don't care about the internal order, sort for simplicity
	slices.Sort(prefetches[url])
	assert.Equal(t, prefetches[url], []int64{0, 1, 2, 3, 4, 6, 7, 8}, "unexpected outstanding prefetches after remove")
}

func TestHttpReaderFailures(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	failurl := "https://fail.sda.local/url"

	httpmock.RegisterResponder("GET", failurl,
		testFailResponder)

	url := "https://my.sda.local/url"

	httpmock.RegisterResponder("GET", failurl,
		testDataResponder)

	var readBackBuffer [4096]byte

	failreader, err := NewHttpReader(failurl, "token", 14000, http.DefaultClient, nil)
	// We don't fail yet
	assert.Nil(t, err, "unexpected error when creating reader")
	assert.NotNil(t, failreader, "unexpected error when creating reader")

	n, err := failreader.Read(readBackBuffer[:])
	assert.Equal(t, 0, n, "unexpected data read from broken reader")
	assert.NotNil(t, err, "unexpected lack of error when using broken reader")

	noheaderreader, err := NewHttpReader(url, "token", 14000, http.DefaultClient, nil)
	// We don't fail yet
	assert.Nil(t, err, "unexpected error when creating reader")
	assert.NotNil(t, noheaderreader, "unexpected error when creating reader")

	n, err = failreader.Read(readBackBuffer[:])
	assert.Equal(t, 0, n, "unexpected data read from broken reader")
	assert.NotNil(t, err, "unexpected lack of error when using broken reader")
}

func testDoRequestResponder(r *http.Request) (*http.Response, error) {

	data := make([]byte, 0)

	auth := r.Header.Get("Authorization")
	if auth == "" {
		// Fail if we don't get a good header
		resp := http.Response{StatusCode: http.StatusInternalServerError}
		return &resp, nil
	}

	a := r.Header.Get("HeaderA")
	b := r.Header.Get("HeaderB")
	data = append(data, []byte("Auth:"+auth+" ")...)
	data = append(data, []byte("A:"+a+" ")...)
	data = append(data, []byte("B:"+b+" ")...)

	resp := http.Response{StatusCode: http.StatusOK}

	resp.Body = io.NopCloser(bytes.NewBuffer(data))
	return &resp, nil
}

func TestDoRequest(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	checkurl := "https://check.sda.local/url"

	httpmock.RegisterResponder("GET", checkurl,
		testDoRequestResponder)

	r := HttpReader{
		fileURL: checkurl,
		token:   "token",
		client:  http.DefaultClient,
	}
	resp, err := r.doRequest()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Unexpected status code for doRequest")
	assert.Nil(t, err, "Unexpected error from doRequest")

	msg, err := io.ReadAll(resp.Body)
	assert.Equal(t, []byte("Auth:Bearer token A: B: "), msg, "Unexpected headers from doRequest")

	h := http.Header{}
	r.extraHeaders = &h
	h.Add("HeaderA", "SomeGoose")

	resp, err = r.doRequest()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Unexpected status code for doRequest")
	assert.Nil(t, err, "Unexpected error from doRequest")

	msg, err = io.ReadAll(resp.Body)
	assert.Equal(t, []byte("Auth:Bearer token A:SomeGoose B: "), msg, "Unexpected headers from doRequest")

	h.Add("HeaderB", "SomeELSE")

	resp, err = r.doRequest()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Unexpected status code for doRequest")
	assert.Nil(t, err, "Unexpected error from doRequest")

	msg, err = io.ReadAll(resp.Body)
	assert.Equal(t, []byte("Auth:Bearer token A:SomeGoose B:SomeELSE "), msg, "Unexpected headers from doRequest")

}
