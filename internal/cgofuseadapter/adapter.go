package cgofuseadapter

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/NBISweden/sdafs/internal/sdafs"
)

// MountedFS is the interface to abstract a filesystem once mounted
type MountedFS interface {
	Join(context.Context) error
}

// Adapter is the struct that keeps all information for this adapter
type Adapter struct {
	fs         sda
	mountpoint string
	host       *hostType

	filesystembase
}

type sda interface {
	OpenDir(_ context.Context, op *sdafs.OpenDirOp) error
	ReadDir(_ context.Context, op *sdafs.ReadDirOp) error
	OpenFile(_ context.Context, op *sdafs.OpenFileOp) error
	ReadFile(_ context.Context, op *sdafs.ReadFileOp) error
	GetInodeAttributes(_ context.Context, op *sdafs.GetInodeAttributesOp) error
	LookUpInode(_ context.Context, op *sdafs.LookUpInodeOp) error
	ReleaseFileHandle(_ context.Context, op *sdafs.ReleaseFileHandleOp) error
}

func (a *Adapter) Join(context.Context) error {
	// cgofuse mount call will not return until the filesystem is unmounted
	// so nothing to do here

	return nil
}

// Getattr adapts Getattr between cgofuse and ja fuse APIs
func (a *Adapter) Getattr(path string, st *Stat_t, handle uint64) int {
	inode, err := a.filenameToInode(path)

	if err != nil {
		slog.Info("error in getattr filename lookup", "err", err, "path", path)
		return -mapError(err)
	}

	op := &sdafs.GetInodeAttributesOp{
		Inode: inode,
	}
	err = a.fs.GetInodeAttributes(context.Background(), op)
	if err != nil {
		slog.Info("error from Getattr call", "err", err, "path", path)
		return -mapError(err)
	}

	attribToStat(&op.Attributes, st)
	st.Ino = uint64(inode)

	return 0
}

// Statfs adapts Statfs between cgofuse and ja fuse APIs
func (a *Adapter) Statfs(path string, st *Statfs_t) int {
	// We shortcut here for now and don't call our statfs since
	// it's so stubby anyway
	// slog.Info("Statfs is called", "path", path)
	st.Bsize = 65536

	return 0
}

// Opendir adapts Opendir between cgofuse and ja fuse APIs
func (a *Adapter) Opendir(path string) (int, uint64) {
	inode, err := a.filenameToInode(path)

	if err != nil {
		slog.Info("error in opendir filename lookup", "err", err, "path", path)
		return -mapError(err), 0
	}

	op := &sdafs.OpenDirOp{
		Inode: inode,
	}
	err = a.fs.OpenDir(context.Background(), op)
	if err != nil {
		slog.Info("error from OpenDir call", "err", err, "path", path)
		return -mapError(err), 0
	}

	return 0, uint64(op.Handle)
}

// Readdir adapts Readdir between cgofuse and ja fuse APIs
func (a *Adapter) Readdir(path string,
	fill func(name string, stat *Stat_t, ofst int64) bool,
	offset int64,
	handle uint64) int {

	inode, err := a.filenameToInode(path)

	if err != nil {
		slog.Info("error in readdir filename lookup", "err", err, "path", path)
		return -mapError(err)
	}

	op := &sdafs.ReadDirOp{
		Inode:  inode,
		Handle: sdafs.HandleID(handle),
		Offset: sdafs.DirOffset(offset),
		Dst:    make([]byte, 16384),
	}

	// FIXME: Better way of managing this?
	// Handle EAGAIN for dataset loading here
	// otherwise Windows Explorer easily gets
	// sad and throws up scary dialogs

	err = syscall.EAGAIN
	failCount := 0
	for errors.Is(err, syscall.EAGAIN) && failCount < 12000 {
		err = a.fs.ReadDir(context.Background(), op)

		if err != nil && !errors.Is(err, syscall.EAGAIN) {
			slog.Info("error from readdir op", "err", err, "path", path)
			return -mapError(err)
		}

		if err != nil {
			time.Sleep(5 * time.Millisecond)
		}
		failCount += 1
	}

	if err != nil {
		slog.Info("error from readdir op", "err", err, "path", path)
		return -mapError(err)
	}

	if op.BytesRead > 0 && op.BytesRead < 24 {
		// Invalid directory entry, doesn't hold the base structure
		return -mapError(syscall.EIO) // FIXME: Error type?
	}

	direntsData := op.Dst[:op.BytesRead]

	// Real call done, now walk through results and call the fill function
	// for each entry
	for len(direntsData) > 0 && err == nil {
		de, newDirentsData, err := getDirent(direntsData)

		if err != nil {
			slog.Info("error from readdir result parsing",
				"err", err, "path", path)
			return -mapError(err)
		}

		// Since this interface expects a Stat_t, we get one (since this
		// is all preloaded, it's expected to be cheap)
		statOp := &sdafs.GetInodeAttributesOp{
			Inode: de.Inode,
		}
		err = a.fs.GetInodeAttributes(context.Background(), statOp)
		if err != nil {
			slog.Info("error from stat for readdir result parsing",
				"err", err, "path", path)
			return -mapError(err)
		}

		st := &Stat_t{
			Ino: uint64(de.Inode),
		}
		attribToStat(&statOp.Attributes, st)

		ok := fill(de.Name, st, int64(de.Offset))
		if !ok {
			return 0
		}
		direntsData = newDirentsData
	}

	return 0
}

