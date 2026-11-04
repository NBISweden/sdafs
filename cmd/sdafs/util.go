package main

import (
	"fmt"
	"os"
)

func writeString(s string, f *os.File) error {
	for i := 0; i < len(s); {

		n, err := f.WriteString(s[i:])

		i += n
		if err != nil {
			return fmt.Errorf("error while writing string: %w", err)
		}
	}

	return nil
}
