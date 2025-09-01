package csidriver

import (
	"net"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/tj/assert"
)

func makeTempPath() string {
	return "/tmp/testsocket" + uuid.New().String()
}

func TestCheckSocket(t *testing.T) {

	s := "else:/doesnotexist"
	// Not unix,
	cont, err := checkSocket(&s)
	assert.Equal(t, true, cont, "checkSocket shouldn't judge this")
	assert.Equal(t, nil, err, "Unexpected error from checkSocket")

	s = "unix:/doesnotexist"
	// Explicit unix, but socket doesn't exist
	cont, err = checkSocket(&s)
	assert.Equal(t, true, cont, "checkSocket shouldn't judge this")
	assert.Equal(t, nil, err, "Unexpected error from checkSocket")

	s = "/doesnotexist"
	// Implicit unix, but socket doesn't exist
	cont, err = checkSocket(&s)
	assert.Equal(t, true, cont, "checkSocket shouldn't judge this")
	assert.Equal(t, nil, err, "Unexpected error from checkSocket")

	s = "unix:/dev"
	// Implicit unix, socket path exists but isn't sensible (not socket)
	cont, err = checkSocket(&s)
	assert.Equal(t, false, cont, "this shouldn't work")
	assert.Error(t, err, "Missing expected error from checkSocket")

	// Actual abandoned socket path
	// FIXME: This seems actually broken/doesn't test what it should,
	// it seems Close will also cause the socket to be deleted somehow
	socketPath := makeTempPath()
	listener, err := net.Listen("unix", socketPath)
	assert.Equal(t, nil, err, "Unexpected error from Listen")
	listener.Close() // nolint:errcheck

	defer os.Remove(socketPath) // nolint:errcheck
	cont, err = checkSocket(&socketPath)
	assert.Equal(t, true, cont, "this should be ok")
	assert.Equal(t, nil, err, "Unexpected error from checkSocket")

	// Again, but keep something listening
	socketPath = makeTempPath()
	listener, err = net.Listen("unix", socketPath)
	assert.Equal(t, nil, err, "Unexpected error from net Listen")

	go serviceListener(t, listener)

	defer os.Remove(socketPath) // nolint:errcheck
	cont, err = checkSocket(&socketPath)
	assert.Equal(t, false, cont, "checkSocket with live service should not go on")
	assert.Equal(t, nil, err, "Unexpected error from checkSocket")
}

func serviceListener(t *testing.T, l net.Listener) {
	l.Accept() // nolint:errcheck
	l.Close()  // nolint:errcheck
}
