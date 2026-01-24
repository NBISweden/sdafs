package csidriver

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
)

// Return a suitable path for a token
func (d *Driver) getTokenFilePath(v *volumeInfo) string {
	return path.Join(*d.tokenDir, "token-"+v.ID)
}

// Return a suitable path for a CA
func (d *Driver) getCAFilePath(v *volumeInfo) string {
	return path.Join(*d.tokenDir, "extraca-"+v.ID)
}

// writeToken is managed as a field to enable easier
// testing
func writeToken(d *Driver, v *volumeInfo) error {
	err := writeDataToFile(d,
		d.getTokenFilePath(v),
		[]byte("access_token = "+v.secret+"\n\n"))

	if err == nil {
		return nil
	}

	return fmt.Errorf("token writing failed: %v", err)
}

func (d *Driver) writeExtraCA(v *volumeInfo) error {
	data, found := v.context["extraca"]
	if !found {
		return fmt.Errorf("writeExtraCA called without any passed")
	}

	err := writeDataToFile(d,
		d.getCAFilePath(v),
		[]byte(data))

	if err == nil {
		return nil
	}

	return fmt.Errorf("extra CA writing failed: %v", err)
}

func writeDataToFile(d *Driver, path string, data []byte) error {
	f, err := os.CreateTemp(*d.tokenDir, "")

	if err != nil {
		return fmt.Errorf("data writing failed; couldn't create "+
			"temporary file: %v", err)
	}

	// os.CreateTemp now sets 0o600 but let's be explicit about it for clarity
	// for reviewers and if anyone should try with a really old version.
	if err := os.Chmod(f.Name(), 0o600); err != nil {
		f.Close() // nolint:errcheck
		return fmt.Errorf("data writing failed; couldn't set secure "+
			"permissions on temporary file: %v", err)
	}

	written := 0
	for written < len(data) {
		n, err := f.Write(data[written:])
		if err != nil {
			return fmt.Errorf("data writing failed; couldn't write "+
				"temporary file contents: %v", err)
		}
		written += n
	}

	err = f.Close()
	if err != nil {
		return fmt.Errorf("data writing failed; error when closing "+
			"temporary file: %v", err)
	}

	err = os.Rename(f.Name(), path)
	if err != nil {
		return fmt.Errorf("data writing failed; couldn't rename "+
			"temporary file to proper name: %v", err)
	}
	return nil
}

func (d *Driver) ensureTargetDir(v *volumeInfo) error {
	return os.MkdirAll(v.path, 0o700)
}

