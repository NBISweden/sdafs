package fuseadapter

import (
	"os"
	"testing"
	"time"
)

func TestInodeIDConstants(t *testing.T) {
	if RootInodeID != 1 {
		t.Errorf("RootInodeID should be 1, got %d", RootInodeID)
	}
}

func TestInodeAttributes(t *testing.T) {
	now := time.Now()
	attrs := InodeAttributes{
		Size:  1024,
		Nlink: 1,
		Mode:  0644,
		Uid:   1000,
		Gid:   1000,
		Atime: now,
		Mtime: now,
		Ctime: now,
	}

	if attrs.Size != 1024 {
		t.Errorf("Expected size 1024, got %d", attrs.Size)
	}
	if attrs.Mode != 0644 {
		t.Errorf("Expected mode 0644, got %o", attrs.Mode)
	}
}

func TestDirent(t *testing.T) {
	dirent := Dirent{
		Offset: 1,
		Inode:  2,
		Name:   "test.txt",
		Type:   DT_File,
	}

	if dirent.Name != "test.txt" {
		t.Errorf("Expected name 'test.txt', got '%s'", dirent.Name)
	}
	if dirent.Type != DT_File {
		t.Errorf("Expected type DT_File, got %d", dirent.Type)
	}
}

func TestMountConfig(t *testing.T) {
	config := &MountConfig{
		ReadOnly:                  true,
		DisableDefaultPermissions: true,
		FSName:                    "testfs",
		VolumeName:                "Test Volume",
		Options:                   map[string]string{"allow_other": ""},
	}

	if !config.ReadOnly {
		t.Error("Expected ReadOnly to be true")
	}
	if config.FSName != "testfs" {
		t.Errorf("Expected FSName 'testfs', got '%s'", config.FSName)
	}
}

func TestWriteDirent(t *testing.T) {
	dirent := Dirent{
		Offset: 1,
		Inode:  2,
		Name:   "test.txt",
		Type:   DT_File,
	}

	// Test with buffer that's too small
	smallBuf := make([]byte, 10)
	n := WriteDirent(smallBuf, dirent)
	if n != 0 {
		t.Errorf("Expected WriteDirent to return 0 for too-small buffer, got %d", n)
	}

	// Test with sufficient buffer
	buf := make([]byte, 100)
	n = WriteDirent(buf, dirent)
	if n == 0 {
		t.Error("Expected WriteDirent to return non-zero for sufficient buffer")
	}
}

func TestNewHandleID(t *testing.T) {
	id := NewHandleID(42)
	if uint64(id) != 42 {
		t.Errorf("Expected HandleID 42, got %d", id)
	}
}

func TestNewInodeID(t *testing.T) {
	id := NewInodeID(42)
	if uint64(id) != 42 {
		t.Errorf("Expected InodeID 42, got %d", id)
	}
}

func TestDirentTypes(t *testing.T) {
	tests := []struct {
		name string
		dt   DirentType
	}{
		{"unknown", DT_Unknown},
		{"file", DT_File},
		{"dir", DT_Dir},
		{"link", DT_Link},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.dt < 0 || tt.dt > 3 {
				t.Errorf("DirentType %s has invalid value %d", tt.name, tt.dt)
			}
		})
	}
}

func TestFileSystemBase(t *testing.T) {
	fs := &FileSystemBase{}

	// Test that all operations return ErrNotImplemented
	err := fs.GetInodeAttributes(nil, nil)
	if err != ErrNotImplemented {
		t.Errorf("Expected ErrNotImplemented, got %v", err)
	}

	err = fs.LookUpInode(nil, nil)
	if err != ErrNotImplemented {
		t.Errorf("Expected ErrNotImplemented, got %v", err)
	}

	err = fs.StatFS(nil, nil)
	if err != ErrNotImplemented {
		t.Errorf("Expected ErrNotImplemented, got %v", err)
	}

	err = fs.OpenFile(nil, nil)
	if err != ErrNotImplemented {
		t.Errorf("Expected ErrNotImplemented, got %v", err)
	}

	err = fs.ReadFile(nil, nil)
	if err != ErrNotImplemented {
		t.Errorf("Expected ErrNotImplemented, got %v", err)
	}

	err = fs.OpenDir(nil, nil)
	if err != ErrNotImplemented {
		t.Errorf("Expected ErrNotImplemented, got %v", err)
	}

	err = fs.ReadDir(nil, nil)
	if err != ErrNotImplemented {
		t.Errorf("Expected ErrNotImplemented, got %v", err)
	}
}

func TestOpContext(t *testing.T) {
	ctx := OpContext{
		Pid: 1234,
		Uid: 1000,
		Gid: 1000,
	}

	if ctx.Pid != 1234 {
		t.Errorf("Expected PID 1234, got %d", ctx.Pid)
	}
	if ctx.Uid != 1000 {
		t.Errorf("Expected UID 1000, got %d", ctx.Uid)
	}
}

func TestInodeAttributesWithMode(t *testing.T) {
	// Test directory attributes
	dirAttrs := InodeAttributes{
		Mode: os.ModeDir | 0755,
	}
	if !dirAttrs.Mode.IsDir() {
		t.Error("Expected Mode to indicate directory")
	}

	// Test regular file attributes
	fileAttrs := InodeAttributes{
		Mode: 0644,
	}
	if fileAttrs.Mode.IsDir() {
		t.Error("Expected Mode to not indicate directory")
	}
}
