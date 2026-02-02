//go:build cgofuse

// Package cgofuse provides a FUSE adapter implementation using cgofuse library
package cgofuse

import (
	"context"
	"fmt"

	"github.com/NBISweden/sdafs/internal/fuseadapter"
	"github.com/winfsp/cgofuse/fuse"
)

// Adapter implements the FUSEAdapter interface using cgofuse
type Adapter struct{}

// NewAdapter creates a new cgofuse adapter
func NewAdapter() *Adapter {
	return &Adapter{}
}

// Mount mounts a filesystem using cgofuse
func (a *Adapter) Mount(mountpoint string, fs fuseadapter.FileSystem, config *fuseadapter.MountConfig) (fuseadapter.MountedFileSystem, error) {
	// Create a bridge between our interface and cgofuse's interface
	bridge := &fileSystemBridge{fs: fs}

	// Create the host
	host := fuse.NewFileSystemHost(bridge)

	// Build mount options
	opts := make([]string, 0)
	if config.ReadOnly {
		opts = append(opts, "-o", "ro")
	}
	if config.FSName != "" {
		opts = append(opts, "-o", "fsname="+config.FSName)
	}
	if config.VolumeName != "" {
		opts = append(opts, "-o", "volname="+config.VolumeName)
	}
	for k, v := range config.Options {
		if v == "" {
			opts = append(opts, "-o", k)
		} else {
			opts = append(opts, "-o", k+"="+v)
		}
	}

	// Mount the filesystem
	success := host.Mount(mountpoint, opts)
	if !success {
		return nil, fmt.Errorf("cgofuse mount failed")
	}

	return &mountedFileSystem{host: host}, nil
}

// Unmount unmounts the filesystem at the given mountpoint
func (a *Adapter) Unmount(mountpoint string) error {
	// cgofuse handles unmount differently - usually through the host
	return fmt.Errorf("cgofuse unmount should be called via MountedFileSystem")
}

// mountedFileSystem wraps cgofuse's FileSystemHost
type mountedFileSystem struct {
	host *fuse.FileSystemHost
}

// Join waits for the filesystem to be unmounted
func (m *mountedFileSystem) Join(ctx context.Context) error {
	// cgofuse doesn't have a direct Join equivalent
	// The Mount call blocks, so this is typically not needed
	return nil
}

// Unmount unmounts the filesystem
func (m *mountedFileSystem) Unmount() error {
	return m.host.Unmount()
}

// fileSystemBridge bridges our FileSystem interface to cgofuse's FileSystemInterface
type fileSystemBridge struct {
	fuse.FileSystemBase
	fs fuseadapter.FileSystem
}

// TODO: Implement cgofuse bridge methods
// This is a placeholder implementation that would need to be completed
// when cgofuse support is actually added.

// Getattr implements the Getattr operation for cgofuse
func (b *fileSystemBridge) Getattr(path string, stat *fuse.Stat_t, fh uint64) int {
	// TODO: Convert path to inode and call GetInodeAttributes
	// This would require maintaining a path-to-inode mapping
	return -fuse.ENOSYS
}

// Open implements the Open operation for cgofuse
func (b *fileSystemBridge) Open(path string, flags int) (int, uint64) {
	// TODO: Convert path to inode and call OpenFile
	return -fuse.ENOSYS, 0
}

// Read implements the Read operation for cgofuse
func (b *fileSystemBridge) Read(path string, buff []byte, ofst int64, fh uint64) int {
	// TODO: Call ReadFile with the handle
	return -fuse.ENOSYS
}

// Readdir implements the Readdir operation for cgofuse
func (b *fileSystemBridge) Readdir(path string,
	fill func(name string, stat *fuse.Stat_t, ofst int64) bool,
	ofst int64,
	fh uint64) int {
	// TODO: Convert path to inode and call ReadDir
	return -fuse.ENOSYS
}

// Statfs implements the Statfs operation for cgofuse
func (b *fileSystemBridge) Statfs(path string, stat *fuse.Statfs_t) int {
	// TODO: Call StatFS
	return -fuse.ENOSYS
}
