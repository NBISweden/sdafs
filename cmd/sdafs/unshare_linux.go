//go:build linux

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/jacobsa/fuse"
)

const REEXEC_MARKER = "SDAFS_REEXEC"

func canUnshare() bool {
	return true
}

func isReExec() bool {
	return os.Getenv(REEXEC_MARKER) != ""
}

func getUidGidSlice() ([]uint32, error) {
	s := os.Getenv(REEXEC_MARKER)
	if s == "" {
		return nil, fmt.Errorf("%s unexpectedly empty", REEXEC_MARKER)
	}

	numstrs := strings.Split(s, ":")

	if len(numstrs) < 2 {
		return nil, fmt.Errorf("marker format doesn't match expectations")
	}

	var nums []uint32

	for i := range numstrs {

		n, err := strconv.ParseUint(numstrs[i], 10, 32)
		if err != nil {
			return nums, err
		}

		nums = append(nums, uint32(n))
	}

	return nums, nil
}

func getUid() (uint32, error) {
	nums, err := getUidGidSlice()
	if err != nil {
		return 0, fmt.Errorf("bad info in %s: %w", REEXEC_MARKER, err)
	}

	return nums[0], nil
}

// getGid
func getGid() (uint32, error) {
	nums, err := getUidGidSlice()
	if err != nil {
		return 0, fmt.Errorf("bad info in %s: %w", REEXEC_MARKER, err)
	}

	return nums[1], nil
}

func reExecNewNS() error {

	myuid := os.Getuid()
	mygid := os.Getgid()

	err := os.Setenv(REEXEC_MARKER, fmt.Sprintf("%d:%d",
		myuid,
		mygid))

	if err != nil {
		return fmt.Errorf("couldn't set re-exec marker in environment: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("couldn't find path to myself: %w", err)
	}

	cmd := exec.Command(exe,
		os.Args[1:]...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Unshareflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWUSER,

		UidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Getuid(), Size: 1},
		},
		GidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Getgid(), Size: 1},
		},
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error while reexecing for unshare: %w", err)
	}

	// We need to report the exit status as our own

	if cmd.ProcessState == nil {
		return fmt.Errorf("process state is nil afted successful run?")
	}

	os.Exit(cmd.ProcessState.ExitCode())
	return nil
}

func reExecRun(m *mainConfig) error {

	cmd := exec.Command(m.run)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	realUid, err := getUid()
	if err != nil {
		return fmt.Errorf("error while deriving correct uid: %w", err)
	}

	realGid, err := getGid()
	if err != nil {
		return fmt.Errorf("error while deriving correct gid: %w", err)
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Unshareflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWUSER | syscall.CLONE_FILES,

		UidMappings: []syscall.SysProcIDMap{
			{ContainerID: int(realUid), HostID: os.Getuid(), Size: 1},
		},
		GidMappings: []syscall.SysProcIDMap{
			{ContainerID: int(realGid), HostID: os.Getgid(), Size: 1},
		},

		Credential: &syscall.Credential{
			Uid: realUid,
			Gid: realGid,
		},
	}

	// Remove our marker
	err = os.Unsetenv(REEXEC_MARKER)
	if err != nil {
		return fmt.Errorf(
			"couldn't remove reexec marker from environment: %w",
			err)
	}

	if err := cmd.Run(); err != nil {
		umountErr := fuse.Unmount(m.mountPoint)
		if umountErr != nil {
			/// Just flag this error
			log.Printf(
				"error while unmounting for cleanup after failure: %v",
				umountErr)
		}

		return fmt.Errorf("error while reexecing for run: %w", err)
	}

	//
	err = fuse.Unmount(m.mountPoint)
	if err != nil {
		return fmt.Errorf("couldn't unmount after run: %w", err)
	}

	// We need to report the exit status as our own
	if cmd.ProcessState == nil {
		return fmt.Errorf("process state is nil afted successful run?")
	}

	os.Exit(cmd.ProcessState.ExitCode())
	return nil
}
