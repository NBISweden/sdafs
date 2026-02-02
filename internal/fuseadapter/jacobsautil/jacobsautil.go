// Package jacobsautil provides utility functions for working with jacobsa/fuse types
package jacobsautil

import (
	"github.com/NBISweden/sdafs/internal/fuseadapter"
	"github.com/jacobsa/fuse/fuseops"
	"github.com/jacobsa/fuse/fuseutil"
)

// WriteDirent writes a directory entry in jacobsa/fuse format
func WriteDirent(dst []byte, d fuseadapter.Dirent) int {
	jacobsaDirent := fuseutil.Dirent{
		Offset: fuseops.DirOffset(d.Offset),
		Inode:  fuseops.InodeID(d.Inode),
		Name:   d.Name,
		Type:   fuseutil.DirentType(d.Type),
	}
	return fuseutil.WriteDirent(dst, jacobsaDirent)
}