func (a *Adapter) filenameToInode(path string) (sdafs.InodeID, error) {

	inode := sdafs.InodeID(sdafs.RootInodeID)

	if path == "/" {
		return inode, nil
	}

	pathParts := strings.Split(strings.TrimSpace(path), "/")

	for _, name := range pathParts[1:] {
		op := sdafs.LookUpInodeOp{
			Parent: inode,
			Name:   name,
		}

		err := a.fs.LookUpInode(context.Background(), &op)
		if err != nil {
			return inode, fmt.Errorf(
				"error from LookUpInode when looking for %s: %w", name,
				err)
		}

		inode = sdafs.InodeID(op.Entry.Child)
	}

	return inode, nil
}

type dirent_binary_format struct {
	ino     uint64
	off     uint64
	namelen uint32
	type_   uint32
}

func mapError(e error) int {
	for _, t := range []syscall.Errno{
		syscall.EACCES,
		syscall.EINVAL,
		syscall.EEXIST,
		syscall.EROFS,
		syscall.EIO,
		syscall.EAGAIN} {
		if errors.Is(e, t) {
			slog.Debug("returning error", "in", e, "out", int(t))
			return int(t)
		}
	}
	slog.Debug("returning error", "in", e, "out", int(syscall.EIO))

	return int(syscall.EIO)
}

func getDirent(in []byte) (*sdafs.Dirent, []byte, error) {
	if len(in) < 24 {
		return nil, in, fmt.Errorf("input too small")
	}

	dirent := &dirent_binary_format{}

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

	if len(in) < 24+int(dirent.namelen) {
		return nil, in, fmt.Errorf("input too small")
	}

	name := in[24 : 24+int(dirent.namelen)]

	rDirent := &sdafs.Dirent{
		Name:   string(name),
		Offset: sdafs.DirOffset(dirent.off),
		Inode:  sdafs.InodeID(dirent.ino),
	}

	pad := (8 - int(dirent.namelen%8)) % 8
	return rDirent, in[24+pad+int(dirent.namelen):], nil
}

// Access adapts Access between cgofuse and jacobsa fuse APIs
// for cgofuse we only support wide open so access is always accepted
func (a *Adapter) Access(path string, mask uint32) int {
	// W_OK is 2
	if mask&2 > 0 {
		return -mapError(syscall.EROFS)
	}

	_, err := a.filenameToInode(path)

	if err != nil {
		slog.Info("error in access filename lookup", "err", err, "path", path)
		return -mapError(err)
	}

	// TODO: Maybe do execute check here?
	return 0
}

// Release adapts Close between cgofuse and jacobsa fuse APIs
func (a *Adapter) Release(path string, fh uint64) int {
	_, err := a.filenameToInode(path)

	if err != nil {
		slog.Info("error in Release filename lookup", "err", err, "path", path)
		return -mapError(err)
	}

	err = a.fs.ReleaseFileHandle(context.Background(),
		&sdafs.ReleaseFileHandleOp{
			Handle: sdafs.HandleID(fh),
		})

	if err != nil {
		return -mapError(err)
	}

	return 0
}

// ReleaseDir adapts Closedir between cgofuse and jacobsa fuse APIs
func (a *Adapter) ReleaseDir(path string, fh uint64) int {
	_, err := a.filenameToInode(path)

	if err != nil {
		slog.Info("error in ReleaseDir filename lookup", "err", err, "path", path)
		return -mapError(err)
	}
	return 0
}

// Open adapts Open for file between cgofuse and jacobsa fuse APIs
func (a *Adapter) Open(path string, flags int) (int, uint64) {

	for _, flag := range []int{os.O_WRONLY, os.O_RDWR, os.O_CREATE, os.O_TRUNC, os.O_EXCL, os.O_APPEND} {
		if flags&flag > 0 {
			return -mapError(syscall.EINVAL), 0
		}
	}

	inode, err := a.filenameToInode(path)

	if err != nil {
		slog.Info("error in Open filename lookup", "err", err, "path", path)
		return -mapError(err), 0
	}

	op := &sdafs.OpenFileOp{
		Inode: inode,
	}
	err = a.fs.OpenFile(context.Background(), op)
	if err != nil {
		slog.Info("error from OpenFile call", "err", err, "path", path)
		return -mapError(err), 0
	}

	return 0, uint64(op.Handle)
}

// Read adapts Read for file between cgofuse and jacobsa fuse APIs
func (a *Adapter) Read(path string,
	buf []byte,
	offset int64,
	handle uint64) int {
	inode, err := a.filenameToInode(path)

	if err != nil {
		slog.Info("error in Read filename lookup", "err", err, "path", path)
		return -mapError(err)
	}

	op := &sdafs.ReadFileOp{
		Inode:  inode,
		Handle: sdafs.HandleID(handle),
		Offset: offset,
		Size:   int64(len(buf)),
		Dst:    buf,
	}
	err = a.fs.ReadFile(context.Background(), op)
	if err != nil {
		slog.Info("error from ReadFile call", "err", err, "path", path)
		return -mapError(err)
	}

	return op.BytesRead
}
