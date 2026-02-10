//go:build !cgo || darwin || (linux && arm64)

// This file is just to enable building, cgofuse is not actually available
// with darwin for now

package cgofuseadapter

import (
	"log/slog"

	"github.com/NBISweden/sdafs/internal/sdafs"
)

type hostType = string

func CGOFuseAvailable() bool {
	return false
}

type Stat_t struct {
	Ino uint64
}
type Statfs_t struct {
	Bsize uint64
}

type filesystembase struct {
}

func Mount(mp string, fs *sdafs.SDAfs, options string) (*Adapter, error) {
	slog.Info("this should never happen")
	return nil, nil
}

func attribToStat(a *sdafs.InodeAttributes, st *Stat_t) {
}
