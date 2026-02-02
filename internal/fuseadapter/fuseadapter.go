// Package fuseadapter provides an abstraction layer for FUSE implementations,
// allowing the use of either jacobsa/fuse or cgofuse.
package fuseadapter

import (
	"context"
	"io"
	"os"
	"syscall"
	"time"
)

// InodeID represents a unique identifier for an inode in the filesystem
type InodeID uint64

// HandleID represents a unique identifier for an open file handle
type HandleID uint64

// DirOffset represents an offset for directory reading operations
type DirOffset uint64

// RootInodeID is the constant representing the root directory inode
const RootInodeID InodeID = 1

// InodeAttributes contains metadata for an inode
type InodeAttributes struct {
	Size  uint64
	Nlink uint32
	Mode  os.FileMode
	Uid   uint32
	Gid   uint32
	Atime time.Time
	Mtime time.Time
	Ctime time.Time
}

// OpContext provides context about the operation being performed
type OpContext struct {
	Pid uint32
	Uid uint32
	Gid uint32
}

// Dirent represents a directory entry
type Dirent struct {
	Offset DirOffset
	Inode  InodeID
	Name   string
	Type   DirentType
}

// DirentType represents the type of a directory entry
type DirentType uint32

const (
	DT_Unknown DirentType = 0
	DT_File    DirentType = 1
	DT_Dir     DirentType = 2
	DT_Link    DirentType = 3
)

// GetInodeAttributesOp represents a request to get inode attributes
type GetInodeAttributesOp struct {
	Inode      InodeID
	Attributes InodeAttributes
}

// LookUpInodeOp represents a request to look up an inode by name
type LookUpInodeOp struct {
	Parent InodeID
	Name   string
	Entry  struct {
		Child      InodeID
		Attributes InodeAttributes
	}
}

// OpenFileOp represents a request to open a file
type OpenFileOp struct {
	OpContext
	Inode  InodeID
	Handle HandleID
}

// ReadFileOp represents a request to read from a file
type ReadFileOp struct {
	Inode     InodeID
	Handle    HandleID
	Offset    int64
	Dst       []byte
	BytesRead int
}

// OpenDirOp represents a request to open a directory
type OpenDirOp struct {
	OpContext
	Inode InodeID
}

// ReadDirOp represents a request to read directory contents
type ReadDirOp struct {
	Inode     InodeID
	Offset    DirOffset
	Dst       []byte
	BytesRead int
}

// StatFSOp represents a request for filesystem statistics
type StatFSOp struct {
	BlockSize      uint32
	Blocks         uint64
	BlocksFree     uint64
	BlocksAvail    uint64
	IoSize         uint32
	Inodes         uint64
	InodesFree     uint64
	MaxFilenameLen uint32
}

// FileSystem is the interface that must be implemented by FUSE filesystems
type FileSystem interface {
	// GetInodeAttributes gets the attributes for the given inode
	GetInodeAttributes(ctx context.Context, op *GetInodeAttributesOp) error

	// LookUpInode looks up a child inode by name within a parent directory
	LookUpInode(ctx context.Context, op *LookUpInodeOp) error

	// StatFS returns filesystem statistics
	StatFS(ctx context.Context, op *StatFSOp) error

	// OpenFile opens a file for reading
	OpenFile(ctx context.Context, op *OpenFileOp) error

	// ReadFile reads data from an open file
	ReadFile(ctx context.Context, op *ReadFileOp) error

	// OpenDir opens a directory for reading
	OpenDir(ctx context.Context, op *OpenDirOp) error

	// ReadDir reads directory entries
	ReadDir(ctx context.Context, op *ReadDirOp) error
}

// MountConfig contains configuration for mounting a filesystem
type MountConfig struct {
	ReadOnly                  bool
	DisableDefaultPermissions bool
	FSName                    string
	VolumeName                string
	Options                   map[string]string
}

// MountedFileSystem represents an active FUSE mount
type MountedFileSystem interface {
	// Join waits for the filesystem to be unmounted
	Join(ctx context.Context) error

	// Unmount unmounts the filesystem
	Unmount() error
}

// FUSEAdapter is the interface for different FUSE implementations
type FUSEAdapter interface {
	// Mount mounts a filesystem at the given mountpoint
	Mount(mountpoint string, fs FileSystem, config *MountConfig) (MountedFileSystem, error)

	// Unmount unmounts the filesystem at the given mountpoint
	Unmount(mountpoint string) error
}

// FileSystemBase provides a base implementation with default error responses
type FileSystemBase struct{}

// Common error values
var (
	ErrNotImplemented = syscall.ENOSYS
	ErrNoEntry        = syscall.ENOENT
	ErrIO             = syscall.EIO
	ErrInvalid        = syscall.EINVAL
	ErrAccess         = syscall.EACCES
	ErrExist          = syscall.EEXIST
)

// GetInodeAttributes returns ENOSYS by default
func (fs *FileSystemBase) GetInodeAttributes(ctx context.Context, op *GetInodeAttributesOp) error {
	return ErrNotImplemented
}

// LookUpInode returns ENOSYS by default
func (fs *FileSystemBase) LookUpInode(ctx context.Context, op *LookUpInodeOp) error {
	return ErrNotImplemented
}

// StatFS returns ENOSYS by default
func (fs *FileSystemBase) StatFS(ctx context.Context, op *StatFSOp) error {
	return ErrNotImplemented
}

// OpenFile returns ENOSYS by default
func (fs *FileSystemBase) OpenFile(ctx context.Context, op *OpenFileOp) error {
	return ErrNotImplemented
}

// ReadFile returns ENOSYS by default
func (fs *FileSystemBase) ReadFile(ctx context.Context, op *ReadFileOp) error {
	return ErrNotImplemented
}

// OpenDir returns ENOSYS by default
func (fs *FileSystemBase) OpenDir(ctx context.Context, op *OpenDirOp) error {
	return ErrNotImplemented
}

// ReadDir returns ENOSYS by default
func (fs *FileSystemBase) ReadDir(ctx context.Context, op *ReadDirOp) error {
	return ErrNotImplemented
}

// WriteDirent writes a directory entry to the destination buffer using fuseutil format
// Returns the number of bytes written, or 0 if the buffer is too small
// This implementation needs to match the format expected by the FUSE kernel module
func WriteDirent(dst []byte, dirent Dirent) int {
	// This is intentionally a passthrough for now
	// The actual implementation will depend on the adapter being used
	// For jacobsa/fuse, we'll handle this in the adapter layer
	
	// Calculate approximate size (this will be overridden by the adapter)
	nameLen := len(dirent.Name)
	// Dirent structure: inode (8) + offset (8) + name_len (4) + type (4) + name + padding
	entrySize := 24 + nameLen
	// Align to 8-byte boundary
	entrySize = (entrySize + 7) &^ 7

	if len(dst) < entrySize {
		return 0
	}

	return entrySize
}

// NewHandleID generates a new unique handle ID from a uint64
func NewHandleID(id uint64) HandleID {
	return HandleID(id)
}

// NewInodeID creates a new InodeID from a uint64
func NewInodeID(id uint64) InodeID {
	return InodeID(id)
}

// FileHandle represents an open file with its reader
type FileHandle struct {
	ID     HandleID
	Reader io.ReadSeekCloser
}
