package main

// fun gen：从运行中的服务采集元数据，渲染 Go/TS 客户端代码

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// goCommand 组装 go run/build 命令：-p 为目录时切换工作目录，
// 否则视为当前模块内的包路径（如 ./cmd/server）
func goCommand(subcmd string, pkg string, extra ...string) *exec.Cmd {
	args := append([]string{subcmd}, extra...)
	if st, err := os.Stat(pkg); err == nil && st.IsDir() {
		args = append(args, ".")
		cmd := exec.Command("go", args...)
		cmd.Dir = pkg
		return cmd
	}
	args = append(args, pkg)
	return exec.Command("go", args...)
}

// collectMeta 采集元数据：优先 -from 文件，否则 FUN_DUMP=<tmp> go run <pkg>
func collectMeta(pkg string, from string) (*FunMeta, error) {
	if from != "" {
		return loadMeta(from)
	}
	tmp, err := os.CreateTemp("", "fun-meta-*.json")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	cmd := goCommand("run", pkg)
	cmd.Env = append(os.Environ(), "FUN_DUMP="+tmpPath)
	cmd.Stdout = os.Stderr // 用户程序的输出不污染元数据文件
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 go run 失败: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	// 用户 main 需要响应 FUN_DUMP（调用 f.Start 的都会自动响应）；
	// 首次 go run 含编译耗时，给足余量
	select {
	case err := <-done:
		if err != nil {
			return nil, fmt.Errorf("go run 退出: %w（程序需调用 f.Start 或显式处理 fun.DumpRequested）", err)
		}
	case <-time.After(120 * time.Second):
		cmd.Process.Kill()
		return nil, fmt.Errorf("等待元数据超时：程序 120 秒内未退出。请确认 main 调用了 f.Start（会自动响应 FUN_DUMP 导出后退出），或用 fun.DumpRequested + f.DumpMetadata 显式导出")
	}

	return loadMeta(tmpPath)
}

func cmdGen(args []string) {
	fs := flag.NewFlagSet("gen", flag.ExitOnError)
	out := fs.String("o", "./gen", "输出目录（会被清空重建）")
	from := fs.String("from", "", "从元数据 JSON 文件读取（跳过 go run 采集）")
	pkg := fs.String("p", ".", "业务 main 包路径")
	fs.Parse(reorderFlags(args))

	rest := fs.Args()
	wantTS, wantGo := false, false
	switch {
	case len(rest) == 0:
		wantTS, wantGo = true, true
	case len(rest) == 1 && (rest[0] == "ts" || rest[0] == "go"):
		wantTS = rest[0] == "ts"
		wantGo = rest[0] == "go"
	default:
		fmt.Fprintln(os.Stderr, "用法: fun gen [ts|go] [-o 输出目录] [-from meta.json] [-p 包路径]")
		os.Exit(2)
	}

	lang := "both"
	if wantTS && !wantGo {
		lang = "ts"
	} else if wantGo && !wantTS {
		lang = "go"
	}

	if *from != "" {
		meta, err := loadMeta(*from)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fun: 读取元数据失败:", err)
			os.Exit(1)
		}
		files := fileSet{}
		if wantTS {
			renderTs{}.renderAll(meta, files)
		}
		if wantGo {
			renderGo{}.renderAll(meta, files)
		}
		if err := writeFiles(*out, files); err != nil {
			fmt.Fprintln(os.Stderr, "fun: 写入产物失败:", err)
			os.Exit(1)
		}
		reportGen(*out, files)
		return
	}

	root, err := findModuleRoot(*pkg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fun:", err)
		os.Exit(1)
	}
	if err := runFunGen(root, lang, *out); err != nil {
		fmt.Fprintln(os.Stderr, "fun:", err)
		os.Exit(1)
	}
	count := countGenFiles(*out)
	fmt.Printf("fun: 已生成客户端（%s）→ %s\n", lang, *out)
	for _, name := range count {
		fmt.Println("  生成:", name)
	}
}

func reportGen(out string, files fileSet) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, filepath.ToSlash(name))
	}
	sortStrings(names)
	fmt.Printf("fun: 已生成 %d 个文件 → %s\n", len(files), out)
	for _, name := range names {
		fmt.Println("  生成:", name)
	}
}

func countGenFiles(out string) []string {
	var names []string
	filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(out, path)
			names = append(names, filepath.ToSlash(rel))
		}
		return nil
	})
	sortStrings(names)
	return names
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// reorderFlags 把 -x 值 对挪到位置参数之前，让 "fun gen ts -o dir" 与
// "fun gen -o dir ts" 等价（flag 包遇到首个位置参数就停止解析）
func reorderFlags(args []string) []string {
	var flags, rest []string
	i := 0
	for i < len(args) {
		a := args[i]
		if strings.HasPrefix(a, "-") && a != "-" && a != "--" {
			flags = append(flags, a)
			// 有值的标志：-o dir / -odir
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags = append(flags, args[i+1])
				i += 2
				continue
			}
			i++
			continue
		}
		rest = append(rest, a)
		i++
	}
	return append(flags, rest...)
}
