//go:build !linux

package main

func canUnshare() bool {
	return false
}

func reExecNewNS() error {
	return nil
}

func reExecRun(m *main) error {
	return nil
}
