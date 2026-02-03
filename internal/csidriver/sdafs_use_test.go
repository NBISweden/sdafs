package csidriver

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/user"
	"path"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tj/assert"
)

func nonExistentPath(t *testing.T) string {
	return path.Join(t.TempDir(), "does-not-exist")
}

func TestGetCAFilePath(t *testing.T) {

	path := "PATHSTART"
	d := Driver{
		tokenDir: &path,
	}

	v := volumeInfo{ID: "IDENTIFIER"}
	tp := d.getCAFilePath(&v)

	assert.Equal(t, "PATHSTART/extraca-IDENTIFIER", tp, "Unexpected path")
}

func TestWriteExtraCA(t *testing.T) {
	path := nonExistentPath(t)
	d := Driver{
		tokenDir: &path,
	}

	v := volumeInfo{ID: "IDENTIFIER"}
	err := d.writeExtraCA(&v)
	assert.NotNil(t, err, "No extra CA passed, writeExtraCA should fail")

	// Let's try it with data but a bad path
	v.Context = make(map[string]string)
	v.Context["extraca"] = "somedata"
	err = d.writeExtraCA(&v)
	assert.NotNil(t, err, "Bad write path, writeExtraCA should fail")

	newpath := "."
	d.tokenDir = &newpath
	err = d.writeExtraCA(&v)
	assert.Nil(t, err, "writeExtraCA should work here")
	defer os.Remove(d.getCAFilePath(&v)) // nolint:errcheck

	f, err := os.Open(d.getCAFilePath(&v))
	assert.Nil(t, err, "Unexpected open failure")

	readback, err := io.ReadAll(f)
	assert.Nil(t, err, "Unexpected read failure")

	assert.Equal(t, []byte(v.Context["extraca"]), readback,
		"Data from extra CA file doesn't match expected contents")

	err = f.Close()
	assert.Nil(t, err, "Unexpected close failure")
}

func TestGetTokenFilePath(t *testing.T) {

	path := "PATHSTART"
	d := Driver{
		tokenDir: &path,
	}

	v := volumeInfo{ID: "IDENTIFIER"}
	tp := d.getTokenFilePath(&v)

	assert.Equal(t, "PATHSTART/token-IDENTIFIER", tp, "Unexpected path")
}

func TestWriteToken(t *testing.T) {

	tokenPath := "."
	nowrite := path.Join(nonExistentPath(t), "some", "levels", "down")
	d := Driver{
		tokenDir: &tokenPath,
	}

	v := volumeInfo{
		ID:     uuid.New().String(),
		Secret: uuid.New().String(),
	}

	err := writeToken(&d, &v)
	assert.Nil(t, err, "writeToken failed")

	filename := d.getTokenFilePath(&v)
	defer os.Remove(filename) // nolint:errcheck
	// Clean up afterwards

	f, err := os.Open(filename)
	assert.Nil(t, err, "Error while opening token file")

	data, err := io.ReadAll(f)
	assert.Nil(t, err, "Error while reading token file")
	defer f.Close() // nolint:errcheck

	assert.Equal(t,
		fmt.Sprintf("access_token = %s\n\n", v.Secret),
		string(data),
		"Token in file not as expected")

	d.tokenDir = &nowrite
	err = writeToken(&d, &v)
	assert.NotNil(t, err, "writeToken should have failed but did not")

}

func TestEnsureTargetDir(t *testing.T) {

	d := Driver{}
	v := volumeInfo{}
	nowrite := "/proc/cantwrite/here"
	exists := "/bin"
	good := "testdirensuretarget" + uuid.New().String()

	v.Path = nowrite
	err := d.ensureTargetDir(&v)
	assert.NotNil(t, err, "Unexpected lack of error from EnsureTargetDir fail")

	v.Path = exists
	err = d.ensureTargetDir(&v)
	assert.Nil(t, err, "Unexpected error from EnsureTargetDir exist case")

	defer os.Remove(good) // nolint:errcheck

	v.Path = good
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
	v := volumeInfo{Path: nonExistentPath(t)}
	err := unmount(&d, &v)
	assert.Nil(t, err, "Nonexistant path is okay for unmount")

	// So is unmounted existant
	v.Path = "testdirunmount" + uuid.New().String()
	err = d.ensureTargetDir(&v)
	assert.Nil(t, err, "ensureTargetDir failed for testing unmount")

	defer os.Remove(v.Path) // nolint:errcheck

	err = unmount(&d, &v)
	assert.Nil(t, err, "Not mounted path is okay for unmount")
	_, err = os.Stat(v.Path)
	assert.NotNil(t, err, "unmount should have deleted the mount directory")

	cu, err := user.Current()
	assert.Nil(t, err, "User lookup failed unexpectedly")

	if cu.Uid == "0" {
		t.Log("Running as root, won't do mount tests")
		// Skip tests that break when running as root
		return
	}

	v.Path = "/bin"
	err = unmount(&d, &v)
	assert.NotNil(t, err, "Cleanup should have failed and been signalled")

	_, err = os.Stat("/proc")
	if err != nil {
		t.Log("Not normal linux; skipping unmount failure check")
		return
	}

	v.Path = "/proc"
	err = unmount(&d, &v)
	assert.NotNil(t, err, "We should have gotten a failure")
}

