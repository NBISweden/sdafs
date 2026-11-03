//go:build !linux

package main

func canUnshare() bool {
	return false
}

func reExecNewNS() error {
	return nil
}

func reExecTool(m *main) error {
	return nil
}
