//go:build unix

package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/NBISweden/sdafs/internal/cgofuseadapter"
	"github.com/NBISweden/sdafs/internal/sdafs"
	"github.com/jacobsa/fuse"
	"github.com/jacobsa/fuse/fuseutil"
	"github.com/sevlyar/go-daemon"
)

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

// detachIfNeeded daemonizes if needed
func detachIfNeeded(c mainConfig) {
	if !c.foreground {
		context := new(daemon.Context)
		child, err := context.Reborn()

		if err != nil {
			log.Fatalf("Failed to detach")
		}

		if child != nil {
			os.Exit(0)
		}

		if err := context.Release(); err != nil {
			slog.Info("Unable to release pid file",
				"error", err.Error())
		}
	}
}
