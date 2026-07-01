package sdafs

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/user"
	"path"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jarcoal/httpmock"
	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/stretchr/testify/suite"
	"github.com/tj/assert"
)

func uid() uint32 {
	currentUser, err := user.Current()
	if err == nil {
		uid, err := strconv.Atoi(currentUser.Uid)
		if err != nil {
			return 0
		}
		return uint32(uid)
	}
	return 0
}

func gid() uint32 {
	currentUser, err := user.Current()
	if err == nil {
		gid, err := strconv.Atoi(currentUser.Gid)
		if err != nil {
			return 0
		}
		return uint32(gid)
	}
	return 0

}

func preLoadDatasetResponses(t *testing.T) {
	httpmock.RegisterResponder("GET", "https://my.sda.local/datasets",
		httpmock.NewStringResponder(200, `{"datasets": ["dataset1", "dataset2", "dataset3"], 
		"nextPageToken": null}`))

	httpmock.RegisterResponder("GET",
		"https://my.sda.local/datasets/dataset1",
		httpmock.NewStringResponder(200, `{"files": 10, "size":20, "date":"2026-06-30T14:06:58Z"}`))
	httpmock.RegisterResponder("GET",
		"https://my.sda.local/datasets/dataset2",
		httpmock.NewStringResponder(200, `{"files": 100, "size":200, "date":"2026-06-30T14:06:58Z"}`))
	httpmock.RegisterResponder("GET",
		"https://my.sda.local/datasets/dataset3",
		httpmock.NewStringResponder(200, `{"files": 1000, "size":2000, "date":"2023-06-30T14:06:58Z"}`))

	httpmock.RegisterResponder("GET",
		"https://my.sda.local/datasets/dataset1/files",
		httpmock.NewStringResponder(200, `{"files": [], "nextPageToken":null}`))

	httpmock.RegisterResponder("GET",
		"https://my.sda.local/datasets/dataset1",
		httpmock.NewStringResponder(200, `{"files": 0, "size":0, "date":"2026-06-30T01:01:01Z"}`))

	httpmock.RegisterResponder("GET",
		"https://my.sda.local/datasets/dataset2/files",
		httpmock.NewStringResponder(200, `{"files": [], "nextPageToken":null}`))

	httpmock.RegisterResponder("GET",
		"https://my.sda.local/datasets/dataset2",
		httpmock.NewStringResponder(200, `{"files": 0, "size":0, "date":"2026-06-30T02:02:02Z"}`))

	httpmock.RegisterResponder("GET",
		"https://my.sda.local/datasets/dataset3/files",
		httpmock.NewStringResponder(200, `{"files": [
		{ "fileId": "1", 
			"filePath":"file1",
			"size": 14000
		}, 
		{ "fileId": "2", 
			"filePath":"dir1/file2",
			"size": 1000,
			"downloadURL": "broken"
		},
		{ "fileId": "3",
			"downloadURL": "files/file3",
			"filePath":"dir1/file3",
			"size": 100
		}
			], "nextPageToken": null}`))

	httpmock.RegisterResponder("GET",
		"https://my.sda.local/datasets/dataset3",
		httpmock.NewStringResponder(200, `{"files": 3, "size":16000, "date":"2026-06-30T03:03:03Z"}`))

	httpmock.RegisterResponder("GET",
		"https://my.sda.local/broken/header",
		httpmock.NewStringResponder(500, `{}`))

	httpmock.RegisterResponder("GET",
		"https://my.sda.local/broken/content",
		httpmock.NewStringResponder(500, `{}`))
}

type cryptSuite struct {
	suite.Suite

	private          [32]byte
	public           [32]byte
	data             []byte
	encryptedContent []byte
	headerLength     int
}

