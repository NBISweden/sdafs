//go:build unix

package sdafs

// Compatibility layer to allow for builds for Windows, for Unixes
// we simply alias everything from jacobsa fuse

import (
	"syscall"

	"github.com/jacobsa/fuse"
	"github.com/jacobsa/fuse/fuseops"
	"github.com/jacobsa/fuse/fuseutil"
)

type ReadDirOp = fuseops.ReadDirOp
type OpContext = fuseops.OpContext
type GetInodeAttributesOp = fuseops.GetInodeAttributesOp
type LookUpInodeOp = fuseops.LookUpInodeOp
type StatFSOp = fuseops.StatFSOp
type OpenFileOp = fuseops.OpenFileOp
type ReadFileOp = fuseops.ReadFileOp
type ReleaseFileHandleOp = fuseops.ReleaseFileHandleOp
type OpenDirOp = fuseops.OpenDirOp

type Dirent = fuseutil.Dirent
type InodeID = fuseops.InodeID
type DirOffset = fuseops.DirOffset
type HandleID = fuseops.HandleID
type InodeAttributes = fuseops.InodeAttributes

const RootInodeID = fuseops.RootInodeID
const ENOENT = fuse.ENOENT
const EIO = fuse.EIO
const EEXIST = fuse.EEXIST
const EINVAL = fuse.EINVAL
const EACCES = syscall.EACCES
const EAGAIN = syscall.EAGAIN

type notImplemented = fuseutil.NotImplementedFileSystem

func writeDirent(b []byte, d Dirent) int {
	return fuseutil.WriteDirent(b, d)
}
