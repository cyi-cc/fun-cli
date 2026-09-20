//go:build windows

package main

import (
	"os/exec"
)

// setProcGroup Windows：无进程组概念，空操作。
func setProcGroup(cmd *exec.Cmd) {}

// termProcGroup Windows：taskkill 整棵进程树强制终止（无优雅信号）。
func termProcGroup(pid int) {
	_ = exec.Command("taskkill", "/T", "/F", "/PID", itoaPid(pid)).Run()
}

// killProcGroup 与 termProcGroup 相同。
func killProcGroup(pid int) {
	termProcGroup(pid)
}

func itoaPid(pid int) string {
	if pid == 0 {
		return "0"
	}
	neg := pid < 0
	if neg {
		pid = -pid
	}
	var buf [20]byte
	i := len(buf)
	for pid > 0 {
		i--
		buf[i] = byte('0' + pid%10)
		pid /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
