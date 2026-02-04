package cgofuseadapter

import (
	"context"
	"encoding/binary"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	ops "github.com/jacobsa/fuse/fuseops"
	"github.com/jacobsa/fuse/fuseutil"

	"github.com/NBISweden/sdafs/internal/sdafs"
	"github.com/winfsp/cgofuse/fuse"
)

type MountedFS interface {
	Join(context.Context) error
}

type Adapter struct {
	fs         *sdafs.SDAfs
	mountpoint string
	fuse.FileSystemBase
}

func Mount(mp string, fs *sdafs.SDAfs) (*Adapter, error) {
	a := &Adapter{fs: fs, mountpoint: mp}

	host := fuse.NewFileSystemHost(a)
	host.Mount(mp, nil)
	return a, nil
}

func (a *Adapter) Join(context.Context) error {
	time.Sleep(1 * time.Second)

	return nil
}

func attribToStat(a *ops.InodeAttributes, st *fuse.Stat_t) {
	st.Size = int64(a.Size)
	st.Uid = a.Uid
	st.Gid = a.Gid
	st.Atim = fuse.NewTimespec(a.Atime)
	st.Ctim = fuse.NewTimespec(a.Ctime)
	st.Mtim = fuse.NewTimespec(a.Mtime)
	st.Nlink = a.Nlink
	st.Rdev = uint64(a.Rdev)

	st.Mode = uint32(a.Mode) & 0777
	if a.Mode&fs.ModeDir > 0 {
		st.Mode |= fuse.S_IFDIR
	} else {
		st.Mode |= fuse.S_IFREG
	}

}

// Getattr adapts Getattr between cgofuse and ja fuse APIs
func (a *Adapter) Getattr(path string, st *fuse.Stat_t, handle uint64) int {
	inode, err := a.filenameToInode(path)

	if err != nil {
		slog.Info("error in getattr filename lookup", "err", err, "path", path)
		return -fuse.ENOENT
	}

	op := &ops.GetInodeAttributesOp{
		Inode: inode,
	}
	err = a.fs.GetInodeAttributes(context.Background(), op)
	if err != nil {
		slog.Info("error from Getattr call", "err", err, "path", path)
		return -fuse.ENOENT
	}

	attribToStat(&op.Attributes, st)
	st.Ino = uint64(inode)

	return 0
}

// Statfs adapts Statfs between cgofuse and ja fuse APIs
func (a *Adapter) Statfs(path string, st *fuse.Statfs_t) int {
	// We shortcut here for now and don't call our statfs since
	// it's so stubby anyway

	st.Bsize = 65536

	slog.Info("statfs done", "path", path, "st", st)

	return 0
}

// Opendir adapts Opendir between cgofuse and ja fuse APIs
func (a *Adapter) Opendir(path string) (int, uint64) {

	inode, err := a.filenameToInode(path)

	if err != nil {
		slog.Info("error in opendir filename lookup", "err", err, "path", path)
		return -fuse.ENOENT, 0
	}

	op := &ops.OpenDirOp{
		Inode: inode,
	}
	err = a.fs.OpenDir(context.Background(), op)
	if err != nil {
		slog.Info("error from OpenDir call", "err", err, "path", path)
		return -fuse.ENOENT, 0
	}

	return 0, uint64(op.Handle)
}

// Readdir adapts Readdir between cgofuse and ja fuse APIs
func (a *Adapter) Readdir(path string,
	fill func(name string, stat *fuse.Stat_t, ofst int64) bool,
	offset int64,
	handle uint64) int {

	inode, err := a.filenameToInode(path)

	if err != nil {
		slog.Info("error in readdir filename lookup", "err", err, "path", path)
		return -fuse.ENOENT
	}

	op := &ops.ReadDirOp{
		Inode:  inode,
		Handle: ops.HandleID(handle),
		Offset: ops.DirOffset(offset),
		Dst:    make([]byte, 65536),
	}

	err = a.fs.ReadDir(context.Background(), op)
	if err != nil {
		slog.Info("error from readdir op", "err", err, "path", path)
		return -fuse.ENOENT
	}

	direntsData := op.Dst[:op.BytesRead]

	for len(direntsData) > 0 && err == nil {
		de, newDirentsData, err := getDirent(direntsData)

		if err != nil {
			slog.Info("error from readdir result parsing",
				"err", err, "path", path)
			return -fuse.ENOENT
		}

		statOp := &ops.GetInodeAttributesOp{
			Inode: de.Inode,
		}
		err = a.fs.GetInodeAttributes(context.Background(), statOp)
		if err != nil {
			slog.Info("error from stat for readdir result parsing",
				"err", err, "path", path)
			return -fuse.ENOENT
		}

		st := &fuse.Stat_t{}
		st.Ino = uint64(de.Inode)

		attribToStat(&statOp.Attributes, st)

		fill(de.Name, st, int64(de.Offset))
		direntsData = newDirentsData
	}

	return 0
}

