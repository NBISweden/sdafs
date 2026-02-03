package csidriver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"time"

	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"
)

// loadPersisted reads in information about persisted mounts and
// refreshes them
func (d *Driver) loadPersisted() error {

	// Not set - no persistence so nothing to do
	if d.persistDir == nil || len(*d.persistDir) == 0 {
		return nil
	}

	_, err := os.Stat(*d.persistDir)
	if os.IsNotExist(err) {
		return fmt.Errorf(
			"error while checking existence of persistence dir: %v", err)
	}

	dents, err := os.ReadDir(*d.persistDir)
	if err != nil {
		return fmt.Errorf("error while reading persistence dir: %v", err)
	}

	for _, dent := range dents {
		err := d.loadOnePersist(path.Join(*d.persistDir, dent.Name()))

		if err != nil {
			klog.V(4).Infof("error while restoring persisted mounts: %v", err)
		}
	}

	return nil
}

// loadPersist
func (d *Driver) loadOnePersist(s string) error {
	f, err := os.Open(s)

	if err != nil {
		return fmt.Errorf(
			"error while opening persistent information (%s) for read: %v",
			s, err)
	}

	defer f.Close() // nolint:errcheck

	jsonData, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf(
			"error while reading persistent information from %s: %v",
			s, err)
	}

	var v volumeInfo
	err = json.Unmarshal(jsonData, &v)

	if err != nil {
		return fmt.Errorf(
			"failed to unmarshal persistent information from %s: %v",
			s, err)
	}

	d.volumes[v.ID] = &v

	_ = unix.Unmount(v.Path, unix.MNT_DETACH) // nolint:errcheck

	err = d.writeToken(d, &v)
	if err != nil {
		klog.V(10).Infof(
			"couldn't write token secret for persisted mount: %v", err)

		return fmt.Errorf("couldn't write token secret for persisted mount: %s", err)
	}

	err = d.mounter(d, &v)

	if err != nil {
		klog.V(10).Infof(
			"couldn't restore persisted mount: %v", err)

		return fmt.Errorf("couldn't restore persisted mount: %s", err)
	}

	return nil
}

func (d *Driver) writePersisted(v *volumeInfo) error {
	// Not set - no persistence so nothing to do
	if d.persistDir == nil || len(*d.persistDir) == 0 {
		return nil
	}

	p := path.Join(*d.persistDir, v.ID)
	f, err := os.Create(p)

	if err != nil {
		return fmt.Errorf(
			"error while creating persistent information (%s): %v",
			p, err)
	}

	defer f.Close() // nolint:errcheck

	jsonData, err := json.Marshal(v)
	for len(jsonData) > 0 {
		n, err := f.Write(jsonData)
		if err != nil {
			return fmt.Errorf(
				"error while writing persistent information (%s): %v",
				p, err)
		}

		jsonData = jsonData[n:]
	}

	return nil
}

func (d *Driver) unPersist(v *volumeInfo) {
	// Not set - no persistence so nothing to do
	if d.persistDir == nil || len(*d.persistDir) == 0 {
		return
	}

	p := path.Join(*d.persistDir, v.ID)
	_ = os.Remove(p) // nolint:errcheck
	return
}

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
		[]byte("access_token = "+v.Secret+"\n\n"))

	if err == nil {
		return nil
	}

	return fmt.Errorf("token writing failed: %v", err)
}

func (d *Driver) writeExtraCA(v *volumeInfo) error {
	data, found := v.Context["extraca"]
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
	return os.MkdirAll(v.Path, 0o700)
}

func doMount(d *Driver, v *volumeInfo) error {

	// It seems we might get called again even if we think we have correctly
	// mounted and replied so. As a workaround, we start by checking if the
	// requested path is a mountpoint and decide it's good if that's the case

	if d.isMountPoint(d, v) {
		klog.V(10).Infof("Request for already mounted path at %s, considering good", v.Path)
		return nil
	}

	args := []string{"--open", "--loglevel", "-16",
		"--credentialsfile", d.getTokenFilePath(v)}
	if d.logDir != nil && len(*d.logDir) > 0 {
		logName := path.Join(*d.logDir, v.ID+".log")
		args = append(args, "--log="+logName)
	}

	_, found := v.Context["extraca"]
	if found {
		err := d.writeExtraCA(v)
		if err != nil {
			return fmt.Errorf("error while writing extra CAs to %s for volume %s: %w", d.getCAFilePath(v), v.ID, err)
		}

		args = append(args, "--extracafile", d.getCAFilePath(v))
	}

	contextOptions := []string{"chunksize", "cachesize", "rootURL", "maxretries", "owner", "cachettl"}

	if v.Group != "" {
		// group passed through volume mount - pass that along
		args = append(args, "--group", v.Group)
	} else {
		// group not passed through volume mount - we can respect group option
		contextOptions = append(contextOptions, "group")
	}

	klog.V(14).Infof("VolumeContext is %v", v.Context)
	for _, k := range contextOptions {
		if val, ok := v.Context[k]; ok {
			args = append(args, "--"+k, val)
		}
	}

	args = append(args, v.Path)

	klog.V(10).Infof("Mounting sdafs at %s", v.Path)

	klog.V(14).Infof("Running sda from %s with arguments %v", *d.sdafsPath, args)
	c := exec.Command(*d.sdafsPath, args...)

	err := d.ensureTargetDir(v)
	if err != nil {
		return fmt.Errorf("error while ensuring mount target %s existed: %v", v.Path, err)
	}

	// We should try to unmount if asked to
	v.Attached = true

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
		klog.V(10).Infof("Filesystem mounted at: %s", v.Path)
		return nil
	}
	klog.V(10).Infof("Filesystem wasn't mounted after %v, giving up", d.maxWaitMount)

	return fmt.Errorf("filesystem didn't mount after %v", d.maxWaitMount)

}

func isMountPoint(d *Driver, v *volumeInfo) bool {

	var stat unix.Stat_t
	err := unix.Stat(v.Path, &stat)

	if err != nil && errors.Is(err, fs.ErrNotExist) {

		return false
	}

	parent := path.Dir(v.Path)
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
	err := unix.Stat(v.Path, &stat)

	if err != nil && errors.Is(err, fs.ErrNotExist) {
		klog.V(12).Infof("unmounting skipped for %s as it doesn't exist", v.Path)
		v.Attached = false
		// Given path doesn't exist
		return nil
	}

	klog.V(10).Infof("unmounting %s", v.Path)
	err = unix.Unmount(v.Path, unix.MNT_DETACH)

	if err != nil && d.isMountPoint(d, v) {
		// Only fail if we have an actual mount point

		klog.V(10).Infof("unmount of %s failed with %v, giving up", v.Path, err)
		return fmt.Errorf("unmount of %s failed: %v giving up", v.Path, err)
	}

	v.Attached = false

	err = os.Remove(v.Path)
	if err != nil {
		return fmt.Errorf("couldn't remove directory for mount %s: %v", v.Path, err)
	}

	d.unPersist(v)
	return nil
}
