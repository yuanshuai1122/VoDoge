package api

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func loadSpec(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("openapi.vodoge.yaml")
	if err != nil {
		t.Fatalf("read openapi.vodoge.yaml: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("openapi.vodoge.yaml is invalid YAML: %v", err)
	}
	return doc
}

func TestOpenAPIVoDogeYAMLValid(t *testing.T) {
	doc := loadSpec(t)
	if doc["openapi"] == "" {
		t.Fatalf("openapi.vodoge.yaml missing openapi version")
	}
}

func specSchemas(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	comp, ok := doc["components"].(map[string]any)
	if !ok {
		t.Fatal("spec 缺少 components")
	}
	schemas, ok := comp["schemas"].(map[string]any)
	if !ok {
		t.Fatal("spec 缺少 components.schemas")
	}
	return schemas
}

func TestOpenAPIDeclaresEnvelopeSchemas(t *testing.T) {
	schemas := specSchemas(t, loadSpec(t))
	for _, name := range []string{"Envelope", "ErrorEnvelope", "ErrorDetail"} {
		if _, ok := schemas[name]; !ok {
			t.Fatalf("spec 缺少 %s —— 响应结构的定义没跟上实现", name)
		}
	}
}

// 每个 2xx 的 JSON 响应都必须引用信封。
//
// spec 与实现分叉过一次（缺 17 条真实端点、含 3 条不存在的），代价是"照着
// spec 写客户端只会拿到 404"。这条测试守的是同一件事的另一半：路径对得上，
// 但响应形状对不上，同样会让照着 spec 写的客户端拿到解析不了的东西。
func TestOpenAPISuccessResponsesUseEnvelope(t *testing.T) {
	doc := loadSpec(t)
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatal("spec 缺少 paths")
	}

	var offenders []string
	for path, item := range paths {
		ops, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for method, op := range ops {
			opMap, ok := op.(map[string]any)
			if !ok {
				continue
			}
			responses, ok := opMap["responses"].(map[string]any)
			if !ok {
				continue
			}
			for status, resp := range responses {
				if !strings.HasPrefix(fmt.Sprint(status), "2") {
					continue
				}
				schema := jsonSchemaOf(resp)
				if schema == nil {
					continue // 非 JSON 响应（HTML、SSE、YAML 文档）
				}
				if !referencesEnvelope(schema) {
					offenders = append(offenders,
						fmt.Sprintf("%s %s -> %v", strings.ToUpper(method), path, status))
				}
			}
		}
	}

	if len(offenders) > 0 {
		t.Fatalf("以下 2xx JSON 响应没有引用 Envelope：\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// 错误响应同理：必须是 ErrorEnvelope。
func TestOpenAPIErrorResponsesUseErrorEnvelope(t *testing.T) {
	doc := loadSpec(t)
	comp := doc["components"].(map[string]any)
	responses, ok := comp["responses"].(map[string]any)
	if !ok {
		t.Fatal("spec 缺少 components.responses")
	}
	for name, resp := range responses {
		schema := jsonSchemaOf(resp)
		if schema == nil {
			t.Fatalf("响应组件 %s 不是 JSON", name)
		}
		if !refersTo(schema, "ErrorEnvelope") {
			t.Fatalf("响应组件 %s 未引用 ErrorEnvelope", name)
		}
	}
}

// jsonSchemaOf 取出响应里 application/json 的 schema，非 JSON 返回 nil。
func jsonSchemaOf(resp any) map[string]any {
	respMap, ok := resp.(map[string]any)
	if !ok {
		return nil
	}
	// $ref 指向 components/responses 的，由上面那条测试覆盖
	if _, isRef := respMap["$ref"]; isRef {
		return nil
	}
	content, ok := respMap["content"].(map[string]any)
	if !ok {
		return nil
	}
	media, ok := content["application/json"].(map[string]any)
	if !ok {
		return nil
	}
	schema, _ := media["schema"].(map[string]any)
	return schema
}

func referencesEnvelope(schema map[string]any) bool {
	return refersTo(schema, "Envelope")
}

func refersTo(schema map[string]any, name string) bool {
	want := "#/components/schemas/" + name
	if ref, ok := schema["$ref"].(string); ok && ref == want {
		return true
	}
	all, ok := schema["allOf"].([]any)
	if !ok {
		return false
	}
	for _, part := range all {
		p, ok := part.(map[string]any)
		if !ok {
			continue
		}
		if ref, ok := p["$ref"].(string); ok && ref == want {
			return true
		}
	}
	return false
}
