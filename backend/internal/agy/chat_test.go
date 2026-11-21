package agy

import (
	"testing"

	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/session"
)

func TestStreamParserOfficialAgyEnvelope(t *testing.T) {
	lines := []string{
		`{"event":"init","conversation_id":"c3b66b04-872b-4fbe-a3a4-058a026ef20a","init":{"cwd":"/vol1/code","tools":["run_command"],"permission_mode":"request-review"}}`,
		`{"event":"step_update","step_update":{"conversation_id":"c3b66b04-872b-4fbe-a3a4-058a026ef20a","step_index":0,"state":"DONE","step_type":"user_input"}}`,
		`{"event":"step_update","step_update":{"conversation_id":"c3b66b04-872b-4fbe-a3a4-058a026ef20a","step_index":2,"state":"ACTIVE","step_type":"agent_response","text_delta":"你好，"}}`,
		`{"event":"step_update","step_update":{"conversation_id":"c3b66b04-872b-4fbe-a3a4-058a026ef20a","step_index":2,"state":"DONE","step_type":"agent_response","text_delta":"我是 Antigravity！\n","usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120}}}`,
		`{"event":"step_update","step_update":{"conversation_id":"c3b66b04-872b-4fbe-a3a4-058a026ef20a","step_index":3,"state":"DONE","step_type":"tool","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"ls"},"output":"README.md\n"}}}`,
		`{"event":"result","result":{"conversation_id":"c3b66b04-872b-4fbe-a3a4-058a026ef20a","status":"SUCCESS","response":"你好，我是 Antigravity！\n","num_turns":1,"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120}}}`,
	}

	res := &ChatResult{Message: session.Message{Role: "assistant"}}
	var events []ChatEvent
	p := newStreamParser(res, func(ev ChatEvent) {
		events = append(events, ev)
	})
	for _, l := range lines {
		p.ProcessLine(l)
	}
	res.Message.Content = p.textBuilder.String()

	if res.ConversationID != "c3b66b04-872b-4fbe-a3a4-058a026ef20a" {
		t.Fatalf("unexpected conversation_id: %q", res.ConversationID)
	}
	if res.Message.Content != "你好，我是 Antigravity！\n" {
		t.Fatalf("unexpected content: %q", res.Message.Content)
	}
	if len(res.Message.ToolCalls) != 1 || res.Message.ToolCalls[0].Name != "run_command" || res.Message.ToolCalls[0].Output != "README.md\n" {
		t.Fatalf("unexpected tool calls: %+v", res.Message.ToolCalls)
	}
	if res.Message.Usage == nil || res.Message.Usage.TotalTokens != 120 {
		t.Fatalf("unexpected usage: %+v", res.Message.Usage)
	}
}

func TestStreamParserResultOnlyFallback(t *testing.T) {
	lines := []string{
		`{"event":"result","result":{"conversation_id":"abc-123","status":"SUCCESS","response":"最终回复","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`,
	}
	res := &ChatResult{Message: session.Message{Role: "assistant"}}
	p := newStreamParser(res, func(ChatEvent) {})
	for _, l := range lines {
		p.ProcessLine(l)
	}
	if p.textBuilder.String() != "最终回复" {
		t.Fatalf("expected fallback result.response, got %q", p.textBuilder.String())
	}
}
