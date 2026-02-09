package sdafs

// Compatibility layer to allow for builds for Windows by excluding jacobsa
// fuse completely -
// here we provide types to use to replace those of jacobsa

import (
	"encoding/binary"
	"log/slog"
	"os"
	"syscall"
	"time"
)

// Dirent
type Dirent struct {
	Offset DirOffset

	Inode InodeID
	Name  string
}

type InodeAttributes struct {
	Size uint64

	Nlink uint32

	Mode os.FileMode

	Rdev uint32

	Atime  time.Time // Time of last access
	Mtime  time.Time // Time of last modification
	Ctime  time.Time // Time of last modification to inode
	Crtime time.Time // Time of creation (OS X only)

	Uid uint32
	Gid uint32
}

type InodeID = uint64
type DirOffset = uint64
type HandleID = uint64

const RootInodeID uint64 = 1

const EIO = syscall.EIO
const ENOENT = syscall.ENOENT
const EEXIST = syscall.EEXIST
const EINVAL = syscall.EINVAL
const EACCES = syscall.EACCES
const EAGAIN = syscall.EAGAIN

type notImplemented struct {
}

// writeDirent adds the directory entry d to the buffer and
// returns the used length. 8-byte alignment
func writeDirent(out []byte, d Dirent) int {

	if len(out) < 32+len(d.Name) {
		slog.Info("bailing as out is to short", "out", len(out), "needed", 32+len(d.Name))
		return 0
	}

	_, err := binary.Encode(out[:8], binary.NativeEndian, &d.Inode)
	if err != nil {
		slog.Error("couldn't store direntry inode", "err", err)
		return 0
	}

	_, err = binary.Encode(out[8:16], binary.NativeEndian, &d.Offset)
	if err != nil {
		slog.Error("couldn't store direntry offset", "err", err)
		return 0
	}

	var nameLength uint32 = uint32(len(d.Name))
	_, err = binary.Encode(out[16:20], binary.NativeEndian, &nameLength)
	if err != nil {
		slog.Error("couldn't store direntry name length", "err", err)
		return 0
	}

	copy(out[24:], d.Name)

	pad := (8 - len(d.Name)%8) % 8

	return 24 + pad + len(d.Name)

}

type OpContext struct {
	Uid uint32
}

type ReadDirOp struct {
	Inode     InodeID
	Handle    HandleID
	Offset    DirOffset
	Dst       []byte
	BytesRead int
}

type OpenDirOp struct {
	Inode     InodeID
	Handle    HandleID
	OpContext OpContext
}

type OpenFileOp struct {
	Inode     InodeID
	Handle    HandleID
	OpContext OpContext
}

type ReadFileOp struct {
	Inode     InodeID
	Handle    HandleID
	Offset    int64
	Size      int64
	Dst       []byte
	BytesRead int
}

type ReleaseFileHandleOp struct {
	Inode     InodeID
	Handle    HandleID
	OpContext OpContext
}

type StatFSOp struct {
	BlockSize uint32
}

type ChildInodeEntry struct {
	Attributes InodeAttributes
	Child      InodeID
}

type LookUpInodeOp struct {
	Inode  InodeID
	Parent InodeID
	Name   string
	Entry  ChildInodeEntry
}

type GetInodeAttributesOp struct {
	Inode      InodeID
	Attributes InodeAttributes
}
