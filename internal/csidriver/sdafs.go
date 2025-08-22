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

	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"
)

const maxWaitMount = 60 * time.Second
const waitPeriod = 10 * time.Millisecond

func (d *Driver) getTokenfilePath(v *volumeInfo) string {
	return path.Join(*d.tokenDir, "token-"+v.ID)
}

func (d *Driver) writeToken(v *volumeInfo) error {

	f, err := os.CreateTemp(*d.tokenDir, "")

	if err != nil {
		return fmt.Errorf("token writing failed; couldn't create temporary file: %v", err)
	}

	_, err = f.WriteString("access_token = " + v.secret + "\n\n")
	if err != nil {
		return fmt.Errorf("token writing failed; couldn't write temporary file contents: %v", err)
	}

	err = os.Rename(f.Name(), d.getTokenfilePath(v))
	if err != nil {
		return fmt.Errorf("token writing failed; couldn't rename temporary file to proper name: %v", err)
	}
	return nil
}

func (d *Driver) ensureTargetDir(v *volumeInfo) error {
	return os.MkdirAll(v.path, 0o700)
}

func (d *Driver) doMount(v *volumeInfo) error {

	// It seems we might get called again even if we think we have correctly
	// mounted and replied so. As a workaround, we start by checking if the
	// requested path is a mountpoint and decide it's good if that's the case

	if d.isMountPoint(v) {
		klog.V(10).Infof("Request for already mounted path at %s, considering good", v.path)
		return nil
	}

	args := []string{"--open", "--loglevel", "-16",
		"--credentialsfile", d.getTokenfilePath(v)}
	if d.logDir != nil && len(*d.logDir) > 0 {
		logName := path.Join(*d.logDir, v.ID+".log")
		args = append(args, "--log="+logName)
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
		klog.V(10).Infof("Output (sterr) from broken sdafs run: %s", errorMsg)
		return fmt.Errorf("error while running sdafs: %v", err)
	}

	waited := 0 * waitPeriod

	for !d.isMountPoint(v) && waited < maxWaitMount {
		time.Sleep(waitPeriod)
		waited += waitPeriod
	}

	if d.isMountPoint(v) {
		klog.V(10).Infof("Filesystem mounted at : %s", v.path)
		return nil
	}
	klog.V(10).Infof("Filesystem wasn't mounted after %v, giving up", maxWaitMount)

	return fmt.Errorf("filesystem didn't mount after %v", maxWaitMount)

}

func (d *Driver) isMountPoint(v *volumeInfo) bool {

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

func (d *Driver) unmount(v *volumeInfo) error {
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

	if err != nil && d.isMountPoint(v) {
		// Only fail if we have an actual mount point

		// Only fail here if we have an actual mount point
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