func (s *cryptSuite) preloadFetchData(t *testing.T) {

	var err error
	s.private, s.public, err = keys.GenerateKeyPair()
	assert.Nil(t, err, "Key-pair generation failed")

	t.Logf("Keypair generated: private %v, public: %v", s.private, s.public)
	s.data = []byte("this is just some random bytes")

	httpmock.RegisterResponder("GET",
		"https://my.sda.local/files/file3/header",
		s.encryptingResponder)
	httpmock.RegisterResponder("GET",
		"https://my.sda.local/files/file3/content",
		s.encryptingResponder)
}

// getEncryptedContents generates an entire encrypted object as a []byte and
// returns that including an int that tells where the header ends
func (s *cryptSuite) getEncryptedContents(req *http.Request) ([]byte, int, error) {
	// We can't reencrypt every time since the header and content aren't fetched together

	if len(s.encryptedContent) > 0 {
		return s.encryptedContent, s.headerLength, nil
	}

	// t := s.T()

	buf := new(bytes.Buffer)

	publicKeyHeader := req.Header.Get("X-C4GH-Public-Key")
	if publicKeyHeader == "" {
		return nil, 0, fmt.Errorf("No public key provided")
	}

	nobase, err := base64.StdEncoding.DecodeString(publicKeyHeader)
	if err != nil {
		return nil, 0, fmt.Errorf("decoding base64 public key header string (%s) failed: %w",
			publicKeyHeader,
			err)
	}

	// t.Logf("public key decoded from base64: %s", string(nobase))

	public, err := keys.ReadPublicKey(bytes.NewBuffer(nobase))
	if err != nil {
		return nil, 0, fmt.Errorf("reading public key from header string (%s) failed: %w",
			publicKeyHeader,
			err)
	}

	// t.Logf("public key is %v", public)
	c4ghwriter, err := streaming.NewCrypt4GHWriter(buf, s.private, [][32]byte{public, s.public}, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("newcrypt4ghwriter failed: %w", err)
	}

	wrote, err := c4ghwriter.Write(s.data)
	if err != nil {
		return nil, 0, fmt.Errorf("writing encrypted failed: %w", err)
	}
	if wrote != len(s.data) {
		return nil, 0, fmt.Errorf("less data written (%v) than expected (%v)",
			wrote,
			len(s.data))
	}

	c4ghwriter.Close()
	content := buf.Bytes()

	headerBuffer := bytes.NewReader(bytes.Clone(content))
	_, err = headers.ReadHeader(headerBuffer)
	if err != nil {
		return nil, 0, fmt.Errorf("readheader failed: %w", err)
	}

	// Length of header is original size minus
	s.headerLength = len(content) - headerBuffer.Len()
	s.encryptedContent = content

	return bytes.Clone(s.encryptedContent), s.headerLength, nil
}

func (s *cryptSuite) encryptingResponder(req *http.Request) (*http.Response, error) {
	// encrypting responder deals with the dynamic request that needs to respond
	// for a requested
	t := s.T()
	t.Logf("handling request for contents: %s", req.URL.Path)
	r := http.Response{}

	content, headerLength, err := s.getEncryptedContents(req)
	if err != nil {
		return nil, fmt.Errorf("getting encrypted contents failed: %w", err)
	}

	contentStart, contentEnd, valid := decodeRange(t, req.Header)

	if !valid {
		contentStart = 0
		contentEnd = len(content) - s.headerLength
	}

	if headerLength+contentStart > len(content) {
		contentStart = len(content) - headerLength
		contentEnd = len(content) - headerLength
	}
	if headerLength+contentEnd > len(content) {
		contentEnd = len(content) - headerLength
	}

	t.Logf("Contents total is: %v", content)

	if strings.HasSuffix(req.URL.Path, "/header") {
		t.Logf("sending header %v", content[:headerLength])
		r.Body = io.NopCloser(bytes.NewReader(content[:headerLength]))
	} else {
		t.Logf("sending content %v", content[headerLength+contentStart:headerLength+contentEnd])
		t.Logf("Passing %v %v", contentStart, contentEnd)
		r.Body = io.NopCloser(bytes.NewReader(content[headerLength+contentStart : headerLength+contentEnd]))
	}

	r.StatusCode = 200
	return &r, nil
}

