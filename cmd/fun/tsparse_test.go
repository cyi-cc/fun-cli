package main

// 渲染→解析回环:用渲染器从元数据生成 TS,再解析回元数据,验证解析器覆盖
// 枚举/可空/流式/嵌套等全部特性

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTsRenderParseRoundTrip(t *testing.T) {
	meta, err := loadMeta(filepath.Join("testdata", "demo.json"))
	if err != nil {
		t.Fatal(err)
	}
	files := fileSet{}
	renderTs{}.renderAll(meta, files)
	dir := t.TempDir()
	if err := writeFiles(dir, files); err != nil {
		t.Fatal(err)
	}

	parsed, err := parseTsDir(filepath.Join(dir, "ts"))
	if err != nil {
		t.Fatal(err)
	}

	var ask, create *MetaMethod
	var order, chat *MetaService
	for i := range parsed.Services {
		switch parsed.Services[i].Name {
		case "orderSvc":
			order = &parsed.Services[i]
		case "chatSvc":
			chat = &parsed.Services[i]
		}
	}
	if order == nil || chat == nil {
		t.Fatalf("services missing: %+v", parsed.Services)
	}
	for i := range order.Methods {
		if order.Methods[i].Name == "create" {
			create = &order.Methods[i]
		}
	}
	for i := range chat.Methods {
		if chat.Methods[i].Name == "ask" {
			ask = &chat.Methods[i]
		}
	}

	if ask == nil || !ask.IsStream || ask.Returns == nil || ask.Returns.Kind != "string" {
		t.Errorf("chatSvc.ask 解析错误: %+v", ask)
	}
	if ask.DTO == nil || len(ask.DTO.Fields) != 1 || ask.DTO.Fields[0].Name != "prompt" {
		t.Errorf("chatSvc.ask DTO 解析错误: %+v", ask.DTO)
	}
	if create == nil || create.Void {
		t.Errorf("orderSvc.create 解析错误: %+v", create)
	}
	if create == nil || create.DTO == nil || len(create.DTO.Fields) != 5 {
		t.Fatalf("createOrderDto 字段数错误: %+v", create.DTO)
	}
	want := map[string]struct {
		kind string
		opt  bool
	}{
		"sku":    {"string", false},
		"count":  {"int", false},
		"note":   {"string", true},
		"status": {"enum", true},
		"tags":   {"slice", false},
	}
	for _, f := range create.DTO.Fields {
		w, ok := want[f.Name]
		if !ok {
			t.Errorf("多余字段: %s", f.Name)
			continue
		}
		if f.Type.Kind != w.kind || f.Type.Optional != w.opt {
			t.Errorf("字段 %s: kind=%s opt=%v, 期望 %s/%v", f.Name, f.Type.Kind, f.Type.Optional, w.kind, w.opt)
		}
	}
	_ = os.Environ
}
