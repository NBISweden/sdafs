package main

import (
	"flag"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/tj/assert"
)

func TestSdafsDefaultPath(t *testing.T) {
	pathVar := os.Getenv("PATH")
	err := os.Setenv("PATH", "/nosuchdir")
	assert.Equal(t, nil, err, "Unexpected error while cleaning environment")
	p := sdafsPathDefault()
	err = os.Setenv("PATH", pathVar)
	assert.Equal(t, nil, err, "Unexpected error while restoring environment")

	assert.Equal(t, "/sdafs", p, "Unexpected return from default sdafs check")
}

func TestBadFlags(t *testing.T) {
	if os.Getenv("BE_CRASHER") == "1" {
		os.Args = []string{"binary", "-unknown"}
		flag.CommandLine = flag.NewFlagSet("test", flag.ExitOnError)

		getConfig()

		return
	}

	runExiting(t, "TestBadFlags", true)
}

func TestHelpFlag(t *testing.T) {
	if os.Getenv("BE_CRASHER") == "1" {
		os.Args = []string{"binary", "-help"}
		flag.CommandLine = flag.NewFlagSet("test", flag.ExitOnError)

		getConfig()

		return
	}

	runExiting(t, "TestHelpFlag", false)
}

func TestGoodFlags(t *testing.T) {
	os.Args = []string{"binary"}
	flag.CommandLine = flag.NewFlagSet("test", flag.ExitOnError)

	getConfig()
}

func runExiting(t *testing.T, testName string, fail bool) {
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run="+testName)
	stdoutPipe, err := cmd.StdoutPipe()
	assert.Equal(t, nil, err, "Error while creating stdout pipe")

	stderrPipe, err := cmd.StderrPipe()
	assert.Equal(t, nil, err, "Error while creating stderr pipe")

	cmd.Env = append(os.Environ(), "BE_CRASHER=1")
	err = cmd.Start()
	assert.Equal(t, nil, err, "Error while starting test")

	stdout, err := io.ReadAll(stdoutPipe)
	assert.Equal(t, nil, err, "Error while reading stdout pipe")
	stderr, err := io.ReadAll(stderrPipe)
	assert.Equal(t, nil, err, "Error while reading stderr pipe")

	t.Logf("Result of call test %s\n\n%s\n\n%s", testName, stdout, stderr)

	err = cmd.Wait()

	if !fail && err == nil {
		// We don't want a failure and we didn't get one
		return
	}

	if e, ok := err.(*exec.ExitError); fail && ok && !e.Success() {
		return
	}

	if fail {
		t.Fatalf("process succeeded when it should not for test %s"+
			" %v, want exit status 1",
			testName,
			err)
	}

	t.Fatalf("process failed when it should succeed %s"+
		" %v, want exit status 0",
		testName,
		err)

}
