package transcript

import (
	"regexp"
	"strings"
	"unicode"
)

type MessageType string

const (
	MessageThinking MessageType = "thinking"
	MessageTool     MessageType = "tool"
	MessageOutput   MessageType = "output"
)

type Message struct {
	Type    MessageType `json:"type"`
	Content string      `json:"content"`
}

var (
	ansiRegexp   = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	markerRegexp = regexp.MustCompile(`^\[([^\]]+)\]\s*(.*)$`)
)

// Parse converts a full IMClaw transcript into structured messages.
// It keeps only thinking, tool, and assistant output blocks.
func Parse(raw string) []Message {
	raw = normalize(raw)
	lines := strings.Split(raw, "\n")

	var messages []Message
	var currentType MessageType
	var currentLines []string

	flush := func() {
		if currentType == "" {
			currentLines = nil
			return
		}

		content := trimBlankLines(strings.Join(currentLines, "\n"))
		if content != "" {
			messages = append(messages, Message{
				Type:    currentType,
				Content: content,
			})
		}

		currentType = ""
		currentLines = nil
	}

	for _, line := range lines {
		if matches := markerRegexp.FindStringSubmatch(line); matches != nil {
			flush()

			switch matches[1] {
			case string(MessageThinking):
				currentType = MessageThinking
			case string(MessageTool):
				currentType = MessageTool
			default:
				currentType = ""
			}

			if currentType != "" && matches[2] != "" {
				currentLines = append(currentLines, matches[2])
			}
			continue
		}

		switch currentType {
		case MessageThinking, MessageTool:
			if line == "" || startsWithWhitespace(line) {
				currentLines = append(currentLines, line)
				continue
			}

			flush()
			currentType = MessageOutput
			currentLines = append(currentLines, line)
		case MessageOutput:
			currentLines = append(currentLines, line)
		default:
			if strings.TrimSpace(line) == "" {
				continue
			}
			currentType = MessageOutput
			currentLines = append(currentLines, line)
		}
	}

	flush()
	return messages
}

func normalize(raw string) string {
	raw = ansiRegexp.ReplaceAllString(raw, "")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	return raw
}

func trimBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}

	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}

	return strings.Join(lines[start:end], "\n")
}

func startsWithWhitespace(s string) bool {
	if s == "" {
		return false
	}
	r, _ := utf8DecodeRuneInString(s)
	return unicode.IsSpace(r)
}

func utf8DecodeRuneInString(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return rune(0), 0
}
