package main

// fun docs：拉起 API 文档站——侧栏服务/方法树、DTO 参数表单、
// 一键发送打到反向代理的后端，流式方法实时展示 NDJSON 行

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
)

//go:embed docsui/index.html
var docsFS embed.FS

func cmdDocs(args []string) {
	fs := flag.NewFlagSet("docs", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:7788", "文档站监听地址")
	upstream := fs.String("upstream", "http://127.0.0.1:8080", "后端服务地址（反向代理目标）")
	from := fs.String("from", "", "从元数据 JSON 文件读取")
	pkg := fs.String("p", ".", "业务 main 包路径（用于采集元数据与自动启动后端）")
	noStart := fs.Bool("no-run", false, "不自动启动后端（后端已在别处运行时使用）")
	editsPath := fs.String("edits", "fun-docs-edits.json", "说明/备注编辑存档文件（启动时自动创建）")
	fs.Parse(args)

	var meta *FunMeta
	var err error
	var root string
	if *from != "" {
		meta, err = loadMeta(*from)
	} else {
		root, err = findModuleRoot(*pkg)
		if err == nil {
			genDir, _ := os.MkdirTemp("", "fun-docs-gen-*")
			defer os.RemoveAll(genDir)
			err = runFunGen(root, "ts", genDir)
			if err == nil {
				meta, err = parseTsDir(filepath.Join(genDir, "ts"))
			}
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fun: 采集元数据失败:", err)
		os.Exit(1)
	}
	if len(meta.Services) == 0 {
		fmt.Fprintln(os.Stderr, "fun: 元数据中没有服务，请先 BindService")
		os.Exit(1)
	}

	// 自动起后端：先编译成功再启动（与 fun run 同套语义，热更新复用）
	binPath := filepath.Join(os.TempDir(), fmt.Sprintf("fun-docs-run-%d", os.Getpid()))
	var backend *exec.Cmd
	var backendDone chan struct{}
	if !*noStart {
		if !build(*pkg, binPath) {
			fmt.Fprintln(os.Stderr, "fun: 后端编译失败")
			os.Exit(1)
		}
		backend, backendDone = spawn(binPath, nil)
	}

	target, err := url.Parse(*upstream)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fun: upstream 地址无效:", err)
		killChild(backend, nil)
		os.Exit(1)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)

	loadOrCreateEdits(*editsPath)

	// 热更新：元数据与版本号会被 watcher 刷新，HTTP 读侧与写侧用同一把锁
	var metaMu sync.RWMutex
	metaJSON := renderMetaJSON(meta)
	var metaVersion int64

	// 监听源码变更：重采元数据 + 重建重启后端（-from 静态模式不启用）
	if root != "" {
		restart := make(chan struct{}, 1)
		go watchLoop(root, restart)
		go func() {
			for range restart {
				fmt.Println("fun: 检测到源码变更，重新采集元数据...")
				genDir, _ := os.MkdirTemp("", "fun-docs-gen-*")
				genErr := runFunGen(root, "ts", genDir)
				var m *FunMeta
				if genErr == nil {
					m, genErr = parseTsDir(filepath.Join(genDir, "ts"))
				}
				os.RemoveAll(genDir)
				if genErr != nil {
					fmt.Fprintln(os.Stderr, "fun: 元数据刷新失败（保留旧文档）:", genErr)
					continue
				}
				metaMu.Lock()
				metaJSON = renderMetaJSON(m)
				metaVersion++
				metaMu.Unlock()
				if *noStart {
					fmt.Println("fun: 元数据已刷新（后端由外部管理）")
					continue
				}
				if !build(*pkg, binPath) {
					fmt.Fprintln(os.Stderr, "fun: 编译失败，旧后端继续运行")
					continue
				}
				killChild(backend, backendDone)
				backend, backendDone = spawn(binPath, nil)
				fmt.Println("fun: 已重启后端并刷新文档元数据")
			}
		}()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/index.html":
			data, _ := docsFS.ReadFile("docsui/index.html")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(data)
		case "/fun-meta.json":
			metaMu.RLock()
			data := metaJSON
			metaMu.RUnlock()
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Write(data)
		case "/fun-version":
			metaMu.RLock()
			v := metaVersion
			metaMu.RUnlock()
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			fmt.Fprintf(w, `{"v":%d}`, v)
		case "/fun-docs-edits.json":
			editsMu.RLock()
			data, _ := json.Marshal(edits)
			editsMu.RUnlock()
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Write(data)
		case "/fun-docs-edits":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var req struct {
				Key   string          `json:"key"`
				Entry json.RawMessage `json:"entry"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Key == "" {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			editsMu.Lock()
			if len(req.Entry) == 0 || string(req.Entry) == "null" {
				delete(edits, req.Key)
			} else {
				edits[req.Key] = req.Entry
			}
			data, _ := json.MarshalIndent(edits, "", "  ")
			editsMu.Unlock()
			if err := os.WriteFile(*editsPath, data, 0o644); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Write([]byte(`{"ok":true}`))
		default:
			proxy.ServeHTTP(w, r)
		}
	})

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		killChild(backend, backendDone)
		os.Exit(0)
	}()

	nMethods := 0
	for _, svc := range meta.Services {
		nMethods += len(svc.Methods)
	}
	hot := "关（-from 静态模式）"
	if root != "" {
		hot = "开（源码变更自动重建后端 + 刷新元数据）"
	}
	fmt.Printf(`
  fun docs 已启动
  ─────────────────────────────────────────
  文档站:   http://%s
  后端代理: %s → %s
  说明存档: %s
  热更新:   %s
  ─────────────────────────────────────────
  %d 个服务 / %d 个方法。Ctrl+C 退出（连同自动拉起的后端）
`, *addr, *upstream, *upstream, *editsPath, hot, len(meta.Services), nMethods)

	if err := http.ListenAndServe(*addr, mux); err != nil {
		fmt.Fprintln(os.Stderr, "fun: 文档站启动失败:", err)
		killChild(backend, nil)
		os.Exit(1)
	}
}

// 说明/备注编辑存档：map[服务.方法] -> {note, fields:{路径:备注}}
var (
	editsMu sync.RWMutex
	edits   = map[string]json.RawMessage{}
)

func loadOrCreateEdits(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			os.WriteFile(path, []byte("{}\n"), 0o644)
			return
		}
		fmt.Fprintln(os.Stderr, "fun: 读取编辑存档失败:", err)
		return
	}
	if err := json.Unmarshal(data, &edits); err != nil {
		fmt.Fprintln(os.Stderr, "fun: 编辑存档格式无效，按空处理:", err)
	}
}

func renderMetaJSON(meta *FunMeta) []byte {
	data, err := json.Marshal(meta)
	if err != nil {
		return []byte(`{"services":[]}`)
	}
	return data
}