// decodes the Range
func decodeRange(t *testing.T, h http.Header) (start int, end int, valid bool) {

	hrange := h.Get("Range")
	if hrange == "" {
		return
	}

	parts := strings.FieldsFunc(hrange, func(r rune) bool {
		if r == '-' || r == '=' || r == ',' {
			return true
		}
		return false
	})

	if len(parts) != 3 {
		return
	}

	startv, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil {
		return
	}
	start = int(startv)

	endv, err := strconv.ParseInt(parts[2], 10, 32)
	if err != nil {
		return
	}

	end = int(endv)

	valid = true
	return
}

func TestConfOptions(t *testing.T) {

	httpmock.Activate()
	preLoadDatasetResponses(t)

	c := Conf{}
	c.CredentialsFile = "test.ini"
	c.RootURL = "https://my.sda.local"
	c.HTTPClient = http.DefaultClient
	c.GID = 1200
	c.UID = 999
	c.DirPerms = 0111
	c.FilePerms = 01

	sda, err := NewSDAfs(&c)

	assert.Nil(t, err, "Unexpected error")
	assert.NotNil(t, sda, "Did not get a sda when we should")

	assert.Equal(t, os.FileMode(0500), sda.DirPerms, "Did not use expected default dirperms")
	assert.Equal(t, os.FileMode(0400), sda.FilePerms, "Did not use expected default fileperms")

	assert.Equal(t, uid(), sda.Owner, "Did not pick up owner as expected")
	assert.Equal(t, gid(), sda.Group, "Did not pick up group as expected")

	c.SpecifyDirPerms = true
	c.SpecifyFilePerms = true
	c.SpecifyUID = true
	c.SpecifyGID = true

	sda, err = NewSDAfs(&c)

	assert.Nil(t, err, "Unexpected error")
	assert.NotNil(t, sda, "Did not get a sda when we should")

	assert.Equal(t, os.FileMode(0111), sda.DirPerms, "Did not use expected default dirperms")
	assert.Equal(t, os.FileMode(01), sda.FilePerms, "Did not use expected default fileperms")

	assert.Equal(t, uint32(999), sda.Owner, "Did not pick up owner as expected")
	assert.Equal(t, uint32(1200), sda.Group, "Did not pick up group as expected")
}

func TestNewSDAfs(t *testing.T) {
	sda, err := NewSDAfs(nil)

	assert.Nil(t, sda, "Got a sda when we should not")
	assert.NotNil(t, err, "No error when expected")

	c := Conf{}
	sda, err = NewSDAfs(&c)

	assert.Nil(t, sda, "Got a sda when we should not")
	assert.NotNil(t, err, "No error when expected")

	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	httpmock.RegisterResponder("GET", "https://my.sda.local/datasets",
		httpmock.NewStringResponder(500, "Something wrong"))

	c.CredentialsFile = "test.ini"
	c.RootURL = "https://my.sda.local"
	c.HTTPClient = http.DefaultClient
	sda, err = NewSDAfs(&c)

	assert.Nil(t, sda, "Got a sda when we should not")
	assert.NotNil(t, err, "No error when expected")

	preLoadDatasetResponses(t)
	sda, err = NewSDAfs(&c)
	assert.Nil(t, err, "Unexpected error")
	assert.NotNil(t, sda, "Did not get a sda when we should")

	assert.NotNil(t, sda.inodes, "inodes in sda is nil")
	assert.Equal(t, 4, len(sda.inodes), "inodes in sda is unexpected length")

	// Test certificate handling

	c.ExtraCAFile = path.Join(t.TempDir(), "does-not-exist")
	sda, err = NewSDAfs(&c)
	assert.Nil(t, sda, "Got a sda when we should not")
	assert.NotNil(t, err, "Unexpected lack of error")

	c.ExtraCAFile = "/dev/null"
	sda, err = NewSDAfs(&c)
	assert.Nil(t, sda, "Got a sda when we should not")
	assert.NotNil(t, err, "Unexpected lack of error")

	c.ExtraCAFile = "testchain.pem"
	sda, err = NewSDAfs(&c)
	assert.NotNil(t, sda, "Did not get a sda when we should")
	assert.Nil(t, err, "Unexpected error")
}

