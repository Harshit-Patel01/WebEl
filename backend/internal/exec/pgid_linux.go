//go:build linux

package exec

import (
	osexec "os/exec"
	"syscall"
)

func setPgid(cmd *osexec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree signals the whole process group led by pgid. setPgid makes the child a
// group leader, so a negative pid reaches its descendants too — plain context
// cancellation only reaches the direct child and leaks builds like npm/lxc/pip.
func killTree(pgid int) error {
	if pgid <= 1 {
		return syscall.ESRCH
	}
	// SIGTERM first so cleanup handlers get a chance; caller escalates if needed.
	return syscall.Kill(-pgid, syscall.SIGTERM)
}