func (a *Adapter) filenameToInode(path string) (ops.InodeID, error) {

	if path == "/" {
		return ops.InodeID(ops.RootInodeID), nil
	}

	pathParts := strings.Split(strings.TrimSpace(path), "/")

	inode := ops.InodeID(ops.RootInodeID)
	for _, name := range pathParts[1:] {
		op := ops.LookUpInodeOp{
			Parent: inode,
			Name:   name,
		}

		err := a.fs.LookUpInode(context.Background(), &op)
		if err != nil {
			return inode, fmt.Errorf(
				"error from LookUpInode when looking for %s: %v", name,
				err)
		}

		inode = op.Entry.Child
	}

	return inode, nil
}

type ja_fuse_dirent struct {
	ino     uint64
	off     uint64
	namelen uint32
	type_   uint32
}

func getDirent(in []byte) (*fuseutil.Dirent, []byte, error) {
	dirent := &ja_fuse_dirent{}

	// Decoding the entire struct at once doesn't seem to work for whatever
	// reason

	_, err := binary.Decode(in[:8], binary.NativeEndian, &dirent.ino)
	if err != nil {
		return nil, in, fmt.Errorf("couldn't parse direntry ino: %v", err)
	}

	_, err = binary.Decode(in[8:16], binary.NativeEndian, &dirent.off)
	if err != nil {
		return nil, in, fmt.Errorf("couldn't parse direntry off: %v", err)
	}

	_, err = binary.Decode(in[16:20], binary.NativeEndian, &dirent.namelen)
	if err != nil {
		return nil, in, fmt.Errorf("couldn't parse direntry namelen: %v", err)
	}

	_, err = binary.Decode(in[20:24], binary.NativeEndian, &dirent.type_)
	if err != nil {
		return nil, in, fmt.Errorf("couldn't parse direntry type: %v", err)
	}

	name := in[24 : 24+int(dirent.namelen)]

	rDirent := &fuseutil.Dirent{
		Name:   string(name),
		Offset: ops.DirOffset(dirent.off),
		Inode:  ops.InodeID(dirent.ino),
	}

	pad := (8 - int(dirent.namelen%8)) % 8
	return rDirent, in[24+pad+int(dirent.namelen):], nil
}

func (a *Adapter) Access(path string, mask uint32) int {
	slog.Info("access", "path", path)

	_, err := a.filenameToInode(path)

	if err != nil {
		slog.Info("error in access filename lookup", "err", err, "path", path)
		return -fuse.ENOENT
	}

	return 0
}

// func (a *Adapter) Create(path string, flags int, mode uint32) (int, uint64) {
// 	slog.Info("create", "path", path)
// 	return -fuse.EACCES, ^uint64(0)
// }

func (a *Adapter) Release(path string, fh uint64) int {
	_, err := a.filenameToInode(path)

	if err != nil {
		slog.Info("error in Release filename lookup", "err", err, "path", path)
		return -fuse.ENOENT
	}

	return 0
}

func (a *Adapter) ReleaseDir(path string, fh uint64) int {
	_, err := a.filenameToInode(path)

	if err != nil {
		slog.Info("error in ReleaseDir filename lookup", "err", err, "path", path)
		return -fuse.ENOENT
	}
	return 0
}

// Opendir adapts Opendir between cgofuse and ja fuse APIs
func (a *Adapter) Open(path string, flags int) (int, uint64) {

	inode, err := a.filenameToInode(path)

	if err != nil {
		slog.Info("error in Open filename lookup", "err", err, "path", path)
		return -fuse.ENOENT, 0
	}

	op := &ops.OpenFileOp{
		Inode: inode,
	}
	err = a.fs.OpenFile(context.Background(), op)
	if err != nil {
		slog.Info("error from OpenFile call", "err", err, "path", path)
		return -fuse.ENOENT, 0
	}

	return 0, uint64(op.Handle)
}

func (a *Adapter) Read(path string, buf []byte, offset int64, handle uint64) int {

	inode, err := a.filenameToInode(path)

	if err != nil {
		slog.Info("error in Read filename lookup", "err", err, "path", path)
		return -fuse.ENOENT
	}

	op := &ops.ReadFileOp{
		Inode:  inode,
		Handle: ops.HandleID(handle),
		Offset: offset,
		Size:   int64(len(buf)),
		Dst:    buf,
	}
	err = a.fs.ReadFile(context.Background(), op)
	if err != nil {
		slog.Info("error from ReadFile call", "err", err, "path", path)
		return -fuse.ENOENT
	}

	return op.BytesRead

}
