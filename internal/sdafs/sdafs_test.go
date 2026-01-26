package sdafs

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/user"
	"path"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/jarcoal/httpmock"
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

func TestConfOptions(t *testing.T) {

	httpmock.Activate()
	httpmock.RegisterResponder("GET", "https://my.sda.local/metadata/datasets",
		httpmock.NewStringResponder(200, `["dataset1", "dataset2"]`))

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

	httpmock.RegisterResponder("GET", "https://my.sda.local/metadata/datasets",
		httpmock.NewStringResponder(500, "Something wrong"))

	c.CredentialsFile = "test.ini"
	c.RootURL = "https://my.sda.local"
	c.HTTPClient = http.DefaultClient
	sda, err = NewSDAfs(&c)

	assert.Nil(t, sda, "Got a sda when we should not")
	assert.NotNil(t, err, "No error when expected")

	httpmock.RegisterResponder("GET", "https://my.sda.local/metadata/datasets",
		httpmock.NewStringResponder(200, `["dataset1", "dataset2"]`))
	sda, err = NewSDAfs(&c)

	assert.NotNil(t, sda, "Did not get a sda when we should")
	assert.Nil(t, err, "Unexpected error")

	assert.NotNil(t, sda.inodes, "inodes in sda is nil")
	assert.Equal(t, 3, len(sda.inodes), "inodes in sda is unexpected length")

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

	httpmock.RegisterResponder("GET", "https://my.sda.local/metadata/datasets",
		httpmock.NewStringResponder(200, `["dataset1", "dataset2", "dataset3"]`))

	c := Conf{CredentialsFile: "test.ini",
		RootURL:    "https://my.sda.local/",
		HTTPClient: http.DefaultClient}

	sda, err := NewSDAfs(&c)
	assert.Equal(t, []string{"dataset1", "dataset2", "dataset3"}, sda.datasets)

	assert.Nil(t, err, "Unexpected error")
	assert.NotNil(t, sda, "Did not get a sda when we should")

	assert.NotNil(t, sda.inodes, "inodes in sda is nil")
	assert.Equal(t, 4, len(sda.inodes), "inodes in sda is unexpected length")

	httpmock.RegisterResponder("GET",
		"https://my.sda.local/metadata/datasets/dataset1/files",
		httpmock.NewStringResponder(200, `[]`))

	// NB: inodes is a map, not an array
	err = sda.checkLoaded(sda.inodes[2])
	assert.Nil(t, err, "Unexpected error")

	httpmock.RegisterResponder("GET",
		"https://my.sda.local/metadata/datasets/dataset2/files",
		httpmock.NewStringResponder(500, `[]`))
	err = sda.checkLoaded(sda.inodes[3])
	assert.NotNil(t, err, "Unexpected lack of error")

	httpmock.RegisterResponder("GET",
		"https://my.sda.local/metadata/datasets/dataset3/files",
		httpmock.NewStringResponder(200, `[
		{ "fileId": "1", 
			"filePath":"/file1",
			"fileSize": 14000
		}, 
		{ "fileId": "2", 
			"filePath":"/dir1/file2",
			"fileSize": 1000
		},
		{ "fileId": "3", 
			"filePath":"/dir1/file3",
			"fileSize": 1000
		}

			]`))
	err = sda.checkLoaded(sda.inodes[4])
	assert.Nil(t, err, "Unexpected error")
	assert.Equal(t, 8, len(sda.inodes), "inodes in sda is unexpected length")

	// Repeat should just return directly with the same visible result
	err = sda.checkLoaded(sda.inodes[4])
	assert.Nil(t, err, "Unexpected error")
	assert.Equal(t, 8, len(sda.inodes), "inodes in sda is unexpected length")

	// Test if DatasetsToShow is respected and works as expected

	sda.conf.DatasetsToShow = []string{"dataset3", "dataset1"}
	sda.getDatasets()
	assert.Equal(t, []string{"dataset1", "dataset3"}, sda.datasets)

	sda.conf.DatasetsToShow = []string{}
	sda.getDatasets()
	assert.Equal(t, []string{"dataset1", "dataset2", "dataset3"}, sda.datasets)

	sda.conf.DatasetsToShow = []string{"dataset4"}
	sda.getDatasets()
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
				w.Write([]byte("[]\n")) // nolint:errcheck
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
			w.Write([]byte("[]\n")) // nolint:errcheck
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
