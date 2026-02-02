package jacobsa

import (
	"context"
	"testing"

	"github.com/NBISweden/sdafs/internal/fuseadapter"
	"github.com/jacobsa/fuse/fuseops"
)

func TestNewAdapter(t *testing.T) {
	adapter := NewAdapter()
	if adapter == nil {
		t.Fatal("NewAdapter() returned nil")
	}
}

func TestConvertDirentToJacobsa(t *testing.T) {
	adapterDirent := fuseadapter.Dirent{
		Offset: 1,
		Inode:  2,
		Name:   "test.txt",
		Type:   fuseadapter.DT_File,
	}

	jacobsaDirent := ConvertDirentToJacobsa(adapterDirent)

	if uint64(jacobsaDirent.Offset) != uint64(adapterDirent.Offset) {
		t.Errorf("Offset mismatch: expected %d, got %d", adapterDirent.Offset, jacobsaDirent.Offset)
	}
	if uint64(jacobsaDirent.Inode) != uint64(adapterDirent.Inode) {
		t.Errorf("Inode mismatch: expected %d, got %d", adapterDirent.Inode, jacobsaDirent.Inode)
	}
	if jacobsaDirent.Name != adapterDirent.Name {
		t.Errorf("Name mismatch: expected %s, got %s", adapterDirent.Name, jacobsaDirent.Name)
	}
	if uint32(jacobsaDirent.Type) != uint32(adapterDirent.Type) {
		t.Errorf("Type mismatch: expected %d, got %d", adapterDirent.Type, jacobsaDirent.Type)
	}
}

// mockFileSystem implements fuseadapter.FileSystem for testing
type mockFileSystem struct {
	fuseadapter.FileSystemBase
	getInodeAttrCalled bool
	lookupCalled       bool
	statfsCalled       bool
	openFileCalled     bool
	readFileCalled     bool
	openDirCalled      bool
	readDirCalled      bool
}

func (m *mockFileSystem) GetInodeAttributes(ctx context.Context, op *fuseadapter.GetInodeAttributesOp) error {
	m.getInodeAttrCalled = true
	op.Attributes = fuseadapter.InodeAttributes{
		Size:  100,
		Nlink: 1,
		Mode:  0644,
		Uid:   1000,
		Gid:   1000,
	}
	return nil
}

func (m *mockFileSystem) LookUpInode(ctx context.Context, op *fuseadapter.LookUpInodeOp) error {
	m.lookupCalled = true
	op.Entry.Child = 2
	op.Entry.Attributes = fuseadapter.InodeAttributes{
		Size:  200,
		Nlink: 1,
		Mode:  0644,
		Uid:   1000,
		Gid:   1000,
	}
	return nil
}

func (m *mockFileSystem) StatFS(ctx context.Context, op *fuseadapter.StatFSOp) error {
	m.statfsCalled = true
	op.BlockSize = 4096
	return nil
}

func (m *mockFileSystem) OpenFile(ctx context.Context, op *fuseadapter.OpenFileOp) error {
	m.openFileCalled = true
	op.Handle = fuseadapter.HandleID(42)
	return nil
}

func (m *mockFileSystem) ReadFile(ctx context.Context, op *fuseadapter.ReadFileOp) error {
	m.readFileCalled = true
	op.BytesRead = len(op.Dst)
	return nil
}

func (m *mockFileSystem) OpenDir(ctx context.Context, op *fuseadapter.OpenDirOp) error {
	m.openDirCalled = true
	return nil
}

func (m *mockFileSystem) ReadDir(ctx context.Context, op *fuseadapter.ReadDirOp) error {
	m.readDirCalled = true
	op.BytesRead = 0
	return nil
}

func TestFileSystemBridgeGetInodeAttributes(t *testing.T) {
	mockFS := &mockFileSystem{}
	bridge := &fileSystemBridge{fs: mockFS}

	op := &fuseops.GetInodeAttributesOp{
		Inode: 1,
	}

	err := bridge.GetInodeAttributes(context.Background(), op)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !mockFS.getInodeAttrCalled {
		t.Error("GetInodeAttributes was not called on mock filesystem")
	}
	if op.Attributes.Size != 100 {
		t.Errorf("Expected size 100, got %d", op.Attributes.Size)
	}
}

func TestFileSystemBridgeLookUpInode(t *testing.T) {
	mockFS := &mockFileSystem{}
	bridge := &fileSystemBridge{fs: mockFS}

	op := &fuseops.LookUpInodeOp{
		Parent: 1,
		Name:   "test.txt",
	}

	err := bridge.LookUpInode(context.Background(), op)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !mockFS.lookupCalled {
		t.Error("LookUpInode was not called on mock filesystem")
	}
	if op.Entry.Child != 2 {
		t.Errorf("Expected child inode 2, got %d", op.Entry.Child)
	}
}

func TestFileSystemBridgeStatFS(t *testing.T) {
	mockFS := &mockFileSystem{}
	bridge := &fileSystemBridge{fs: mockFS}

	op := &fuseops.StatFSOp{}

	err := bridge.StatFS(context.Background(), op)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !mockFS.statfsCalled {
		t.Error("StatFS was not called on mock filesystem")
	}
	if op.BlockSize != 4096 {
		t.Errorf("Expected block size 4096, got %d", op.BlockSize)
	}
}

func TestFileSystemBridgeOpenFile(t *testing.T) {
	mockFS := &mockFileSystem{}
	bridge := &fileSystemBridge{fs: mockFS}

	op := &fuseops.OpenFileOp{
		Inode: 1,
	}

	err := bridge.OpenFile(context.Background(), op)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !mockFS.openFileCalled {
		t.Error("OpenFile was not called on mock filesystem")
	}
	if op.Handle != 42 {
		t.Errorf("Expected handle 42, got %d", op.Handle)
	}
}

func TestFileSystemBridgeReadFile(t *testing.T) {
	mockFS := &mockFileSystem{}
	bridge := &fileSystemBridge{fs: mockFS}

	dst := make([]byte, 100)
	op := &fuseops.ReadFileOp{
		Inode:  1,
		Handle: 42,
		Offset: 0,
		Dst:    dst,
	}

	err := bridge.ReadFile(context.Background(), op)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !mockFS.readFileCalled {
		t.Error("ReadFile was not called on mock filesystem")
	}
	if op.BytesRead != 100 {
		t.Errorf("Expected 100 bytes read, got %d", op.BytesRead)
	}
}

func TestFileSystemBridgeOpenDir(t *testing.T) {
	mockFS := &mockFileSystem{}
	bridge := &fileSystemBridge{fs: mockFS}

	op := &fuseops.OpenDirOp{
		Inode: 1,
	}

	err := bridge.OpenDir(context.Background(), op)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !mockFS.openDirCalled {
		t.Error("OpenDir was not called on mock filesystem")
	}
}

func TestFileSystemBridgeReadDir(t *testing.T) {
	mockFS := &mockFileSystem{}
	bridge := &fileSystemBridge{fs: mockFS}

	dst := make([]byte, 1000)
	op := &fuseops.ReadDirOp{
		Inode:  1,
		Offset: 0,
		Dst:    dst,
	}

	err := bridge.ReadDir(context.Background(), op)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !mockFS.readDirCalled {
		t.Error("ReadDir was not called on mock filesystem")
	}
}
