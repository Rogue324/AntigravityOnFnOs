package agy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/config"
)

const mockACPScript = `#!/usr/bin/env python3
import json
import sys

def send(obj):
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()

def read_msg():
    line = sys.stdin.readline()
    if not line:
        return None
    return json.loads(line)

session_id = "acp-sess-100"

while True:
    msg = read_msg()
    if msg is None:
        break
    method = msg.get("method")
    req_id = msg.get("id")
    params = msg.get("params") or {}

    if method == "initialize":
        send({
            "jsonrpc": "2.0",
            "id": req_id,
            "result": {
                "protocolVersion": 1,
                "agentInfo": {"name": "mock-agy", "version": "1.0.0"}
            }
        })
    elif method == "session/new":
        send({
            "jsonrpc": "2.0",
            "id": req_id,
            "result": {
                "sessionId": session_id,
                "modes": {
                    "currentModeId": "default",
                    "availableModes": [
                        {"id": "default", "name": "Default"},
                        {"id": "accept-edits", "name": "Accept Edits"},
                        {"id": "plan", "name": "Plan"}
                    ]
                }
            }
        })
    elif method in ("session/set_mode", "session/set_model", "session/set_config_option"):
        send({"jsonrpc": "2.0", "id": req_id, "result": {}})
    elif method == "session/prompt":
        send({
            "jsonrpc": "2.0",
            "method": "session/update",
            "params": {
                "sessionId": session_id,
                "update": {
                    "sessionUpdate": "agent_thought_chunk",
                    "content": {"type": "text", "text": "thinking..."}
                }
            }
        })
        send({
            "jsonrpc": "2.0",
            "method": "session/update",
            "params": {
                "sessionId": session_id,
                "update": {
                    "sessionUpdate": "tool_call",
                    "toolCallId": "tc-1",
                    "title": "Run command: ls -la",
                    "kind": "execute",
                    "status": "pending",
                    "rawInput": {"command": "ls -la"}
                }
            }
        })
        # Ask client for permission!
        send({
            "jsonrpc": "2.0",
            "id": 99,
            "method": "session/request_permission",
            "params": {
                "sessionId": session_id,
                "toolCall": {
                    "toolCallId": "tc-1",
                    "title": "Run command: ls -la",
                    "kind": "execute",
                    "rawInput": {"command": "ls -la"}
                },
                "options": [
                    {"optionId": "allow_once", "name": "允许一次", "kind": "allow_once"},
                    {"optionId": "reject_once", "name": "拒绝", "kind": "reject_once"}
                ]
            }
        })
        # Wait for client's JSON-RPC response to id 99
        perm_resp = read_msg()
        outcome = ((perm_resp or {}).get("result") or {}).get("outcome") or {}
        chosen = outcome.get("optionId", "cancelled")

        send({
            "jsonrpc": "2.0",
            "method": "session/update",
            "params": {
                "sessionId": session_id,
                "update": {
                    "sessionUpdate": "tool_call_update",
                    "toolCallId": "tc-1",
                    "status": "completed" if chosen == "allow_once" else "failed",
                    "rawOutput": "decision=" + chosen
                }
            }
        })
        send({
            "jsonrpc": "2.0",
            "method": "session/update",
            "params": {
                "sessionId": session_id,
                "update": {
                    "sessionUpdate": "agent_message_chunk",
                    "content": {"type": "text", "text": "Result: " + chosen}
                }
            }
        })
        send({
            "jsonrpc": "2.0",
            "id": req_id,
            "result": {
                "stopReason": "end_turn",
                "usage": {"inputTokens": 10, "outputTokens": 5, "totalTokens": 15}
            }
        })
`

func TestACPInteractivePermissionFlow(t *testing.T) {
	tmpDir := t.TempDir()
	paths := config.Paths{
		DataDir:      tmpDir,
		HomeDir:      filepath.Join(tmpDir, "home"),
		BinDir:       filepath.Join(tmpDir, "bin"),
		ConfigFile:   filepath.Join(tmpDir, "config.json"),
		SessionsFile: filepath.Join(tmpDir, "sessions.json"),
	}
	store, err := config.NewStore(paths)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	fakeBin := filepath.Join(paths.BinDir, "agy")
	if err := os.WriteFile(fakeBin, []byte(mockACPScript), 0o755); err != nil {
		t.Fatalf("write mock agy failed: %v", err)
	}
	if err := store.Update(func(c *config.Config) {
		c.AgyBinaryPath = fakeBin
		c.AllowDangerousSkip = true
		c.PersistentIdleMinutes = 5
	}); err != nil {
		t.Fatalf("config update failed: %v", err)
	}

	mgr := NewManager(store)
	defer mgr.CloseAll()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Interactive mode: must emit permission_request and wait for ResolvePermission.
	var mu sync.Mutex
	var events []ChatEvent
	permSeen := false

	res, err := mgr.RunChat(ctx, ChatRequest{
		SessionID:      "sess-1",
		Prompt:         "list files",
		Workspace:      tmpDir,
		PermissionMode: "default",
	}, func(ev ChatEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
		if ev.Type == "permission_request" && ev.PermissionRequest != nil {
			permSeen = true
			reqID := ev.PermissionRequest.RequestID
			go func() {
				time.Sleep(50 * time.Millisecond)
				_ = mgr.ResolvePermission("sess-1", reqID, PermissionDecision{
					OptionID: "allow_once",
				})
			}()
		}
	})
	if err != nil {
		t.Fatalf("RunChat failed: %v", err)
	}
	if !permSeen {
		t.Fatalf("expected permission_request event in default mode")
	}
	if res.ConversationID != "acp-sess-100" {
		t.Fatalf("expected ConversationID acp-sess-100, got %q", res.ConversationID)
	}
	if !strings.Contains(res.Message.Content, "Result: allow_once") {
		t.Fatalf("unexpected content: %q", res.Message.Content)
	}
	if res.Message.Thinking != "thinking..." {
		t.Fatalf("unexpected thinking: %q", res.Message.Thinking)
	}
	if len(res.Message.ToolCalls) != 1 || res.Message.ToolCalls[0].Status != "done" {
		t.Fatalf("unexpected tool calls: %+v", res.Message.ToolCalls)
	}

	// 2. Bypass mode: should auto-approve without emitting permission_request.
	bypassPermSeen := false
	res2, err := mgr.RunChat(ctx, ChatRequest{
		SessionID:      "sess-1",
		ConversationID: res.ConversationID,
		Prompt:         "list again",
		Workspace:      tmpDir,
		PermissionMode: "bypass",
	}, func(ev ChatEvent) {
		if ev.Type == "permission_request" {
			bypassPermSeen = true
		}
	})
	if err != nil {
		t.Fatalf("RunChat bypass failed: %v", err)
	}
	if bypassPermSeen {
		t.Fatalf("did not expect permission_request in bypass mode")
	}
	if !strings.Contains(res2.Message.Content, "Result: allow_once") {
		t.Fatalf("unexpected bypass content: %q", res2.Message.Content)
	}
}
