package jacobsa

import (
	"context"
	"fmt"

	"github.com/NBISweden/sdafs/internal/fuseadapter"
	"github.com/jacobsa/fuse"
	"github.com/jacobsa/fuse/fuseops"
	"github.com/jacobsa/fuse/fuseutil"
)

// ConvertDirentToJacobsa converts our Dirent type to jacobsa's Dirent type
func ConvertDirentToJacobsa(d fuseadapter.Dirent) fuseutil.Dirent {
	return fuseutil.Dirent{
		Offset: fuseops.DirOffset(d.Offset),
		Inode:  fuseops.InodeID(d.Inode),
		Name:   d.Name,
		Type:   fuseutil.DirentType(d.Type),
	}
}

// Adapter implements the FUSEAdapter interface using jacobsa/fuse
type Adapter struct{}

// NewAdapter creates a new jacobsa/fuse adapter
func NewAdapter() *Adapter {
	return &Adapter{}
}

// Mount mounts a filesystem using jacobsa/fuse
func (a *Adapter) Mount(mountpoint string, fs fuseadapter.FileSystem, config *fuseadapter.MountConfig) (fuseadapter.MountedFileSystem, error) {
	// Create a bridge between our interface and jacobsa's interface
	bridge := &fileSystemBridge{fs: fs}
	server := fuseutil.NewFileSystemServer(bridge)

	// Convert our config to jacobsa's MountConfig
	jacobsaConfig := &fuse.MountConfig{
		ReadOnly:                  config.ReadOnly,
		DisableDefaultPermissions: config.DisableDefaultPermissions,
		FSName:                    config.FSName,
		VolumeName:                config.VolumeName,
		FuseImpl:                  fuse.FUSEImplMacFUSE,
	}

	if config.Options != nil {
		jacobsaConfig.Options = make(map[string]string)
		for k, v := range config.Options {
			jacobsaConfig.Options[k] = v
		}
	}

	mfs, err := fuse.Mount(mountpoint, server, jacobsaConfig)
	if err != nil {
		return nil, fmt.Errorf("jacobsa mount failed: %w", err)
	}

	return &mountedFileSystem{mfs: mfs}, nil
}

// Unmount unmounts the filesystem at the given mountpoint
func (a *Adapter) Unmount(mountpoint string) error {
	return fuse.Unmount(mountpoint)
}

// mountedFileSystem wraps jacobsa's MountedFileSystem
type mountedFileSystem struct {
	mfs *fuse.MountedFileSystem
}

// Join waits for the filesystem to be unmounted
func (m *mountedFileSystem) Join(ctx context.Context) error {
	return m.mfs.Join(ctx)
}

// Unmount unmounts the filesystem (this is called on the mount, not the adapter)
func (m *mountedFileSystem) Unmount() error {
	// The unmount should be called via the adapter's Unmount method
	// This is here for interface compatibility
	return fmt.Errorf("use adapter.Unmount() instead")
}

// fileSystemBridge bridges our FileSystem interface to jacobsa's fuseutil.FileSystem
type fileSystemBridge struct {
	fuseutil.NotImplementedFileSystem
	fs fuseadapter.FileSystem
}

// GetInodeAttributes bridges to our interface
func (b *fileSystemBridge) GetInodeAttributes(ctx context.Context, op *fuseops.GetInodeAttributesOp) error {
	adapterOp := &fuseadapter.GetInodeAttributesOp{
		Inode: fuseadapter.InodeID(op.Inode),
	}

	err := b.fs.GetInodeAttributes(ctx, adapterOp)
	if err != nil {
		return err
	}

	op.Attributes = fuseops.InodeAttributes{
		Size:  adapterOp.Attributes.Size,
		Nlink: adapterOp.Attributes.Nlink,
		Mode:  adapterOp.Attributes.Mode,
		Uid:   adapterOp.Attributes.Uid,
		Gid:   adapterOp.Attributes.Gid,
		Atime: adapterOp.Attributes.Atime,
		Mtime: adapterOp.Attributes.Mtime,
		Ctime: adapterOp.Attributes.Ctime,
	}

	return nil
}

