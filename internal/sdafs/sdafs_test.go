package sdafs

import (
	"net/http"
	"os"
	"os/user"
	"strconv"
	"testing"

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
			"fileSize": 14000,
			"fileStatus": "ready",
			"lastModified": "2023-12-08T15:12:13.06156Z",
			"createdAt": "2023-12-08T15:12:13.06156Z"
		}, 
		{ "fileId": "2", 
			"filePath":"/dir1/file2",
			"fileSize": 1000,
			"fileStatus": "ready",
			"lastModified": "2023-12-08T15:12:13.06156Z",
			"createdAt": "2023-12-08T15:12:13.06156Z"
		},
		{ "fileId": "3", 
			"filePath":"/dir1/file3",
			"fileSize": 1000,
			"fileStatus": "pending",
			"lastModified": "2023-12-08T15:12:13.06156Z",
			"createdAt": "2023-12-08T15:12:13.06156Z"
		}

			]`))
	err = sda.checkLoaded(sda.inodes[4])
	assert.Nil(t, err, "Unexpected error")
	assert.Equal(t, 6, len(sda.inodes), "inodes in sda is unexpected length")

	// Repeat should just return directly with the same visible result
	err = sda.checkLoaded(sda.inodes[4])
	assert.Nil(t, err, "Unexpected error")
	assert.Equal(t, 6, len(sda.inodes), "inodes in sda is unexpected length")

}
