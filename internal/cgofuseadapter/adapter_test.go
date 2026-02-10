//go:build cgo

package cgofuseadapter

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/NBISweden/sdafs/internal/sdafs"
	"github.com/tj/assert"
)

type mockSDA struct {
	lookupReturn             error
	lookupAction             *func(o *sdafs.LookUpInodeOp)
	readDirReturn            error
	readDirAction            *func(o *sdafs.ReadDirOp)
	openDirReturn            error
	openDirAction            *func(o *sdafs.OpenDirOp)
	readFileReturn           error
	readFileAction           *func(o *sdafs.ReadFileOp)
	releaseFileHandleReturn  error
	releaseFileHandleAction  *func(o *sdafs.ReleaseFileHandleOp)
	openFileReturn           error
	openFileAction           *func(o *sdafs.OpenFileOp)
	getInodeAttributesReturn error
	getInodeAttributesAction *func(o *sdafs.GetInodeAttributesOp)
}

func (m *mockSDA) ReadDir(_ context.Context, op *sdafs.ReadDirOp) error {
	if m.readDirAction != nil {
		(*m.readDirAction)(op)
	}
	return m.readDirReturn
}

func (m *mockSDA) OpenDir(_ context.Context, op *sdafs.OpenDirOp) error {
	if m.openDirAction != nil {
		(*m.openDirAction)(op)
	}
	return m.openDirReturn
}

func (m *mockSDA) OpenFile(_ context.Context, op *sdafs.OpenFileOp) error {
	if m.openFileAction != nil {
		(*m.openFileAction)(op)
	}
	return m.openFileReturn
}
func (m *mockSDA) ReadFile(_ context.Context, op *sdafs.ReadFileOp) error {
	if m.readFileAction != nil {
		(*m.readFileAction)(op)
	}
	return m.readFileReturn
}
func (m *mockSDA) ReleaseFileHandle(_ context.Context, op *sdafs.ReleaseFileHandleOp) error {
	if m.releaseFileHandleAction != nil {
		(*m.releaseFileHandleAction)(op)
	}
	return m.releaseFileHandleReturn
}
func (m *mockSDA) GetInodeAttributes(_ context.Context, op *sdafs.GetInodeAttributesOp) error {
	if m.getInodeAttributesAction != nil {
		(*m.getInodeAttributesAction)(op)
	}
	return m.getInodeAttributesReturn
}
func (m *mockSDA) LookUpInode(_ context.Context, op *sdafs.LookUpInodeOp) error {
	if m.lookupAction != nil {
		(*m.lookupAction)(op)
	}
	return m.lookupReturn
}

func TestGetattr(t *testing.T) {
	mock := mockSDA{}
	a := &Adapter{fs: &mock}

	st := Stat_t{}
	ret := a.Getattr("/", &st, 100)
	assert.Zero(t, ret, "Getattr failed unexpectedly")

	mock.lookupReturn = fmt.Errorf("let's fail this")
	ret = a.Getattr("/notfound", &st, 100)
	assert.NotZero(t, ret, "Getattr worked when it should not")

	mock.lookupReturn = nil
	mock.getInodeAttributesReturn = fmt.Errorf("fail this")
	ret = a.Getattr("/notfound", &st, 100)
	assert.NotZero(t, ret, "Getattr worked when it should not")

	mock.getInodeAttributesReturn = nil

	lookupGood := func(o *sdafs.LookUpInodeOp) {
		o.Entry.Child = 400
	}

	getAttrOK := func(o *sdafs.GetInodeAttributesOp) {
		o.Attributes.Size = 1234
		o.Attributes.Uid = 9876
	}
	mock.getInodeAttributesAction = &getAttrOK
	mock.lookupAction = &lookupGood

	ret = a.Getattr("/good", &st, 100)
	assert.Zero(t, ret, "Getattr failed when it should not")

	assert.Equal(t, uint64(400), st.Ino, "Unexpected translation of attribute")
	assert.Equal(t, int64(1234), st.Size, "Unexpected translation of attribute")
	assert.Equal(t, uint32(9876), st.Uid, "Unexpected translation of attribute")
}

