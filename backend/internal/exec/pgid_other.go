//go:build !linux

package exec

import osexec "os/exec"

func setPgid(cmd *osexec.Cmd) {
	// No-op on non-Linux platforms
}
