package csidriver

import (
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tj/assert"
)

func TestGetTokenfilePath(t *testing.T) {

	path := "PATHSTART"
	d := Driver{
		tokenDir: &path,
	}

	v := volumeInfo{ID: "IDENTIFIER"}
	tp := d.getTokenfilePath(&v)

	assert.Equal(t, "PATHSTART/token-IDENTIFIER", tp, "Unexpected path")
}

func TestWriteToken(t *testing.T) {

	path := "."
	nowrite := "/cantwritehere/some"
	d := Driver{
		tokenDir: &path,
	}

	v := volumeInfo{
		ID:     uuid.New().String(),
		secret: uuid.New().String(),
	}

	err := writeToken(&d, &v)
	assert.Nil(t, err, "writeToken failed")

	filename := d.getTokenfilePath(&v)
	defer os.Remove(filename) // nolint:errcheck
	// Clean up afterwards

	f, err := os.Open(filename)
	assert.Nil(t, err, "Error while opening token file")

	data, err := io.ReadAll(f)
	assert.Nil(t, err, "Error while reading token file")
	defer f.Close() // nolint:errcheck

	assert.Equal(t,
		fmt.Sprintf("access_token = %s\n\n", v.secret),
		string(data),
		"Token in file not as expected")

	d.tokenDir = &nowrite
	err = writeToken(&d, &v)
	assert.NotNil(t, err, "writeToken should have failed but did not")

}

func TestEnsureTargetDir(t *testing.T) {

	d := Driver{}
	v := volumeInfo{}
	nowrite := "/cantwrite/here"
	exists := "/bin"
	good := "testdirensuretarget" + uuid.New().String()

	v.path = nowrite
	err := d.ensureTargetDir(&v)
	assert.NotNil(t, err, "Unexpected lack of error from EnsureTargetDir fail")

	v.path = exists
	err = d.ensureTargetDir(&v)
	assert.Nil(t, err, "Unexpected error from EnsureTargetDir exist case")

	defer os.Remove(good) // nolint:errcheck

	v.path = good
	err = d.ensureTargetDir(&v)
	assert.Nil(t, err, "Unexpected error from EnsureTargetDir good case")

	s, err := os.Stat(good)
	assert.Nil(t, err, "Unexpected error from start for EnsureTargetDir "+
		"good case")

	assert.Equal(t, true, s.IsDir(), "Created directory is not directory")
}

func TestUnmount(t *testing.T) {

	d := Driver{
		isMountPoint: isMountPoint,
	}

	// Non-existant is fine
	v := volumeInfo{path: "/does/not/exist"}
	err := unmount(&d, &v)
	assert.Nil(t, err, "Nonexistant path is okay for unmount")

	// So is unmounted existant
	v.path = "testdirunmount" + uuid.New().String()
	err = d.ensureTargetDir(&v)
	assert.Nil(t, err, "ensureTargetDir failed for testing unmount")

	defer os.Remove(v.path) // nolint:errcheck

	err = unmount(&d, &v)
	assert.Nil(t, err, "Not mounted path is okay for unmount")
	_, err = os.Stat(v.path)
	assert.NotNil(t, err, "unmount should have deleted the mount directory")

	v.path = "/bin"
	err = unmount(&d, &v)
	assert.NotNil(t, err, "Cleanup should have failed and been signalled")

	_, err = os.Stat("/proc")
	if err != nil {
		t.Log("Not normal linux; skipping unmount failure check")
		return
	}

	v.path = "/proc"
	err = unmount(&d, &v)
	assert.NotNil(t, err, "We should have gotten a failure")
}

func TestIsMountPoint(t *testing.T) {

	d := Driver{}
	v := volumeInfo{path: "/does/not/exist"}
	ismp := isMountPoint(&d, &v)
	assert.False(t, ismp, "Nonexistant path is not mount point")

	_, err := os.Stat("/proc")
	if err != nil {
		t.Log("Not normal linux; skipping mount point check")
		return
	}

	v.path = "/proc"
	ismp = isMountPoint(&d, &v)
	assert.True(t, ismp, "Actual mount point should yield true")
}

func TestDoMount(t *testing.T) {

	tokenDir := "/some/dir"
	truePath := "/bin/true"
	falsePath := "/bin/false"
	nonexistant := "/does/not/exist"
	d := Driver{
		tokenDir:     &tokenDir,
		sdafsPath:    &nonexistant,
		isMountPoint: isMountPoint,
		maxWaitMount: 30 * time.Millisecond,
	}
	v := volumeInfo{
		path:    "/does/not/exist",
		context: make(map[string]string),
	}

	err := doMount(&d, &v)
	assert.NotNil(t, err, "Should have error fixing the directory")

	_, err = os.Stat("/proc")
	if err != nil {
		t.Log("Not normal linux; skipping mount point check")
		return
	}

	v.path = "/proc"
	err = doMount(&d, &v)
	assert.Nil(t, err, "doMount should work when path is already mounted")

	v.path = "testdirmount" + uuid.New().String()
	defer os.Remove(v.path) // nolint:errcheck

	err = doMount(&d, &v)
	assert.NotNil(t, err, "Should fail if sdafs is bad")

	d.sdafsPath = &falsePath
	err = doMount(&d, &v)
	assert.NotNil(t, err, "Should fail if sdafs fails")

	d.sdafsPath = &truePath

	count := 0

	d.isMountPoint = func(d *Driver, v *volumeInfo) bool {
		count++
		t.Logf("mountpoint is called with count %v", count)
		return count > 3
	}

	err = doMount(&d, &v)
	assert.Nil(t, err, "doMount should work when path is fine")

	// Trigger timeout instead
	count = 0
	d.waitPeriod = 15 * time.Millisecond
	d.isMountPoint = func(d *Driver, v *volumeInfo) bool {
		count++
		t.Logf("mountpoint is called with count %v", count)
		return count > 10

	}
	err = doMount(&d, &v)
	assert.NotNil(t, err, "doMount should signal failure time out")

}
