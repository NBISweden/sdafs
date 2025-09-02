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

## CSI

We also provide a CSI driver to allow usage with kubernetes. Due to its nature,
providing system services this likely needs exceptions/acknowledgements from
security monitoring/admission systems.

The files in `deploy` provide samples that can be used for deploying in testing
environment. We strongly recommend going through them to understand what
components are involved and what privileges they run with.

The files in `deploy` reflect system paths used by a typical kubernetes
deployment. You may need to adapt them to tailor to your system.

### Requirements

To use sdafs within kubernetes, a setup is needed that provides the roles of
attacher and provisioner as traditional for CSI (an example of configuration
for these is available in `deploy/attacher.yaml`).

At least one `StorageClass` resource must also be created with `provisioner` set
to `csi.sda.nbis.se`. `deploy/storageclass.yaml` has an example that also
demonstrates various options that can be used.

A typical setup will need roles and permissions as per configured in
`deploy/attacher.yaml`.

#### Running sdafs CSI inside Kubernetes

As traditionally is done, the sdafs CSI driver can be deployed inside kubernetes
(as in `deploy/csi-sdafs.yaml`). Due to its nature as a CSI driver it requires
a fair bit of permissions that may require acknowledgements/exceptions from
security policies, these are:

- In pod `securityContext`:
  - `runAsUser: 0`
    - needed to access the directory to actually mount directories for pods
- In container `securityContext`:
  - `privilged: true`
    - needed since we need to use bidirectional mount propagate for mounts
      to show up outside of the CSI pod
  - `allowPrivilegeEscalation: true`
    - Required due to `privileged: true` above
- In `volumes`, there are a number of `hostPath` volumes:
  - `/var/lib/kubelet/plugins_registry`
    - needed to register the CSI plugin with the system
  - `/var/lib/kubelet/plugins/csi.sda.nbis.se`
    - Used for communication with attcher and provisioner. Could possibly be
      worked around by putting attacher and provisioner in the same pod,
      but that would mean they effectively run with more privileges and less
      separation from the high privileges needed to mount the pod directories
      whiile we'd still need the other `hostPath` volumes
  - `/var/lib/kubelet/pods`
    - `kubelet` will make directories for pods under this directory where we
      need to mount sdafs (`kubelet`) binds it into the right place in the
      appropriate container mount namespace. Permissions analogue to
      `root` privilege in the host namespace is required to create/stat these
      directories
  - `/dev/fuse`
    - Needed for integration with the FUSE kernel portions

There is a `Dockerfile` in the repository for building container images with minimal
extra contents and images should be published to the `ghcr.io/nbisweden/sdafs`
repository upon releases.

#### Running sdafs CSI outside kubernetes

The sdafs CSI driver can also be run "outside" of kubernetes. It must still
be run with access to the respective host mount namespace (i.e. typically
alongside `kubelet`). Similarly, using a provisioner is still mandatory.

### Credentials provisioning

The csi-provisioner as used will manage secret provisioning when configured in
the `StorageClass` (see `deploy/storageclass.yaml` for an example). This allows
a flexible way of providing secrets with the ability to use templates.

The example `StorageClass` menioned will look for a `Secret` in the same
namespace as the `PersistantVolumeClaim` the provisioner is trying to satisfy,
as for the actual name of the `Secret`, the example will pick that up from
annotations to the `PersistantVolumeClaim` (expecting an annotation
`sda.nbis.se/token-secret`, but that is also configurable). Similarly to how
the `StorageClass` allows specifying what `Secret` to pick up the credentials
from, it also allows you to specify what key in the `Secret` to use by
passing a `tokenkey` (defaults to `token`)

The appointed `Secret` will be searched for the key mentioned above. The value
will be used to create the file with credentals information passed to `sdafs`.

Using templates in `StorageClass` allows for very flexible scenarios, e.g.
having multple `PersistantVolumeClaim`s in a namespace using credentials
provided by different `Secret`s.
