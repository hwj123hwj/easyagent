package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestToolOutputStore_PutGet(t *testing.T) {
	store := NewToolOutputStore(0)
	ref := store.Put("first output")
	if ref == "" {
		t.Fatal("Put should return a non-empty ref")
	}
	got, ok := store.Get(ref)
	if !ok || got != "first output" {
		t.Errorf("Get(%q) = (%q, %v), want stored content", ref, got, ok)
	}
	if _, ok := store.Get("missing"); ok {
		t.Error("unknown ref should miss")
	}
}

func TestToolOutputStore_EvictsOldest(t *testing.T) {
	store := NewToolOutputStore(2)
	first := store.Put("first")
	second := store.Put("second")
	store.Put("third")

	if _, ok := store.Get(first); ok {
		t.Error("oldest entry should be evicted when over capacity")
	}
	for _, ref := range []string{second} {
		if got, ok := store.Get(ref); !ok || got != "second" {
			t.Errorf("Get(%q) = (%q, %v), want 'second'", ref, got, ok)
		}
	}
	if got, ok := store.Get("out_3"); !ok || got != "third" {
		t.Errorf("Get(out_3) = (%q, %v), want 'third'", got, ok)
	}
}

func TestFullOutputTool_SchemaAndValidation(t *testing.T) {
	tool := NewFullOutputTool(NewToolOutputStore(0))
	if tool.Name() != "full_output" {
		t.Errorf("Name() = %q, want full_output", tool.Name())
	}
	if _, err := tool.Validate(json.RawMessage(`{}`)); err == nil {
		t.Error("missing ref should fail validation")
	}
	if _, err := tool.Validate(json.RawMessage(`{"ref":"  "}`)); err == nil {
		t.Error("blank ref should fail validation")
	}
	if _, err := tool.Validate(json.RawMessage(`{"ref":"out_1"}`)); err != nil {
		t.Errorf("valid ref should pass validation: %v", err)
	}
}

func TestFullOutputTool_Execute(t *testing.T) {
	store := NewToolOutputStore(0)
	tool := NewFullOutputTool(store)
	store.Put("full content here")

	raw, err := json.Marshal(map[string]string{"ref": "out_1"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Execute(context.Background(), raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "full content here" {
		t.Errorf("Content = %q, want stored output", result.Content)
	}

	// 未知引用返回可理解的错误结果，而不是 panic 或空内容。
	raw, _ = json.Marshal(map[string]string{"ref": "out_99"})
	result, err = tool.Execute(context.Background(), raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(result.Content, "out_99") {
		t.Errorf("unknown ref should return an error result mentioning it, got %+v", result)
	}
}
