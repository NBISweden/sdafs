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
	c := make(chan error, 1)

	a := &Adapter{
		fs:          fs,
		mountpoint:  mp,
		statusChan:  c,
		destroyChan: make(chan bool, 1),
	}

	var mountOptions []string = nil

	if len(options) > 0 {
		mountOptions = strings.Split(options, " ")
	}

	a.host = fuse.NewFileSystemHost(a)

	go func() {
		mounted := a.host.Mount(mp, mountOptions)

		if !mounted {
			slog.Info("cgofuse mount failed for unknown reason")
			c <- fmt.Errorf("cgofuse mount failed")
		}
	}()

	err := <-c

	return a, err
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
