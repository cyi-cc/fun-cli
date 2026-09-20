//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// setProcGroup Unix：子进程独立进程组，便于整组终止。
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// termProcGroup 向进程组发送 SIGTERM。
func termProcGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
}

// killProcGroup 向进程组发送 SIGKILL。
func killProcGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
