package main

import (
	"log/slog"

	"github.com/NBISweden/sdafs/internal/cgofuseadapter"
	"github.com/NBISweden/sdafs/internal/sdafs"
)

func doMount(c mainConfig, fs *sdafs.SDAfs) (cgofuseadapter.MountedFS, error) {
	slog.Info("Starting cgofuse mount of SDA",
		"rootURL", c.sdafsconf.RootURL,
		"mountpoint", c.mountPoint)

	mount, err := cgofuseadapter.Mount(c.mountPoint, fs, c.cgofuseOptions)
	return mount, err
}

// This is a noop
func doUnmount(mp string) error {
	return nil
}

// detachIfNeeded daemonizes if needed
func detachIfNeeded(c mainConfig) {

	return
}
