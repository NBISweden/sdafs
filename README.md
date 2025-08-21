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

### Tuning

The amount of data asked for in each request can be tuned with `--chunksize`.
What is best for you will depend on your use case - if you read files
sequentially (e.g. as making a copy), a fairly large value is likely to give
better performance (although with diminishing returns). If you instead have
a very random access pattern, a smaller value may be useful (again, with
diminishing returns when reducing).

A good way to think about this is that each request take a certain minimum
amount of time (e.g. because signals mean to travel back and forth,
authentication needs to be check, bookkeeping be done et.c.) and has another
part that depends on the request size (transferring more data takes longer).

If the additionally transferred data isn't used, obviously it's not worth doing
the transfer. But it is likely better (faster) to do one larger transfer than
two (or more) smaller.

### Troubleshooting

By default, `sdafs` will daemonize after some rudimentary checks. After that,
additional messages can be seen in the log file (`sdafs.log` in the current
directory unless overridden when running in the background). Alternatively,
`sdafs` can be run without detaching with by passing `--foreground`.

The verbosity of messages can be controlled through `--loglevel`, which takes
a number corresponding to [slog levels](https://pkg.go.dev/log/slog#Level). To
get more than you probably want, use a low number, e.g. `-50` (minus fifty).

### Permissions

By default, `sdafs` will not allow access from other users. For use cases where
that is not enough (e.g. running some tool in a container solution), the flag
`--open` will allow access from all users. This may possibly be needed for use
with e.g. Docker.

Using open may require a more liberal configuration for fuse than some systems
have as default (in particular, it's likely to require `user_allow_other` being
allowed in `/etc/fuse.conf`, which likely require root privileges to change).
