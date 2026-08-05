//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	ctrlBreakEvent        = 1
)

var generateConsoleCtrlEvent = syscall.NewLazyDLL("kernel32.dll").NewProc("GenerateConsoleCtrlEvent")

func prepareInteractiveCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func interruptManagedProcess(pid int) error {
	result, _, callErr := generateConsoleCtrlEvent.Call(uintptr(ctrlBreakEvent), uintptr(pid))
	if result == 0 {
		return fmt.Errorf("GenerateConsoleCtrlEvent failed: %v", callErr)
	}
	return nil
}

func forceManagedProcess(pid int) error {
	return exec.Command("taskkill", "/PID", fmt.Sprintf("%d", pid), "/T", "/F").Run()
}
