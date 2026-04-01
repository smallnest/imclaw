package transcript

import "testing"

func TestParseFullTranscript(t *testing.T) {
	raw := `[acpx] session 78f133c0-862e-4ded-a22b-069338116f20 (8b2caf0c-dfe9-40c1-b718-700de1704a88) · /Users/chaoyuepan/ai/imclaw · agent needs reconnect
[client] initialize (running)

[client] session/new (running)

[thinking] The user is asking about the weather forecast for Shanghai for the next three days.

           I have access to a weather skill according to the system reminder.
我来帮您查询上海未来三天的天气预报。

[tool] Skill (pending)
  input: {}

[tool] Skill (completed)
  kind: other
  input: {"skill":"weather"}
  output:
    Launching skill: weather

[thinking] Let me interpret this weather report for Shanghai for the next three days:

           **今天 (3月31日, 周二):**
           - 上午: 阴天, 12°C
根据天气预报，上海未来三天的天气情况如下：

## **3月31日 (周二)**
- **上午**: 阴天，12°C

[done] end_turn`

	got := Parse(raw)
	if len(got) != 6 {
		t.Fatalf("expected 6 messages, got %d: %#v", len(got), got)
	}

	if got[0].Type != MessageThinking {
		t.Fatalf("expected first message to be thinking, got %q", got[0].Type)
	}
	if got[1].Type != MessageOutput || got[1].Content != "我来帮您查询上海未来三天的天气预报。" {
		t.Fatalf("unexpected first output message: %#v", got[1])
	}
	if got[2].Type != MessageTool || got[2].Content != "Skill (pending)\n  input: {}" {
		t.Fatalf("unexpected pending tool message: %#v", got[2])
	}
	if got[3].Type != MessageTool {
		t.Fatalf("expected completed tool message, got %#v", got[3])
	}
	if got[4].Type != MessageThinking {
		t.Fatalf("expected second thinking message, got %#v", got[4])
	}
	if got[5].Type != MessageOutput {
		t.Fatalf("expected final output message, got %#v", got[5])
	}
}

func TestParseStripsANSIEscapes(t *testing.T) {
	raw := "\x1b[1m[thinking]\x1b[0m hello\nplain output"

	got := Parse(raw)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d: %#v", len(got), got)
	}
	if got[0].Type != MessageThinking || got[0].Content != "hello" {
		t.Fatalf("unexpected thinking message: %#v", got[0])
	}
	if got[1].Type != MessageOutput || got[1].Content != "plain output" {
		t.Fatalf("unexpected output message: %#v", got[1])
	}
}

func TestParseIgnoresStatusOnlyTranscript(t *testing.T) {
	raw := `[acpx] session abc
[client] initialize (running)
[done] end_turn`

	got := Parse(raw)
	if len(got) != 0 {
		t.Fatalf("expected no messages, got %#v", got)
	}
}
