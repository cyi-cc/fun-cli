package main

// 渲染回归测试：断言与 fun v1.3.7 内置生成器产物一致的关键特征

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func renderToDir(t *testing.T, fixture string) string {
	t.Helper()
	meta, err := loadMeta(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	files := fileSet{}
	renderTs{}.renderAll(meta, files)
	renderGo{}.renderAll(meta, files)
	dir := t.TempDir()
	if err := writeFiles(dir, files); err != nil {
		t.Fatal(err)
	}
	return dir
}

func read(t *testing.T, root, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("file %s missing: %v", name, err)
	}
	return string(body)
}

func TestRenderTypeScriptSignaturesAndImports(t *testing.T) {
	root := renderToDir(t, "alpha.json")

	client := read(t, root, filepath.Join("ts", "client.ts"))
	for _, want := range []string{
		`state?: Record<string, string>;`,
		`export type ResponseContext = {`,
		`readonly requestState: Readonly<Record<string, string>>;`,
		`readonly response?: Response;`,
		`state = { ...this.state, ...options?.state };`,
		`interceptor(serviceName, methodName, current, context)`,
	} {
		if !strings.Contains(client, want) {
			t.Errorf("client.ts missing %q:\n%s", want, client)
		}
	}

	alpha := read(t, root, filepath.Join("ts", "alphaGenSvc.ts"))
	if first := strings.SplitN(alpha, "\n", 2)[0]; first != `import { Client, type result, type RequestOptions } from "./client";` {
		t.Fatalf("unexpected request-only imports: %s", first)
	}
	for _, want := range []string{
		`async ping(options?: RequestOptions): Promise<result<void>>`,
		`this.client.request<void>("alphaGenSvc", "ping", undefined, options)`,
		`async alpha(dto:alphaGenDto, options?: RequestOptions): Promise<result<zebraGenDto>>`,
		`this.client.request<zebraGenDto>("alphaGenSvc", "alpha", dto, options)`,
	} {
		if !strings.Contains(alpha, want) {
			t.Errorf("alphaGenSvc.ts missing %q:\n%s", want, alpha)
		}
	}
	if strings.Index(alpha, `import type alphaGenDto`) > strings.Index(alpha, `import type zebraGenDto`) {
		t.Fatalf("DTO imports are not sorted:\n%s", alpha)
	}
	if strings.Index(alpha, `async alpha`) > strings.Index(alpha, `async ping`) ||
		strings.Index(alpha, `async ping`) > strings.Index(alpha, `async zebra`) {
		t.Fatalf("methods are not sorted:\n%s", alpha)
	}

	stream := read(t, root, filepath.Join("ts", "zebraGenSvc.ts"))
	if first := strings.SplitN(stream, "\n", 2)[0]; first != `import { Client, type result, type StreamOptions } from "./client";` {
		t.Fatalf("unexpected stream-only imports: %s", first)
	}
	for _, want := range []string{
		`async watch(onMessage: (data: any) => unknown, options?: StreamOptions): Promise<result<void>>`,
		`this.client.stream<any>("zebraGenSvc", "watch", undefined, onMessage, options)`,
	} {
		if !strings.Contains(stream, want) {
			t.Errorf("zebraGenSvc.ts missing %q:\n%s", want, stream)
		}
	}

	mixed := read(t, root, filepath.Join("ts", "mixedGenSvc.ts"))
	if first := strings.SplitN(mixed, "\n", 2)[0]; first != `import { Client, type result, type RequestOptions, type StreamOptions } from "./client";` {
		t.Fatalf("unexpected mixed imports: %s", first)
	}
}

func TestRenderGoErrorOnlyMethod(t *testing.T) {
	root := renderToDir(t, "alpha.json")
	goSrc := read(t, root, filepath.Join("go", "alpha_gen_svc.go"))
	if !strings.Contains(goSrc, "Result[Void]") {
		t.Fatalf("go: expect Result[Void], got:\n%s", goSrc)
	}
	tsSrc := read(t, root, filepath.Join("ts", "alphaGenSvc.ts"))
	if !strings.Contains(tsSrc, "result<void>") {
		t.Fatalf("ts: expect result<void>, got:\n%s", tsSrc)
	}
}

func TestRenderDeterministic(t *testing.T) {
	first := renderToDir(t, "alpha.json")
	second := renderToDir(t, "alpha.json")

	files1, files2 := walkFiles(t, first), walkFiles(t, second)
	if len(files1) != len(files2) {
		t.Fatalf("file count changed: %d != %d", len(files1), len(files2))
	}
	for name, body := range files1 {
		if files2[name] != body {
			t.Errorf("generated file changed between runs: %s", name)
		}
	}

	tsFun := files1[filepath.Join("ts", "fun.ts")]
	positions := []int{
		strings.Index(tsFun, `import alphaGenSvc`),
		strings.Index(tsFun, `import mixedGenSvc`),
		strings.Index(tsFun, `import zebraGenSvc`),
	}
	if !sort.IntsAreSorted(positions) || positions[0] < 0 {
		t.Fatalf("TypeScript services are not sorted:\n%s", tsFun)
	}
	goFun := files1[filepath.Join("go", "fun.go")]
	positions = []int{
		strings.Index(goFun, "AlphaGenSvc *AlphaGenSvc"),
		strings.Index(goFun, "MixedGenSvc *MixedGenSvc"),
		strings.Index(goFun, "ZebraGenSvc *ZebraGenSvc"),
	}
	if !sort.IntsAreSorted(positions) || positions[0] < 0 {
		t.Fatalf("Go services are not sorted:\n%s", goFun)
	}
	goService := files1[filepath.Join("go", "alpha_gen_svc.go")]
	positions = []int{
		strings.Index(goService, "func (ctx *AlphaGenSvc) Alpha("),
		strings.Index(goService, "func (ctx *AlphaGenSvc) Ping("),
		strings.Index(goService, "func (ctx *AlphaGenSvc) Zebra("),
	}
	if !sort.IntsAreSorted(positions) || positions[0] < 0 {
		t.Fatalf("Go methods are not sorted:\n%s", goService)
	}
}

// TestRenderDemoFixture 用真实导出的示例元数据验证枚举、可空字段、流式首条消息
func TestRenderDemoFixture(t *testing.T) {
	root := renderToDir(t, "demo.json")

	enumGo := read(t, root, filepath.Join("go", "order_status.go"))
	for _, want := range []string{"type OrderStatus uint8", "OrderStatus = iota", `func (OrderStatus) DisplayNames()`} {
		if !strings.Contains(enumGo, want) {
			t.Errorf("order_status.go missing %q:\n%s", want, enumGo)
		}
	}
	enumTs := read(t, root, filepath.Join("ts", "orderStatus.ts"))
	if !strings.Contains(enumTs, "enum orderStatus {") {
		t.Errorf("orderStatus.ts missing enum:\n%s", enumTs)
	}

	dtoTs := read(t, root, filepath.Join("ts", "createOrderDto.ts"))
	for _, want := range []string{"sku:string", "count:number", "note?:string | null", "tags:string[]"} {
		if !strings.Contains(dtoTs, want) {
			t.Errorf("createOrderDto.ts missing %q:\n%s", want, dtoTs)
		}
	}

	chatTs := read(t, root, filepath.Join("ts", "chatSvc.ts"))
	if !strings.Contains(chatTs, `this.client.stream<string>("chatSvc", "ask"`) {
		t.Errorf("chatSvc.ts missing first-message stream signature:\n%s", chatTs)
	}
}

func walkFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out[rel] = string(body)
		return nil
	})
	return out
}
