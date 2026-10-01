//go:build !linux

package exec

import osexec "os/exec"

func setPgid(cmd *osexec.Cmd) {
	// No-op on non-Linux platforms
}

func killTree(pgid int) error {
	// No process groups to signal here; context cancellation is the only lever.
	return nil
}