// LookUpInode bridges to our interface
func (b *fileSystemBridge) LookUpInode(ctx context.Context, op *fuseops.LookUpInodeOp) error {
	adapterOp := &fuseadapter.LookUpInodeOp{
		Parent: fuseadapter.InodeID(op.Parent),
		Name:   op.Name,
	}

	err := b.fs.LookUpInode(ctx, adapterOp)
	if err != nil {
		return err
	}

	op.Entry.Child = fuseops.InodeID(adapterOp.Entry.Child)
	op.Entry.Attributes = fuseops.InodeAttributes{
		Size:  adapterOp.Entry.Attributes.Size,
		Nlink: adapterOp.Entry.Attributes.Nlink,
		Mode:  adapterOp.Entry.Attributes.Mode,
		Uid:   adapterOp.Entry.Attributes.Uid,
		Gid:   adapterOp.Entry.Attributes.Gid,
		Atime: adapterOp.Entry.Attributes.Atime,
		Mtime: adapterOp.Entry.Attributes.Mtime,
		Ctime: adapterOp.Entry.Attributes.Ctime,
	}

	return nil
}

// StatFS bridges to our interface
func (b *fileSystemBridge) StatFS(ctx context.Context, op *fuseops.StatFSOp) error {
	adapterOp := &fuseadapter.StatFSOp{}

	err := b.fs.StatFS(ctx, adapterOp)
	if err != nil {
		return err
	}

	op.BlockSize = adapterOp.BlockSize
	op.Blocks = adapterOp.Blocks
	op.BlocksFree = adapterOp.BlocksFree
	op.BlocksAvailable = adapterOp.BlocksAvail
	op.IoSize = adapterOp.IoSize
	op.Inodes = adapterOp.Inodes
	op.InodesFree = adapterOp.InodesFree

	return nil
}

// OpenFile bridges to our interface
func (b *fileSystemBridge) OpenFile(ctx context.Context, op *fuseops.OpenFileOp) error {
	adapterOp := &fuseadapter.OpenFileOp{
		OpContext: fuseadapter.OpContext{
			Pid: op.OpContext.Pid,
			Uid: op.OpContext.Uid,
			Gid: 0, // jacobsa doesn't provide GID in OpContext
		},
		Inode: fuseadapter.InodeID(op.Inode),
	}

	err := b.fs.OpenFile(ctx, adapterOp)
	if err != nil {
		return err
	}

	op.Handle = fuseops.HandleID(adapterOp.Handle)

	return nil
}

// ReadFile bridges to our interface
func (b *fileSystemBridge) ReadFile(ctx context.Context, op *fuseops.ReadFileOp) error {
	adapterOp := &fuseadapter.ReadFileOp{
		Inode:  fuseadapter.InodeID(op.Inode),
		Handle: fuseadapter.HandleID(op.Handle),
		Offset: op.Offset,
		Dst:    op.Dst,
	}

	err := b.fs.ReadFile(ctx, adapterOp)
	if err != nil {
		return err
	}

	op.BytesRead = adapterOp.BytesRead

	return nil
}

// OpenDir bridges to our interface
func (b *fileSystemBridge) OpenDir(ctx context.Context, op *fuseops.OpenDirOp) error {
	adapterOp := &fuseadapter.OpenDirOp{
		OpContext: fuseadapter.OpContext{
			Pid: op.OpContext.Pid,
			Uid: op.OpContext.Uid,
			Gid: 0, // jacobsa doesn't provide GID in OpContext
		},
		Inode: fuseadapter.InodeID(op.Inode),
	}

	return b.fs.OpenDir(ctx, adapterOp)
}

// ReadDir bridges to our interface
func (b *fileSystemBridge) ReadDir(ctx context.Context, op *fuseops.ReadDirOp) error {
	adapterOp := &fuseadapter.ReadDirOp{
		Inode:  fuseadapter.InodeID(op.Inode),
		Offset: fuseadapter.DirOffset(op.Offset),
		Dst:    op.Dst,
	}

	err := b.fs.ReadDir(ctx, adapterOp)
	if err != nil {
		return err
	}

	op.BytesRead = adapterOp.BytesRead

	return nil
}