func TestReaddir(t *testing.T) {
	mock := mockSDA{}
	a := &Adapter{fs: &mock}

	seenStats := make([]Stat_t, 0)
	seenNames := make([]string, 0)
	fill := func(name string, st *Stat_t, offset int64) bool {
		seenStats = append(seenStats, *st)
		seenNames = append(seenNames, name)
		return true
	}

	ret := a.Readdir("/", fill, 0, 0)
	assert.Zero(t, ret, "Readdir failed unexpectedly")

	mock.lookupReturn = fmt.Errorf("")
	ret = a.Readdir("/notfound", fill, 0, 0)
	assert.NotZero(t, ret, "Readdir worked when it should not")

	brokenDirent := func(o *sdafs.ReadDirOp) {
		o.BytesRead = 10
	}
	mock.lookupReturn = nil
	mock.readDirAction = &brokenDirent
	ret = a.Readdir("/found", fill, 0, 0)
	assert.NotZero(t, ret, "Readdir worked when it should not")

	oneDirent := func(o *sdafs.ReadDirOp) {
		o.BytesRead = 48
		writeOut := uint64(10)
		// inode
		written, err := binary.Encode(o.Dst[0:8], binary.NativeEndian, &writeOut)
		assert.Nil(t, err, "Encode failed")
		assert.Equal(t, 8, written, "Unexpected number of bytes written")
		// offset
		writeOut = uint64(0)
		written, err = binary.Encode(o.Dst[8:16], binary.NativeEndian, &writeOut)
		assert.Nil(t, err, "Encode failed")
		assert.Equal(t, 8, written, "Unexpected number of bytes written")
		length := uint32(20)
		written, err = binary.Encode(o.Dst[16:20], binary.NativeEndian, &length)
		assert.Nil(t, err, "Encode failed")
		assert.Equal(t, 4, written, "Unexpected number of bytes written")
		copy(o.Dst[24:], "this_name_is_20_long")
	}
	mock.readDirAction = &oneDirent
	ret = a.Readdir("/found", fill, 0, 0)
	assert.Zero(t, ret, "Readdir failed when it should not")
	assert.Equal(t, []string{"this_name_is_20_long"}, seenNames,
		"Unexpected outcome")
	assert.Equal(t, seenStats[0].Ino, uint64(10),
		"Unexpected outcome")
}

func TestRelease(t *testing.T) {
	mock := mockSDA{}
	a := &Adapter{fs: &mock}

	ret := a.Release("/", 100)
	assert.Zero(t, ret, "Release failed unexpectedly")

	mock.lookupReturn = fmt.Errorf("let's fail this")
	ret = a.Release("/notfound", 100)
	assert.NotZero(t, ret, "Release worked when it should not")

	mock.lookupReturn = nil
	mock.releaseFileHandleReturn = fmt.Errorf("fail now")
	ret = a.Release("/found", 100)
	assert.NotZero(t, ret, "Release worked when it should not")

	mock.releaseFileHandleReturn = nil
	ret = a.Release("/found", 100)
	assert.Zero(t, ret, "Release failed when it should not")
}

func TestReleaseDir(t *testing.T) {
	mock := mockSDA{}
	a := &Adapter{fs: &mock}

	ret := a.ReleaseDir("/", 100)
	assert.Zero(t, ret, "ReleaseDir failed unexpectedly")

	mock.lookupReturn = fmt.Errorf("let's fail this")
	ret = a.ReleaseDir("/notfound", 100)
	assert.NotZero(t, ret, "ReleaseDir worked when it should not")
}

func TestAccess(t *testing.T) {
	mock := mockSDA{}
	a := &Adapter{fs: &mock}

	ret := a.Access("/", 0)
	assert.Zero(t, ret, "Access failed unexpectedly")

	mock.lookupReturn = fmt.Errorf("let's fail this")
	ret = a.Access("/notfound", 0)
	assert.NotZero(t, ret, "Access worked when it should not")

	ret = a.Access("/writeshouldnotwork", 2)
	assert.NotZero(t, ret, "Access worked when it should not")
}

func TestStatFS(t *testing.T) {
	mock := mockSDA{}
	a := &Adapter{fs: &mock}

	st := Statfs_t{}
	ret := a.Statfs("/", &st)
	assert.Zero(t, ret, "Access failed unexpectedly")
	assert.Equal(t, uint64(65536), st.Bsize)
}

