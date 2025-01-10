# sdafs

This repository provides a FUSE file system driver for the sensitive data
archive. The intended use is to provide an easy way to use the archive
off-platform.

## About

### FUSE driver

This should work as user (without additional privileges) installation on any
modern system providing the FUSE stack (i.e. `fusermount3`).

This should be usable. There are some possible improvements regarding cache
handling that can be done and it hasn't currently been tested for very large
datasets

### CSI

We also aim to provide a CSI to allow usage with kubernetes. This is still
work in progress and should not be expected to be usable at this point in time.

## Usage

To be usable, you need to have been granted access to datasets, for bigpicture
you can view available datasets and apply for access in
[REMS](https://bp-rems.sd.csc.fi/).

Once you have been granted access, you can download a configuration file with
access credentials from the
[login site](https://login.bp.nbis.se/).

Armed with you configuration (e.g. in `~/Download/s3cmd.conf`), you can launch
`sdafs` at `/where/you/want/to/mount` as so:

```bash
sdafs --credentialsfile ~/Download/s3cmd.conf /where/you/want/to/mount
```

### Troubleshooting

By default, `sdafs` will daemonize after some rudimentary checks. After that,
additional messages can be seen in the log file (`sdafs.log` in the current
directory unless overridden when running in the background). Alternatively,
`sdafs` can be run without detaching with by passing `--foreground`.

### Permissions

By default, `sdafs` will not allow access from other users. For use cases where
that is not enough (e.g. running some tool in a container solution), the flag
`--open` will allow access from all users.
