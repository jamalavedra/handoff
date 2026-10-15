package core

import "testing"

func TestCompleteCalls(t *testing.T) {
	call := map[string]any{"role": "assistant", "tool_calls": []any{"a", "b"}}
	res := map[string]any{"role": "tool"}
	if got := completeCalls([]map[string]any{call, res}); len(got) != 0 {
		t.Fatalf("half-answered round kept: %v", got)
	}
	if got := completeCalls([]map[string]any{call, res, res}); len(got) != 3 {
		t.Fatalf("answered round dropped: %v", got)
	}
}
