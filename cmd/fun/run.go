package main

// fun run：热更新开发循环——改代码自动重编译重启。
// 纯标准库实现：轮询 mtime 检测变更（400ms），先编译成功再切换进程，
// 编译失败保留旧进程继续跑

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

func cmdRun(args []string) {
	pkg := "."
	var childArgs []string
	for i := 0; i < len(args); i++ {
		if args[i] == "-p" && i+1 < len(args) {
			pkg = args[i+1]
			i++
			continue
		}
		if args[i] == "--" {
			childArgs = append(childArgs, args[i+1:]...)
			break
		}
		if strings.HasPrefix(args[i], "-p") && len(args[i]) > 2 {
			pkg = args[i][2:]
			continue
		}
		childArgs = append(childArgs, args[i])
	}

	binPath := filepath.Join(os.TempDir(), fmt.Sprintf("fun-run-%d", os.Getpid()))

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	var child *exec.Cmd
	var childDone chan struct{}
	restart := make(chan struct{}, 1)

	// 初始编译
	if !build(pkg, binPath) {
		fmt.Println("fun: 初始编译失败，等待文件变更后重试")
	} else {
		child, childDone = spawn(binPath, childArgs)
	}

	go watchLoop(pkg, restart)

	for {
		select {
		case <-sigs:
			fmt.Println("\nfun: 退出")
			killChild(child, childDone)
			return
		case <-restart:
			fmt.Println("fun: 检测到变更，重新编译...")
			if !build(pkg, binPath) {
				fmt.Println("fun: 编译失败，旧进程继续运行")
				continue
			}
			killChild(child, childDone)
			child, childDone = spawn(binPath, childArgs)
		}
	}
}

func build(pkg, binPath string) bool {
	cmd := goCommand("build", pkg, "-o", binPath)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run() == nil
}

func spawn(binPath string, args []string) (*exec.Cmd, chan struct{}) {
	cmd := exec.Command(binPath, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// 独立进程组：杀父进程时连带子进程（业务可能 fork 了 worker）
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	done := make(chan struct{})
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "fun: 启动失败:", err)
		close(done)
		return nil, done
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			fmt.Fprintf(os.Stderr, "fun: 进程退出: %v\n", err)
		}
		close(done)
	}()
	return cmd, done
}

func killChild(child *exec.Cmd, done chan struct{}) {
	if child == nil || child.Process == nil {
		return
	}
	syscall.Kill(-child.Process.Pid, syscall.SIGTERM)
	if done == nil {
		done = make(chan struct{})
		go func() { child.Wait(); close(done) }()
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
	}
}

var skipDirs = map[string]bool{
	".git": true, "vendor": true, "node_modules": true, ".fun": true,
	"gen": true, "testdata": true,
}

// snapshot 收集监控文件的 (路径, mtime)
func snapshot(root string) map[string]int64 {
	out := map[string]int64{}
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && skipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(name)
		if ext == ".go" || name == "go.mod" || name == "go.sum" {
			// 元数据采集的临时辅助文件在模块内一闪而过，忽略避免自触发
			if name == helperTestFile {
				return nil
			}
			if info, err := d.Info(); err == nil {
				out[path] = info.ModTime().UnixNano()
			}
		}
		return nil
	})
	return out
}

func diffSnapshot(a, b map[string]int64) bool {
	if len(a) != len(b) {
		return true
	}
	for k, v := range a {
		if b[k] != v {
			return true
		}
	}
	return false
}

func watchLoop(root string, changed chan struct{}) {
	last := snapshot(root)
	var pending bool
	var pendingAt time.Time
	for {
		time.Sleep(400 * time.Millisecond)
		cur := snapshot(root)
		if !diffSnapshot(last, cur) {
			// 静默 300ms 后触发（编辑器分多次写盘时合并）
			if pending && time.Since(pendingAt) > 300*time.Millisecond {
				pending = false
				select {
				case changed <- struct{}{}:
				default:
				}
			}
			last = cur
			continue
		}
		last = cur
		if !pending {
			pending = true
			pendingAt = time.Now()
		} else {
			pendingAt = time.Now()
		}
	}
}

// sortedKeys 调试用（保留给未来扩展）
func sortedKeys(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
