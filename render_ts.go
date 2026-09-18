package main

// TS 渲染器：fun v1.3.7 gen_ts.go 的等价移植，输入从 reflect.Type 换成元数据类型树

import "strings"

type renderTs struct{}

// typeName 等价旧 typeToTemplateType：指针 → " | null" 后缀，切片 → "[]"，
// 整数 → number（枚举除外），struct/string → 类型名
func (renderTs) typeName(t *MetaType) string {
	suffix := ""
	if t.Optional {
		suffix = " | null"
	}
	switch t.Kind {
	case "enum":
		return t.Name + suffix
	case "int":
		return "number" + suffix
	case "bool":
		return "boolean" + suffix
	case "string", "struct":
		return t.Name + suffix
	default: // slice
		return renderTs{}.typeName(t.Elem) + "[]" + suffix
	}
}

func (r renderTs) genService(svc MetaService, serviceContext *genServiceType, files fileSet) {
	var nestedImports []*genImportType

	for _, gm := range svc.Methods {
		var returnValueText string
		var dtoText string
		var argsText string
		var genericTypeText string

		// () error：无数据返回，客户端类型为 void
		if gm.Void {
			genericTypeText = "void"
			returnValueText = "result<void>"
			if gm.DTO != nil {
				v := firstLetterToLower(r.typeName(gm.DTO))
				if !strings.Contains(v, "[]") && strings.Contains(v, "[") {
					dtoText += "dto:" + getGenericTypeName(v) + parseGenericTypeParams(v)
				} else {
					dtoText += "dto:" + v
				}
				argsText += ",dto"
				nestedImports = append(nestedImports, r.genStruct(gm.DTO, files))
			}
			serviceContext.IsIncludeRequest = true
			serviceContext.GenMethodTypeList = append(serviceContext.GenMethodTypeList, &genMethodType{
				MethodName:      firstLetterToLower(gm.Name),
				ReturnValueText: returnValueText,
				DtoText:         dtoText,
				ArgsText:        argsText,
				GenericTypeText: genericTypeText,
			})
			continue
		}

		if gm.IsStream {
			serviceContext.IsIncludeStream = true
			if gm.Returns == nil {
				genericTypeText = "any"
				returnValueText = "void"
			} else {
				t := firstLetterToLower(r.typeName(gm.Returns))
				if !strings.Contains(t, "[]") && strings.Contains(t, "[") {
					genericTypeText = getGenericTypeName(t) + parseGenericTypeParams(t)
				} else {
					genericTypeText = t
				}
				returnValueText = "void"
				nestedImports = r.genReturnTypes(gm.Returns, nestedImports, files)
			}
		} else {
			serviceContext.IsIncludeRequest = true
			t := firstLetterToLower(r.typeName(gm.Returns))
			if !strings.Contains(t, "[]") && strings.Contains(t, "[") {
				returnValueText = getGenericTypeName(t) + parseGenericTypeParams(t)
			} else {
				returnValueText = t
			}
			genericTypeText = returnValueText
			nestedImports = r.genReturnTypes(gm.Returns, nestedImports, files)
			returnValueText = "result<" + returnValueText + ">"
		}

		if gm.DTO != nil {
			v := firstLetterToLower(r.typeName(gm.DTO))
			if !strings.Contains(v, "[]") && strings.Contains(v, "[") {
				dtoText += "dto:" + getGenericTypeName(v) + parseGenericTypeParams(v)
			} else {
				dtoText += "dto:" + v
			}
			argsText += ",dto"
			nestedImports = append(nestedImports, r.genStruct(gm.DTO, files))
		}

		serviceContext.GenMethodTypeList = append(serviceContext.GenMethodTypeList, &genMethodType{
			MethodName:      firstLetterToLower(gm.Name),
			ReturnValueText: returnValueText,
			DtoText:         dtoText,
			ArgsText:        argsText,
			GenericTypeText: firstLetterToLower(genericTypeText),
			IsStream:        gm.IsStream,
		})
	}
	serviceContext.GenImport = deduplicateServiceImports(nestedImports)

	renderCode(
		templateTs{}.genServiceTemplate(),
		firstLetterToLower(svc.Name),
		serviceContext,
		"ts",
		files,
	)
}

