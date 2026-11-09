package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

// guardPayload builds a safeguard field shaped like the one Claude Code sends,
// with prior_turn_context big enough to model a long conversation.
func guardPayload(t *testing.T, priorTurns int) json.RawMessage {
	t.Helper()
	turns := make([]map[string]any, 0, priorTurns)
	for i := 0; i < priorTurns; i++ {
		turns = append(turns, map[string]any{"role": "assistant", "content": strings.Repeat("x", 4000)})
	}
	raw, err := json.Marshal([]map[string]any{{
		"type": "dangerous_tool_use",
		"classifier_context": map[string]any{
			"permission_mode":    "default",
			"prior_turn_context": turns,
		},
	}})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return raw
}

func extraBodySafeguards(t *testing.T, extraBody string) (map[string]json.RawMessage, []map[string]any) {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(extraBody), &body); err != nil {
		t.Fatalf("unmarshal extra body: %v", err)
	}
	raw, ok := body["safeguards"]
	if !ok {
		return body, nil
	}
	var guards []map[string]any
	if err := json.Unmarshal(raw, &guards); err != nil {
		t.Fatalf("unmarshal safeguards: %v", err)
	}
	return body, guards
}

func TestClaudeExtraBodyKeepsWhatFits(t *testing.T) {
	raw := guardPayload(t, 2)
	body, guards := extraBodySafeguards(t, claudeExtraBody(raw, ""))
	if string(body["safeguards"]) != string(raw) {
		t.Errorf("a payload under the cap should pass through unchanged")
	}
	if len(guards) != 1 {
		t.Fatalf("safeguards entries = %d, want 1", len(guards))
	}
}

func TestClaudeExtraBodyShedsTheTurnsThatGrow(t *testing.T) {
	raw := guardPayload(t, 40)
	if extraBodyFits(raw) {
		t.Fatalf("fixture of %d bytes should exceed the cap", len(raw))
	}
	got := claudeExtraBody(raw, "")
	if len(got) > maxExtraBody {
		t.Errorf("extra body is %d bytes, over the %d cap", len(got), maxExtraBody)
	}
	_, guards := extraBodySafeguards(t, got)
	if len(guards) != 1 {
		t.Fatalf("safeguards entries = %d, want 1", len(guards))
	}
	ctx, ok := guards[0]["classifier_context"].(map[string]any)
	if !ok {
		t.Fatalf("classifier_context missing: %v", guards[0])
	}
	if _, ok := ctx["prior_turn_context"]; ok {
		t.Errorf("prior_turn_context should be shed first")
	}
	if ctx["permission_mode"] != "default" {
		t.Errorf("what does not grow turn to turn should stay, got %v", ctx["permission_mode"])
	}
}

func TestClaudeExtraBodyDropsWhatStaysTooBig(t *testing.T) {
	raw, err := json.Marshal([]map[string]any{{
		"type": "dangerous_tool_use",
		"blob": strings.Repeat("y", 2*maxExtraBody),
	}})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	got := claudeExtraBody(raw, "")
	if len(got) > maxExtraBody {
		t.Errorf("extra body is %d bytes, over the %d cap", len(got), maxExtraBody)
	}
	body, guards := extraBodySafeguards(t, got)
	if guards != nil {
		t.Errorf("a payload that cannot fit should be dropped, got %d entries", len(guards))
	}
	if _, ok := body["safeguards"]; ok {
		t.Errorf("safeguards should not be set when it cannot fit")
	}
}

func TestClaudeExtraBodyKeepsTheCacheTail(t *testing.T) {
	got := claudeExtraBody(guardPayload(t, 40), "keep this")
	body, _ := extraBodySafeguards(t, got)
	if _, ok := body["cache_control"]; !ok {
		t.Errorf("the opted-in cache tail should survive the trim")
	}
}
