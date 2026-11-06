//go:build !linux

package main

func canUnshare() bool {
	return false
}

func isReExec() bool {
	return false
}

func reExecNewNS() error {
	return nil
}

func reExecRun(m *mainConfig) error {
	return nil
}