// genReturnTypes 递归生成返回类型涉及的 struct/enum 导入
func (r renderTs) genReturnTypes(returnType *MetaType, nestedImports []*genImportType, files fileSet) []*genImportType {
	if returnType.Kind == "struct" {
		nestedImports = append(nestedImports, r.genStruct(returnType, files))
	}
	if returnType.Kind == "slice" && returnType.Elem.Kind == "struct" {
		nestedImports = append(nestedImports, r.genStruct(returnType.Elem, files))
	}
	if returnType.Kind == "enum" {
		nestedImports = append(nestedImports, r.getEnum(returnType, files))
	}
	return nestedImports
}

func (r renderTs) renderAll(meta *FunMeta, files fileSet) {
	genContext := genType{GenServiceList: []*genServiceType{}}

	for _, svc := range meta.Services {
		serviceContext := &genServiceType{
			ServiceName:       firstLetterToLower(svc.Name),
			GenMethodTypeList: []*genMethodType{},
		}
		genContext.GenServiceList = append(genContext.GenServiceList, serviceContext)
		r.genService(svc, serviceContext, files)
	}
	renderCode(templateTs{}.genClientTemplate(), "client", nil, "ts", files)
	renderCode(templateTs{}.genDefaultServiceTemplate(), "fun", genContext, "ts", files)
}

func (r renderTs) genStruct(t *MetaType, files fileSet) *genImportType {
	var structTemplate genClassType
	if strings.Contains(t.Name, "[]") == false && strings.Contains(t.Name, "[") {
		structTemplate = genClassType{
			Name: firstLetterToLower(getGenericTypeName(t.Name)) + parseGenericTypeParams(t.Name),
		}
	} else {
		structTemplate = genClassType{
			Name: firstLetterToLower(t.Name),
		}
	}
	var nestedImports []*genImportType

	for _, field := range t.Fields {
		fieldType := field.Type
		jsType := r.typeName(fieldType)
		name := field.Name
		if fieldType.Optional {
			name += "?"
		}
		if !strings.Contains(jsType, "[]") && strings.Contains(jsType, "[") {
			structTemplate.GenClassFieldType = append(structTemplate.GenClassFieldType, &genClassFieldType{
				Name: firstLetterToLower(name),
				Type: firstLetterToLower(getGenericTypeName(jsType)) + parseGenericTypeParams(jsType),
			})
		} else {
			structTemplate.GenClassFieldType = append(structTemplate.GenClassFieldType, &genClassFieldType{
				Name: firstLetterToLower(name),
				Type: firstLetterToLower(jsType),
			})
		}

		if fieldType.Kind == "struct" {
			nestedImports = append(nestedImports, r.genStruct(fieldType, files))
		}
		if fieldType.Kind == "slice" && fieldType.Elem.Kind == "struct" {
			nestedImports = append(nestedImports, r.genStruct(fieldType.Elem, files))
		}
		if fieldType.Kind == "enum" {
			nestedImports = append(nestedImports, r.getEnum(fieldType, files))
		}
	}

	structTemplate.GenImport = deduplicateServiceImports(nestedImports)

	renderCode(
		templateTs{}.genStructTemplate(),
		structTemplate.Name,
		structTemplate,
		"ts",
		files,
	)

	if strings.Contains(t.Name, "[]") == false && strings.Contains(t.Name, "[") {
		return &genImportType{Name: structTemplate.Name}
	}
	return &genImportType{Name: firstLetterToLower(t.Name)}
}

func (r renderTs) getEnum(t *MetaType, files fileSet) *genImportType {
	var enumTemplate genEnumType
	enumTemplate.Names = t.Names
	enumTemplate.DisplayNames = t.DisplayNames
	enumTemplate.Name = firstLetterToLower(t.Name)

	renderCode(
		templateTs{}.genEnumTemplate(),
		firstLetterToLower(t.Name),
		enumTemplate,
		"ts",
		files,
	)
	return &genImportType{Name: firstLetterToLower(t.Name)}
}