func TestOpenDir(t *testing.T) {
	mock := mockSDA{}
	a := &Adapter{fs: &mock}

	ret, _ := a.Opendir("/")
	assert.Zero(t, ret, "OpenDir failed unexpectedly")

	mock.lookupReturn = fmt.Errorf("let's fail this")
	ret, _ = a.Opendir("/notfound")
	assert.NotZero(t, ret, "OpenDir worked when it should not")

	openOK := func(o *sdafs.OpenDirOp) {
		o.Handle = 199
	}
	mock.openDirAction = &openOK
	mock.lookupReturn = nil
	ret, handle := a.Opendir("/found")
	assert.Zero(t, ret, "OpenDir failed unexpectedly")
	assert.Equal(t, uint64(199), handle, "Unexpected handle")

	mock.openDirReturn = fmt.Errorf("let's fail")
	ret, _ = a.Opendir("/broken")
	assert.NotZero(t, ret, "OpenDir worked unexpectedly")
}

func TestOpen(t *testing.T) {
	mock := mockSDA{}
	a := &Adapter{fs: &mock}

	ret, _ := a.Open("/somepath", 2)
	assert.NotZero(t, ret, "Open worked unexpectedly")

	ret, _ = a.Open("/somepath", 0)
	assert.Zero(t, ret, "Open failed unexpectedly")

	mock.lookupReturn = fmt.Errorf("let's fail this")
	ret, _ = a.Open("/notfound", 0)
	assert.NotZero(t, ret, "Open worked when it should not")

	openAction := func(o *sdafs.OpenFileOp) {
		o.Handle = 199
	}
	mock.openFileAction = &openAction
	mock.lookupReturn = nil
	ret, handle := a.Open("/found", 0)
	assert.Zero(t, ret, "Open failed unexpectedly")
	assert.Equal(t, uint64(199), handle, "Unexpected handle")

	mock.openFileReturn = fmt.Errorf("let's fail")
	ret, _ = a.Open("/broken", 0)
	assert.NotZero(t, ret, "Open worked unexpectedly")
}

func TestRead(t *testing.T) {
	mock := mockSDA{}
	a := &Adapter{fs: &mock}
	buf := make([]byte, 65536)

	mock.lookupReturn = fmt.Errorf("fail")
	ret := a.Read("/badpath", buf, 100, 10)
	assert.Less(t, ret, 0, "Read worked unexpectedly")

	mock.lookupReturn = nil
	mock.readFileReturn = fmt.Errorf("fail now")
	ret = a.Read("/someerror", buf, 100, 10)
	assert.Less(t, ret, 0, "Read worked unexpectedly")

	readOK := func(o *sdafs.ReadFileOp) {
		o.BytesRead = 199
	}

	mock.readFileReturn = nil
	mock.readFileAction = &readOK
	ret = a.Read("/works", buf, 100, 10)
	assert.Equal(t, ret, 199, "Read failed unexpectedly")
}

func TestFilenameToInode(t *testing.T) {
	mock := mockSDA{}
	a := &Adapter{fs: &mock}

	lookupTester := func(op *sdafs.LookUpInodeOp) {

		mock.lookupReturn = nil
		switch op.Parent {
		case 1:
			if op.Name != "test" {
				mock.lookupReturn = fmt.Errorf("fail")
			}
			op.Entry.Child = 20
		case 20:
			if op.Name != "path" {
				mock.lookupReturn = fmt.Errorf("fail")
			}
			op.Entry.Child = 25

		case 25:
			if op.Name != "steps" {
				mock.lookupReturn = fmt.Errorf("fail")
			}
			op.Entry.Child = 40
		}
	}

	mock.lookupAction = &lookupTester
	inode, err := a.filenameToInode("/")
	assert.Equal(t, sdafs.InodeID(sdafs.RootInodeID), inode, "Unexpected inode for root")
	assert.Nil(t, err, "Unexpected error for filenameToInode")

	inode, err = a.filenameToInode("/test/path/steps")
	assert.Equal(t, sdafs.InodeID(40), inode, "Unexpected inode for root")
	assert.Nil(t, err, "Unexpected error for filenameToInode")

	inode, err = a.filenameToInode("/test/path/notfound")
	assert.NotNil(t, err, "Unexpected lack of error for filenameToInode")

	inode, err = a.filenameToInode("/notfound/path/steps")
	assert.NotNil(t, err, "Unexpected lack of error for filenameToInode")
}