func doMount(d *Driver, v *volumeInfo) error {

	// It seems we might get called again even if we think we have correctly
	// mounted and replied so. As a workaround, we start by checking if the
	// requested path is a mountpoint and decide it's good if that's the case

	if d.isMountPoint(d, v) {
		klog.V(10).Infof("Request for already mounted path at %s, considering good", v.path)
		return nil
	}

	if v.capability != nil && v.capability.GetAccessMode() != nil {
		// If we see an unexpeced access mode, we must fail

		m := v.capability.GetAccessMode().Mode
		if m != csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY &&
			m != csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY {

			return status.Errorf(codes.PermissionDenied, "Only read-only is supported, %s was requested", v.capability.GetAccessMode().String())
		}
	}

	var mountGroup string
	if v.capability != nil && v.capability.GetMount() != nil {
		// Pick up if we are requested to use a certain group for the mount
		// (fsGroup)
		mg := v.capability.GetMount().GetVolumeMountGroup()
		if mg != "" {
			mountGroup = mg
		}
	}

	args := []string{"--open", "--loglevel", "-16",
		"--credentialsfile", d.getTokenFilePath(v)}
	if d.logDir != nil && len(*d.logDir) > 0 {
		logName := path.Join(*d.logDir, v.ID+".log")
		args = append(args, "--log="+logName)
	}

	_, found := v.context["extraca"]
	if found {
		err := d.writeExtraCA(v)
		if err != nil {
			return fmt.Errorf("error while writing extra CAs to %s for volume %s: %w", d.getCAFilePath(v), v.ID, err)
		}

		args = append(args, "--extracafile", d.getCAFilePath(v))
	}

	contextOptions := []string{"chunksize", "cachesize", "rootURL", "maxretries", "owner"}

	if mountGroup != "" {
		// group passed through volume mount - pass that along
		args = append(args, "--group", mountGroup)
	} else {
		// group not passed through volume mount - we can respect group option
		contextOptions = append(contextOptions, "group")
	}

	klog.V(14).Infof("VolumeContext is %v", v.context)
	for _, k := range contextOptions {
		if val, ok := v.context[k]; ok {
			args = append(args, "--"+k, val)
		}
	}

	args = append(args, v.path)

	klog.V(10).Infof("Mounting sdafs at %s", v.path)

	klog.V(14).Infof("Running sda from %s with arguments %v", *d.sdafsPath, args)
	c := exec.Command(*d.sdafsPath, args...)

	err := d.ensureTargetDir(v)
	if err != nil {
		return fmt.Errorf("error while ensuring mount target %s existed: %v", v.path, err)
	}

	// We should try to unmount if asked to
	v.attached = true

	errPipe, err := c.StderrPipe()
	if err != nil {
		return fmt.Errorf("couldn't make stderr pipe for sdafs: %v", err)
	}

	outPipe, err := c.StdoutPipe()
	if err != nil {
		return fmt.Errorf("couldn't make stdout pipe for sdafs: %v", err)
	}

	err = c.Start()
	if err != nil {
		return fmt.Errorf("couldn't start sdafs: %v", err)
	}

	errorMsg, err := io.ReadAll(errPipe)
	if err != nil {
		return fmt.Errorf("couldn't read stderr from sdafs run: %v", err)
	}
	outMsg, err := io.ReadAll(outPipe)
	if err != nil {
		return fmt.Errorf("couldn't read stdout from sdafs run: %v", err)
	}

	err = c.Wait()
	if err != nil {
		klog.V(10).Infof("Output (stdout) from broken sdafs run: %s", outMsg)
		klog.V(10).Infof("Output (stderr) from broken sdafs run: %s", errorMsg)
		return fmt.Errorf("error while running sdafs: %v", err)
	}

	waited := time.Duration(0)

	for !d.isMountPoint(d, v) && waited < d.maxWaitMount {
		time.Sleep(d.waitPeriod)
		waited += d.waitPeriod
	}

	if d.isMountPoint(d, v) {
		klog.V(10).Infof("Filesystem mounted at: %s", v.path)
		return nil
	}
	klog.V(10).Infof("Filesystem wasn't mounted after %v, giving up", d.maxWaitMount)

	return fmt.Errorf("filesystem didn't mount after %v", d.maxWaitMount)

}

func isMountPoint(d *Driver, v *volumeInfo) bool {

	var stat unix.Stat_t
	err := unix.Stat(v.path, &stat)

	if err != nil && errors.Is(err, fs.ErrNotExist) {

		return false
	}

	parent := path.Dir(v.path)
	var pstat unix.Stat_t
	perr := unix.Stat(parent, &pstat)

	if perr != nil {
		// Default to false here, not sure what to respond if it's not a
		// non-exist error
		return false
	}

	if pstat.Dev != stat.Dev {
		return true
	}

	return false
}

func unmount(d *Driver, v *volumeInfo) error {
	// If mountpoint isn't there, not much to do

	var stat unix.Stat_t
	err := unix.Stat(v.path, &stat)

	if err != nil && errors.Is(err, fs.ErrNotExist) {
		klog.V(12).Infof("unmounting skipped for %s as it doesn't exist", v.path)
		v.attached = false
		// Given path doesn't exist
		return nil
	}

	klog.V(10).Infof("unmounting %s", v.path)
	err = unix.Unmount(v.path, unix.MNT_DETACH)

	if err != nil && d.isMountPoint(d, v) {
		// Only fail if we have an actual mount point

		klog.V(10).Infof("unmount of %s failed with %v, giving up", v.path, err)
		return fmt.Errorf("unmount of %s failed: %v giving up", v.path, err)
	}

	v.attached = false

	err = os.Remove(v.path)
	if err != nil {
		return fmt.Errorf("couldn't remove directory for mount %s: %v", v.path, err)
	}

	return nil
}
