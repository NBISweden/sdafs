//go:build (cgo && !darwin && !(linux && arm64)) || windows

// On windows, cgofuse is supported without cgo

package cgofuseadapter

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/NBISweden/sdafs/internal/sdafs"
	"github.com/winfsp/cgofuse/fuse"
)

const CHECK_MOUNTED_DELAY = 100 * time.Millisecond

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

	checkForMount := true

	go func() {

		for checkForMount {
			// We loop here to trigger operations that will make the adapter
			// signal that it's mounted. We don't need to worry about
			// synchronisation since it's only a boolean and testing a few
			// extra times doesn't hurt

			time.Sleep(CHECK_MOUNTED_DELAY)

			os.Stat(mp) //nolint:errcheck
		}
	}()

	go func() {
		mounted := a.host.Mount(mp, mountOptions)

		checkForMount = false

		if !mounted {
			slog.Info("cgofuse mount failed for unknown reason")
			c <- fmt.Errorf("cgofuse mount failed")

			// If Init was called for some reason and we already signaled
			// success, make sure that Join doesn't wait (this shouldn't happen)
			a.destroyChan <- true
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