func TestIsMountPoint(t *testing.T) {

	d := Driver{}
	v := volumeInfo{Path: nonExistentPath(t)}
	ismp := isMountPoint(&d, &v)
	assert.False(t, ismp, "Nonexistant path is not mount point")

	_, err := os.Stat("/proc")
	if err != nil {
		t.Log("Not normal linux; skipping mount point check")
		return
	}

	v.Path = "/proc"
	ismp = isMountPoint(&d, &v)
	assert.True(t, ismp, "Actual mount point should yield true")
}

func TestDoMount(t *testing.T) {

	tokenDir := "/some/dir"
	truePath := "/bin/true"
	falsePath := "/bin/false"
	argloggerPath := "./sdafsarglogger"
	thisDir := "./"

	nonexistant := nonExistentPath(t)

	d := Driver{
		tokenDir:     &tokenDir,
		sdafsPath:    &nonexistant,
		isMountPoint: isMountPoint,
		maxWaitMount: 30 * time.Millisecond,
	}
	v := volumeInfo{
		Path:    nonExistentPath(t),
		Context: make(map[string]string),
	}

	err := doMount(&d, &v)
	assert.NotNil(t, err, "Should have error fixing the directory")

	_, err = os.Stat("/proc")
	if err != nil {
		t.Log("Not normal linux; skipping mount point check")
		return
	}

	v.Path = "/proc"
	err = doMount(&d, &v)
	assert.Nil(t, err, "doMount should work when path is already mounted")

	v.Path = "testdirmount" + uuid.New().String()
	defer os.Remove(v.Path) // nolint:errcheck

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

	count = 0
	d.isMountPoint = isMountPoint
	d.waitPeriod = 1 * time.Second

	randomString := uuid.New().String()
	anotherRandomString := uuid.New().String()

	v.Group = randomString
	d.sdafsPath = &argloggerPath

	count = 0
	d.isMountPoint = func(d *Driver, v *volumeInfo) bool {
		count++
		t.Logf("mountpoint is called with count %v", count)
		return count > 2
	}

	err = doMount(&d, &v)
	assert.Nil(t, err, "provided group through Group did not work as expected")

	groupFound, err := stringInArgsFile("--group " + randomString)
	assert.Nil(t, err, "error checking for string in argument file")
	assert.True(t, groupFound,
		"Expected group parameter not found for mount capability")

	v.Context["owner"] = anotherRandomString
	v.Context["group"] = anotherRandomString

	count = 0
	err = doMount(&d, &v)
	assert.Nil(t, err, "provided group through volumecapability did not work as expected")

	groupFound, err = stringInArgsFile("--group " + randomString)
	assert.Nil(t, err, "error checking for string in argument file")
	assert.True(t, groupFound,
		"Expected group parameter not found for mount capability when conflicting")

	v.Group = ""

	count = 0
	err = doMount(&d, &v)
	assert.Nil(t, err, "provided no Group did not work as expected")

	groupFound, err = stringInArgsFile("--group " + randomString)
	assert.Nil(t, err, "error checking for string in argument file")
	assert.False(t, groupFound,
		"Bad group parameter not found without mount capability")

	groupFound, err = stringInArgsFile("--group " + anotherRandomString)
	assert.Nil(t, err, "error checking for string in argument file")
	assert.True(t, groupFound,
		"Expected group parameter not found without mount capability")

	ownerFound, err := stringInArgsFile("--owner " + anotherRandomString)
	assert.Nil(t, err, "error checking for string in argument file")
	assert.True(t, ownerFound,
		"Expected owner parameter not found without mount capability")

	// test some other options being forwarded properly

	// extraca will write the argument to be passed to tokendir, try a bad
	// one
	v.Context["extraca"] = randomString
	count = 0
	err = doMount(&d, &v)
	assert.NotNil(t, err, "extraca test did not fail as expected")

	d.tokenDir = &thisDir
	err = doMount(&d, &v)
	assert.Nil(t, err, "extraca test did not work as expected")

	extracaFound, err := stringInArgsFile("--extracafile ")
	assert.Nil(t, err, "error checking for string in argument file")
	assert.True(t, extracaFound,
		"Expected extraca parameter not found")

}

func stringInArgsFile(lookFor string) (bool, error) {
	f, err := os.Open("./sdafsargs")
	if err != nil {
		return false, fmt.Errorf("unexpected error opening argument file after capability: %v", err)
	}
	s, err := io.ReadAll(f)
	if err != nil {
		return false, fmt.Errorf("unexpected error reading argument file after capability: %v", err)
	}

	err = f.Close()
	if err != nil {
		return false, fmt.Errorf("unexpected error closing argument file after capability: %v", err)
	}

	return bytes.Contains(s, []byte(lookFor)), nil
}
