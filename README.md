# sdafs

This repository provides a FUSE file system driver for the sensitive data
archive. The intended use is to provide an easy way to use the archive
off-platform.

## FUSE driver

This should work as user (privilegeless) installation on any modern system
providing the FUSE stack (i.e. fusermount3).

This should be usable. There are some possible improvements regarding cache
handling that can be done and it hasn't currently been tested for very large
datasets

## CSI

We also aim to provide a CSI to allow usage with kubernetes. This is still
work in progress and should not be expected to be usable at this point in time.
