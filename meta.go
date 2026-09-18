package main

// 元数据 JSON 契约：与 fun 核心库 meta.go 的导出结构一一对应。
// CLI 不依赖 fun 模块，仅消费该 JSON，保持零第三方依赖

import (
	"encoding/json"
	"os"
)

type FunMeta struct {
	Services []MetaService `json:"services"`
}

type MetaService struct {
	Name    string       `json:"name"`
	Methods []MetaMethod `json:"methods"`
}

type MetaMethod struct {
	Name     string    `json:"name"`
	IsStream bool      `json:"isStream"`
	Void     bool      `json:"void,omitempty"`
	DTO      *MetaType `json:"dto,omitempty"`
	Returns  *MetaType `json:"returns,omitempty"`
}

type MetaType struct {
	Kind     string      `json:"kind"` // int/bool/string/struct/slice/enum
	Name     string      `json:"name,omitempty"`
	Optional bool        `json:"optional,omitempty"`
	Fields   []MetaField `json:"fields,omitempty"`
	Elem     *MetaType   `json:"elem,omitempty"`

	Names        []string `json:"names,omitempty"`
	DisplayNames []string `json:"displayNames,omitempty"`
}

type MetaField struct {
	Name string    `json:"name"`
	Type *MetaType `json:"type"`
}

// loadMeta 从文件读取元数据
func loadMeta(path string) (*FunMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta FunMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}
