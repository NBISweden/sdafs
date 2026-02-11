//go:build (cgo && !darwin && !(linux && arm64)) || windows

// On windows, cgofuse is supported without cgo

package cgofuseadapter

import (
	"fmt"
	"io/fs"
	"log/slog"
	"strings"

	"github.com/NBISweden/sdafs/internal/sdafs"
	"github.com/winfsp/cgofuse/fuse"
)

func CGOFuseAvailable() bool {
	return true
}

type Stat_t = fuse.Stat_t
type Statfs_t = fuse.Statfs_t

type filesystembase = fuse.FileSystemBase
type hostType = fuse.FileSystemHost

func Mount(mp string, fs *sdafs.SDAfs, options string) (*Adapter, error) {
	a := &Adapter{
		fs:         fs,
		mountpoint: mp,
	}

	var mountOptions []string = nil

	if len(options) > 0 {
		mountOptions = strings.Split(options, " ")
	}

	a.host = fuse.NewFileSystemHost(a)
	mounted := a.host.Mount(mp, mountOptions)

	var reterr error
	if !mounted {
		slog.Info("cgofuse mount failed for unknown reason")
		reterr = fmt.Errorf("cgofuse mount failed")
	}

	return a, reterr
}

func attribToStat(a *sdafs.InodeAttributes, st *Stat_t) {
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