func TestDatasetLoad(t *testing.T) {

	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	preLoadDatasetResponses(t)

	c := Conf{CredentialsFile: "test.ini",
		RootURL:    "https://my.sda.local/",
		HTTPClient: http.DefaultClient}

	sda, err := NewSDAfs(&c)
	assert.Nil(t, err, "Unexpected error")
	assert.NotNil(t, sda.datasets, "No datasets although no error")
	assert.Equal(t, []string{"dataset1", "dataset2", "dataset3"}, sda.datasets)

	assert.NotNil(t, sda, "Did not get a sda when we should")

	assert.NotNil(t, sda.inodes, "inodes in sda is nil")
	assert.Equal(t, 4, len(sda.inodes), "inodes in sda is unexpected length")

	// NB: inodes is a map, not an array
	err = sda.checkLoaded(sda.inodes[2])
	assert.Nil(t, err, "Unexpected error")

	httpmock.RegisterResponder("GET",
		"https://my.sda.local/datasets/dataset2/files",
		httpmock.NewStringResponder(500, `[]`))
	err = sda.checkLoaded(sda.inodes[3])
	assert.NotNil(t, err, "Unexpected lack of error")

	err = sda.checkLoaded(sda.inodes[4])
	assert.Nil(t, err, "Unexpected error")
	assert.Equal(t, 8, len(sda.inodes), "inodes in sda is unexpected length")

	// Repeat should just return directly with the same visible result
	err = sda.checkLoaded(sda.inodes[4])
	assert.Nil(t, err, "Unexpected error")
	assert.Equal(t, 8, len(sda.inodes), "inodes in sda is unexpected length")

	// Test if DatasetsToShow is respected and works as expected

	sda.conf.DatasetsToShow = []string{"dataset3", "dataset1"}
	err = sda.getDatasets()
	assert.Nil(t, err, "Unexpected error")
	assert.Equal(t, []string{"dataset1", "dataset3"}, sda.datasets)

	sda.conf.DatasetsToShow = []string{}
	err = sda.getDatasets()
	assert.Nil(t, err, "Unexpected error")
	assert.Equal(t, []string{"dataset1", "dataset2", "dataset3"}, sda.datasets)

	sda.conf.DatasetsToShow = []string{"dataset4"}
	err = sda.getDatasets()
	assert.Nil(t, err, "Unexpected error")
	assert.Equal(t, []string{}, sda.datasets)

}

func TestSessionHandling(t *testing.T) {

	// We can't (sadly) use httpmock since we want to test the client as done
	// Instead we run a http server on a dynamic port (inspired by
	// https://www.yellowduck.be/posts/dynamically-allocating-ports-in-a-webserver-using-go)

	count := 0
	cookieval := uuid.New().String()

	listener, err := net.Listen("tcp", "[::1]:0")
	assert.Nil(t, err, "Unexpected error")
	assert.NotNil(t, listener, "Unexpected error")
	defer listener.Close() // nolint:errcheck

	http.HandleFunc("/",
		func(w http.ResponseWriter, r *http.Request) {

			count++
			t.Logf("http helper call %d", count)

			if count == 1 {
				http.SetCookie(w, &http.Cookie{
					Name:  "somecookie",
					Value: cookieval,
				})
				w.WriteHeader(200)
				w.Write([]byte(`{"datasets":[]}`)) // nolint:errcheck
				return
			}

			c, err := r.Cookie("somecookie")
			if c == nil || err != nil {
				t.Logf("No somecookie? err: %v", err)
				w.WriteHeader(500)
				return
			}

			if c.Value != cookieval {
				t.Logf("Bad cookie value for somecookie")
				w.WriteHeader(500)
				return
			}

			w.WriteHeader(200)
			w.Write([]byte(`{"datasets":[]}`)) // nolint:errcheck
		})

	go http.Serve(listener, nil) // nolint:errcheck

	c := Conf{}
	c.CredentialsFile = "test.ini"
	c.RootURL = fmt.Sprintf("http://[::1]:%d",
		listener.Addr().(*net.TCPAddr).Port)

	sda, err := NewSDAfs(&c)
	assert.Nil(t, err, "Unexpected error")
	assert.NotNil(t, sda, "Unexpected error")

	err = sda.getDatasets()
	assert.Nil(t, err, "Unexpected error")

}

