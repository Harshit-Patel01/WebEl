//go:build linux

package exec

import (
	osexec "os/exec"
	"syscall"
)

func setPgid(cmd *osexec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
