# Architecture Documentation

## FUSE Abstraction Layer

### Overview

The sdafs project has been designed with an abstraction layer that allows it to work with multiple FUSE library implementations. This provides flexibility in choosing the best FUSE library for different platforms and use cases.

### Directory Structure

```
internal/fuseadapter/
├── fuseadapter.go        # Core interfaces and types
├── jacobsa/
│   └── jacobsa.go        # jacobsa/fuse adapter implementation
├── jacobsautil/
│   └── jacobsautil.go    # Utilities for jacobsa/fuse (e.g., WriteDirent)
└── cgofuse/
    └── cgofuse.go        # cgofuse adapter (planned/stub implementation)
```

### Core Interfaces

#### FileSystem Interface

The `FileSystem` interface defines the operations that any FUSE filesystem implementation must support:

```go
type FileSystem interface {
    GetInodeAttributes(ctx context.Context, op *GetInodeAttributesOp) error
    LookUpInode(ctx context.Context, op *LookUpInodeOp) error
    StatFS(ctx context.Context, op *StatFSOp) error
    OpenFile(ctx context.Context, op *OpenFileOp) error
    ReadFile(ctx context.Context, op *ReadFileOp) error
    OpenDir(ctx context.Context, op *OpenDirOp) error
    ReadDir(ctx context.Context, op *ReadDirOp) error
}
```

#### FUSEAdapter Interface

The `FUSEAdapter` interface abstracts the mounting and unmounting operations:

```go
type FUSEAdapter interface {
    Mount(mountpoint string, fs FileSystem, config *MountConfig) (MountedFileSystem, error)
    Unmount(mountpoint string) error
}
```

### Type Mapping

The abstraction layer defines its own types that are independent of any specific FUSE library:

- `InodeID` - Unique identifier for filesystem inodes
- `HandleID` - Unique identifier for open file handles
- `DirOffset` - Offset for directory reading operations
- `InodeAttributes` - Metadata for inodes (size, mode, ownership, timestamps)
- `OpContext` - Context information for operations (PID, UID, GID)
- `Dirent` - Directory entry representation

### Adapter Implementations

#### jacobsa Adapter

Located in `internal/fuseadapter/jacobsa/`, this adapter bridges between the abstraction layer and the [jacobsa/fuse](https://github.com/jacobsa/fuse) library.

**Key Features:**
- Pure Go implementation
- No C dependencies
- Well-tested on Linux
- Currently the default and recommended implementation

**Implementation Details:**
- The `fileSystemBridge` struct implements jacobsa's `fuseutil.FileSystem` interface
- Converts between adapter types and jacobsa types bidirectionally
- Handles differences in OpContext (jacobsa doesn't provide GID)

#### cgofuse Adapter (Planned)

Located in `internal/fuseadapter/cgofuse/`, this is a stub implementation for future cgofuse support.

**Planned Features:**
- Cross-platform support (Linux, macOS, Windows)
- Uses C libfuse library via cgo
- Better integration with native FUSE implementations

**Status:** Currently returns ENOSYS for all operations. Full implementation is planned for future releases.

### Integration with sdafs

The main `SDAfs` struct in `internal/sdafs/sdafs.go` implements the `FileSystem` interface from the abstraction layer:

1. It embeds `fuseadapter.FileSystemBase` which provides default implementations
2. It overrides the necessary methods to implement the actual filesystem logic
3. All jacobsa-specific types have been replaced with adapter types
4. Uses `jacobsautil.WriteDirent()` for directory entry serialization (library-specific)

### Adding a New FUSE Implementation

To add support for a new FUSE library:

1. Create a new package under `internal/fuseadapter/`
2. Implement the `FUSEAdapter` interface
3. Create a bridge struct that translates between the adapter interface and the library's interface
4. Add the new implementation to the switch statement in `cmd/sdafs/main.go`
5. Update documentation and tests

### Error Handling

The abstraction layer defines common error constants that map to syscall errors:

- `ErrNotImplemented` - `syscall.ENOSYS`
- `ErrNoEntry` - `syscall.ENOENT`
- `ErrIO` - `syscall.EIO`
- `ErrInvalid` - `syscall.EINVAL`
- `ErrAccess` - `syscall.EACCES`
- `ErrExist` - `syscall.EEXIST`

### Configuration

The FUSE implementation can be selected at runtime using the `--fuseimpl` command-line flag:

```bash
sdafs --fuseimpl jacobsa /mountpoint  # Use jacobsa/fuse
sdafs --fuseimpl cgofuse /mountpoint  # Use cgofuse (when implemented)
```

If no implementation is specified, `jacobsa` is used as the default.

### Testing

Each adapter should have its own test suite to ensure:
- Proper type conversions
- Correct error mapping
- Mount/unmount operations work correctly
- All FileSystem operations are properly bridged

The main sdafs tests continue to work with the abstraction layer, ensuring backward compatibility.

### Future Enhancements

Potential improvements to the abstraction layer:

1. **Complete cgofuse implementation** - Enable full cross-platform support
2. **Performance benchmarks** - Compare different FUSE implementations
3. **Dynamic loading** - Load adapters based on what's available at runtime
4. **Additional operations** - Support for write operations, extended attributes, etc.
5. **Better WriteDirent abstraction** - Make directory entry serialization more library-agnostic

### Design Decisions

#### Why an abstraction layer?

1. **Flexibility**: Different FUSE libraries have different strengths (performance, platform support, dependencies)
2. **Portability**: Easier to support multiple platforms
3. **Future-proofing**: Can switch FUSE libraries without rewriting the entire filesystem
4. **Testing**: Easier to mock and test filesystem operations

#### Why keep jacobsa-specific utilities?

Some operations (like `WriteDirent`) are inherently library-specific in their implementation. Rather than creating a complex abstraction for these, we keep library-specific utilities and call them appropriately. This provides a good balance between abstraction and practicality.

#### Why default to jacobsa?

- It's what was used originally
- It's pure Go with no C dependencies
- It works well on Linux (primary target platform)
- Well-tested and stable