type testReadSeekCloser struct {
}

func (testReadSeekCloser) Close() error {
	return nil
}

func (testReadSeekCloser) Seek(int64, int) (int64, error) {
	return 0, nil
}

func (testReadSeekCloser) Read(b []byte) (int, error) {
	return 0, nil
}

func TestReleaseFileHandle(t *testing.T) {
	s := &SDAfs{}
	s.handles = make(map[HandleID]io.ReadSeekCloser, 0)
	err := s.ReleaseFileHandle(context.TODO(), &ReleaseFileHandleOp{})
	assert.NotNil(t, err, "Releasing an unallocated handle should fail")

	s.handles[100] = testReadSeekCloser{}
	err = s.ReleaseFileHandle(context.TODO(), &ReleaseFileHandleOp{Handle: 100})
	assert.Nil(t, err, "Releasing an allocated handle should work")
}

func TestGetNewIdLocked(t *testing.T) {
	s := &SDAfs{}
	s.handles = make(map[HandleID]io.ReadSeekCloser, 0)
	id, err := s.getNewIDLocked()
	assert.Nil(t, err, "Getting a handle should work")

	_, exists := s.handles[id]

	assert.False(t, exists, "Handle from getNewIdLocked should not exist")
}

func (s *cryptSuite) TestOpenFile() {
	t := s.T()

	httpmock.Activate(t)
	preLoadDatasetResponses(t)
	s.preloadFetchData(t)

	c := Conf{
		CredentialsFile:  "test.ini",
		RootURL:          "https://my.sda.local",
		HTTPClient:       http.DefaultClient,
		GID:              1200,
		UID:              999,
		DirPerms:         0777,
		FilePerms:        0777,
		SpecifyDirPerms:  true,
		SpecifyFilePerms: true,
	}

	sda, err := NewSDAfs(&c)

	assert.Nil(t, err, "Unexpected error")
	assert.NotNil(t, sda, "Did not get a sda when we should")

	var datasetInode InodeID
	var fileInode InodeID

	for k, v := range sda.inodes {
		if v.dataset == "dataset3" {
			datasetInode = k
		}
	}

	openDirOp := OpenDirOp{
		Inode: InodeID(datasetInode),
	}

	err = sda.OpenDir(context.TODO(), &openDirOp)
	assert.Nil(t, err, "Unexpected error")

	readDirOp := ReadDirOp{
		Handle: openDirOp.Handle,
		Inode:  openDirOp.Inode,
	}
	err = sda.ReadDir(context.TODO(), &readDirOp)
	assert.Nil(t, err, "Unexpected error")

	for k, v := range sda.inodes {
		if v.dataset == "dataset3" && v.key == "dir1/file3" {
			fileInode = k
		}
	}

	openFileOp := OpenFileOp{Inode: fileInode}
	err = sda.OpenFile(context.TODO(), &openFileOp)
	assert.Nil(t, err, "Unexpected error from openfile")

	readFileOp := ReadFileOp{
		Handle: openFileOp.Handle,
		Dst:    make([]byte, 1024),
	}
	err = sda.ReadFile(context.TODO(), &readFileOp)
	assert.Nil(t, err, "Unexpected error from readfile")

}

func TestRunCryptSuite(t *testing.T) {
	s := cryptSuite{}

	suite.Run(t, &s)
}
