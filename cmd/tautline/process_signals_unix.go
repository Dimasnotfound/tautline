//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

func prepareInteractiveCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func interruptManagedProcess(pid int) error {
	return syscall.Kill(-pid, syscall.SIGINT)
}

func forceManagedProcess(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}
