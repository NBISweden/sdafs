package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"testing"

	"github.com/tj/assert"
)

// The handling of checking things that should exit is based on
// https://stackoverflow.com/a/33404435

func TestConfOptionNoMountPoint(t *testing.T) {
	if os.Getenv("BE_CRASHER") == "1" {
		getConfigs()
		return
	}

	runExiting(t, "TestConfOptionNoMountPoint")
}

func TestBadChunkSize(t *testing.T) {
	if os.Getenv("BE_CRASHER") == "1" {
		os.Args = []string{"binary", "-chunksize", "30", "mountpoint"}
		flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
		flag.Parse()
		getConfigs()
		return
	}

	runExiting(t, "TestBadChunkSize")
}

func TestBadMaxRetries(t *testing.T) {
	if os.Getenv("BE_CRASHER") == "1" {
		os.Args = []string{"binary", "-maxretries", "90", "mountpoint"}
		flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
		flag.Parse()
		getConfigs()
		return
	}

	runExiting(t, "TestBadMaxRetries")
}

func TestBadUID(t *testing.T) {
	if os.Getenv("BE_CRASHER") == "1" {
		os.Args = []string{"binary", "-owner", "9000000000", "mountpoint"}
		flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
		flag.Parse()
		getConfigs()
		return
	}

	runExiting(t, "TestBadUID")
}

func TestBadGID(t *testing.T) {
	if os.Getenv("BE_CRASHER") == "1" {
		os.Args = []string{"binary", "-group", "80000000000", "mountpoint"}
		flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
		flag.Parse()
		getConfigs()
		return
	}

	runExiting(t, "TestBadGID")
}

func TestConfOptions(t *testing.T) {
	safeArgs := os.Args

	os.Args = []string{"binary", "mountpoint"}
	flag.Parse()
	c := getConfigs()

	assert.Equal(t, "mountpoint", c.mountPoint,
		"Didn't pick up expected mountpoint")

	assert.Equal(t, false, c.foreground,
		"Not default value of foreground as expected")

	assert.Equal(t, false, c.sdafsconf.SpecifyGID,
		"group wasn't passed but SpecifyGID is set")
	assert.Equal(t, false, c.sdafsconf.SpecifyUID,
		"owner wasn't passed but SpecifyGID is set")

	os.Args = []string{"binary", "-log", "somelog", "-foreground", "mount2"}
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	flag.Parse()
	c = getConfigs()

	assert.Equal(t, "mount2", c.mountPoint,
		"Didn't pick up expected mountpoint")

	assert.Equal(t, true, c.foreground,
		"Not value of foreground as expected")

	assert.Equal(t, "somelog", c.logFile,
		"Not value of logfile as expected")

	os.Args = []string{"binary", "mount3"}
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	flag.Parse()
	c = getConfigs()

	assert.Equal(t, "mount3", c.mountPoint,
		"Didn't pick up expected mountpoint")

	assert.Equal(t, false, c.foreground,
		"Not value of foreground as expected")

	assert.Equal(t, "sdafs.log", c.logFile,
		"Not value of logfile as expected")

	os.Args = []string{"binary", "-foreground", "mount4"}
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	flag.Parse()
	c = getConfigs()

	assert.Equal(t, "mount4", c.mountPoint,
		"Didn't pick up expected mountpoint")

	assert.Equal(t, true, c.foreground,
		"Not value of foreground as expected")

	assert.Equal(t, "", c.logFile,
		"Not value of logfile as expected")

	assert.Equal(t, false, c.sdafsconf.SpecifyDirPerms,
		"Not default value for dirperms as expected")

	assert.Equal(t, false, c.sdafsconf.SpecifyFilePerms,
		"Not default value for fileperms as expected")

	os.Args = []string{"binary", "-open", "mount5"}
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	flag.Parse()
	c = getConfigs()

	assert.Equal(t, "mount5", c.mountPoint,
		"Didn't pick up expected mountpoint")

	assert.Equal(t, true, c.sdafsconf.SpecifyDirPerms,
		"Not default value for dirperms as expected")

	assert.Equal(t, true, c.sdafsconf.SpecifyFilePerms,
		"Not default value for fileperms as expected")

	assert.Equal(t, os.FileMode(0555), c.sdafsconf.DirPerms,
		"Not default value for dirperms as expected")

	assert.Equal(t, os.FileMode(0444), c.sdafsconf.FilePerms,
		"Not default value for fileperms as expected")

	defaultCacheWithMem := c.sdafsconf.CacheSize

	os.Args = []string{"binary", "-cachesize", "10", "mount6"}
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	flag.Parse()
	c = getConfigs()

	assert.Equal(t, "mount6", c.mountPoint,
		"Didn't pick up expected mountpoint")

	assert.Equal(t, uint64(10*1024*1024), c.sdafsconf.CacheSize,
		"Did not see expected cache size")

	os.Args = []string{"binary", "-cachemempercent", "90", "mount7"}
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	flag.Parse()
	c = getConfigs()

	assert.Equal(t, "mount7", c.mountPoint,
		"Didn't pick up expected mountpoint")

	assert.Less(t, uint64(0), c.sdafsconf.CacheSize,
		"Cache size not picked up from memory as expected ")

	assert.Greater(t, c.sdafsconf.CacheSize, defaultCacheWithMem,
		"Cache with 90% of RAM not larger than with default (8%)")

	os.Args = []string{"binary", "-owner", "20", "mount8"}
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	flag.Parse()
	c = getConfigs()
	assert.Equal(t, true, c.sdafsconf.SpecifyUID,
		"owner passed but not reflected in sdafs config")
	assert.Equal(t, uint32(20), c.sdafsconf.UID,
		"unexpected uid in sdafs config")

	os.Args = []string{"binary", "-group", "30", "mount8"}
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	flag.Parse()
	c = getConfigs()
	assert.Equal(t, true, c.sdafsconf.SpecifyGID,
		"group passed but not reflected in sdafs config")
	assert.Equal(t, uint32(30), c.sdafsconf.GID,
		"unexpected gid in sdafs config")

	os.Args = safeArgs

}

func TestRepoint(t *testing.T) {

	tmpdir, err := os.MkdirTemp("", "logdir")
	assert.Nil(t, err, "Couldn't make temporary log dir")
	fileName := fmt.Sprintf("%s/sdafs.log", tmpdir)

	m := mainConfig{logFile: fileName}
	repointLog(m)
	st, err := os.Stat(fileName)

	// Allow releasing our file reference
	log.SetOutput(os.Stdout)

	assert.Nil(t, err, "Log isn't there")
	assert.False(t, st.IsDir(), "Logfile is a directory")

	err = os.RemoveAll(tmpdir)
	assert.Nil(t, err, "Cleanup failed")
}

func runExiting(t *testing.T, testName string) {
	cmd := exec.Command(os.Args[0], "-test.run="+testName)
	cmd.Env = append(os.Environ(), "BE_CRASHER=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}

	t.Fatalf("process succeeded when it should not for test %s"+
		" %v, want exit status 1",
		testName,
		err)

}

func TestRepointFail(t *testing.T) {

	// https://stackoverflow.com/a/33404435
	if os.Getenv("BE_CRASHER") == "1" {
		m := mainConfig{logFile: "/doesntexist/somefile"}
		repointLog(m)
		return
	}
	runExiting(t, "TestRepointFail")
}
