package csidriver

import (
	"fmt"
	"io/fs"
	"net"
	"os"
	"strings"
)

func CheckSocket(s *string) (bool, error) {

	network := "unix"
	var address string

	endpointParts := strings.Split(*s, ":")

	if len(endpointParts) > 1 {
		network = endpointParts[0]
		address = endpointParts[1]
	} else {
		address = endpointParts[0]
	}

	if network != "unix" {
		return true, nil
	}

	sockStat, err := os.Stat(address)

	if err != nil {
		// Treat any error for stat as non-existant and fine
		return true, nil
	}

	if fs.ModeSocket == (sockStat.Mode() & fs.ModeSocket) {
		// Socket
		_, err = net.Dial("unix", address)
		if err == nil {
			// Something working, don't start a new instance
			return false, nil
		}
	} else {
		return false, fmt.Errorf("%s exists but is not a leftover socket", address)
	}

	// If we get here, there's a socket that doesn't work, so we remove that

	err = os.Remove(address)
	if err != nil {
		return false, fmt.Errorf("couldn't remove stale endpoint %s: %v", address, err)
	}

	return true, nil

}
