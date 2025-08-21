package csidriver

import (
	"fmt"
	"io/fs"
	"net"
	"os"
	"strings"
	"time"
)

func CheckSocket(s *string) (bool, error) {

	endpointParts := strings.Split(*s, ":")
	if endpointParts[0] != "unix" {
		return true, nil
	}

	sockStat, err := os.Stat(endpointParts[1])

	if err != nil {
		// Treat any error for stat as non-existant and fine
		return true, nil
	}

	if fs.ModeSocket == (sockStat.Mode() & fs.ModeSocket) {
		// Socket
		_, err = net.Dial("unix", endpointParts[1])
		if err == nil {
			// Something working, don't start a new instance
			return false, nil
		}
	} else {
		return false, fmt.Errorf("%s exists but is not a leftover socker", endpointParts[1])
	}

	// If we get here, there's a socket that doesn't work, so we remove that

	err = os.Remove(endpointParts[1])
	if err != nil {
		return false, fmt.Errorf("Couldn't remove stale endpoint %s: %v", endpointParts[1], err)
	}

	// FIXME: Better way?
	// Give kubernetes a chance to pick up that the old plugin is gone by waiting somewhat after removing the socket
	// not sure why it's needed...

	time.Sleep(1 * time.Second)

	return true, nil

}
