package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/NBISweden/sdafs/internal/cgofuseadapter"
	"github.com/NBISweden/sdafs/internal/sdafs"
)

func detachAndMount(c mainConfig, fs *sdafs.SDAfs) (cgofuseadapter.MountedFS, error) {
	errpipe, err := detachIfNeeded(c)

	pipelock := sync.Mutex{}

	if err != nil {
		return nil, fmt.Errorf("error while detaching: %w", err)
	}

	if c.cgofuse && errpipe != nil {
		// cgofuse will block and we need special handling to deal with
		go func() {
			// Give a chance to run into error, then signal success
			time.Sleep(5 * time.Second)

			pipelock.Lock()
			defer pipelock.Unlock()

			if errpipe == nil {
				// Already sent what we want?
				return
			}

			err := writeString(GOOD_MOUNT_MARKER, errpipe)
			// If we get here and this fails, it should not be fatal but we can
			// log it
			if err != nil {
				log.Printf("Failed to write to interprocess communication: %v", err)
			}

			err = errpipe.Close()
			if err != nil {
				log.Printf("error while closing interprocess communication pipe: %v", err)
			}

			// Signal to normal processing that we've sent a message
			errpipe = nil
		}()
	}

	mount, err := doMount(c, fs)

	pipemsg := GOOD_MOUNT_MARKER
	if err != nil {
		pipemsg = fmt.Sprintf("error while mounting: %v\n", err)
	}

	pipelock.Lock()
	defer pipelock.Unlock()

	if errpipe != nil {
		err := writeString(pipemsg, errpipe)
		if err != nil {
			return nil, fmt.Errorf("failed to write string to interprocess communication: %w", err)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("error while mounting: %w", err)
	}

	// Good case here

	if errpipe != nil {
		// an error here shouldn't be fatal but we can log
		err := errpipe.Close()

		if err != nil {
			log.Printf("failed to close interprocess communication: %v", err)
		}
		errpipe = nil
	}

	return mount, nil
}
