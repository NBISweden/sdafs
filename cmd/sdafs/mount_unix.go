//go:build unix

package main

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"

	"github.com/NBISweden/sdafs/internal/cgofuseadapter"
	"github.com/NBISweden/sdafs/internal/sdafs"
	"github.com/jacobsa/fuse"
	"github.com/jacobsa/fuse/fuseutil"
)

const DAEMON_MARKER = "SDAFS_DAEMON"
const DAEMON_VALUE = "yes"

func doMount(c mainConfig, fs *sdafs.SDAfs) (cgofuseadapter.MountedFS, error) {

	mountConfig := &fuse.MountConfig{
		ReadOnly:                  true,
		FuseImpl:                  fuse.FUSEImplMacFUSE,
		DisableDefaultPermissions: true,
		FSName:                    fmt.Sprintf("SDA_%s", c.sdafsconf.RootURL),
		VolumeName:                fmt.Sprintf("SDA mount of %s", c.sdafsconf.RootURL),
	}

	if c.open {
		mountConfig.Options = make(map[string]string)
		mountConfig.Options["allow_other"] = ""
	}

	var err error
	var mount cgofuseadapter.MountedFS

	if c.cgofuse {
		slog.Info("Starting cgofuse mount of SDA",
			"rootURL", c.sdafsconf.RootURL,
			"mountpoint", c.mountPoint)
		return cgofuseadapter.Mount(c.mountPoint, fs, c.cgofuseOptions)
	}

	mount, err = fuse.Mount(c.mountPoint, fuseutil.NewFileSystemServer(fs), mountConfig)

	if err == nil {
		slog.Info("SDA mount ready", "rootURL", c.sdafsconf.RootURL, "mountpoint", c.mountPoint)

	}

	return mount, err
}

func doUnmount(mp string) error {
	return fuse.Unmount(mp)
}

// detachIfNeeded daemonizes if needed. It does this by setting a marker and
// re-execing with a pipe for communication any issues on the initial mount
// (we use a pipe rather than just preserving os.Stderr to be able to know
// if we need to set a non-zero exit code).
func detachIfNeeded(c mainConfig) (*os.File, error) {
	if c.foreground {
		return nil, nil
	}

	if os.Getenv(DAEMON_MARKER) == DAEMON_VALUE {
		// child

		err := os.Stdin.Close()
		if err != nil {
			return nil, fmt.Errorf("couldn't close stdin after detach: %w", err)
		}

		err = os.Stdout.Close()
		if err != nil {
			return nil, fmt.Errorf("couldn't close stdout after detach: %w", err)
		}

		// We keep the pipe-as-stderr but return it as the error pipe
		return os.Stderr, nil
	}

	err := os.Setenv(DAEMON_MARKER, DAEMON_VALUE)
	if err != nil {
		return nil, fmt.Errorf("set marker environment value: %w", err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("couldn't create pipe for communication: %w", err)
	}

	p := os.ProcAttr{
		Files: []*os.File{w, w, w},
	}

	proc, err := os.StartProcess("/proc/self/exe", os.Args, &p)

	if err != nil || proc == nil {
		log.Fatalf("Failed to re-exex for detach: %v", err)
	}

	err = w.Close()
	if err != nil {
		log.Fatalf("failure while closing intercommunication pipe: %v", err)
	}

	// This is expected to be small
	errortext, err := io.ReadAll(r)

	if err != nil {
		log.Fatalf("failed to read error stream from detached process: %v", err)
	}

	for i := 0; i < len(errortext); {

		n, err := os.Stderr.Write(errortext[i:])

		i += n
		if err != nil {
			log.Fatalf("error while writing: %v", err)
		}
	}

	if len(errortext) == 0 {
		// No news is good news
		os.Exit(0)
	}

	os.Exit(1)

	return nil, err
}
