package transcript

import (
	"context"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
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

type Parser struct {
	buf          strings.Builder
	currentType  MessageType
	currentLines []string
}

var (
	ansiRegexp   = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	markerRegexp = regexp.MustCompile(`^\[([^\]]+)\]\s*(.*)$`)
)

func NewParser() *Parser {
	return &Parser{}
}

// Parse converts a full IMClaw transcript into structured messages.
// It keeps only thinking, tool, and assistant output blocks.
func Parse(raw string) []Message {
	p := NewParser()
	var messages []Message
	messages = append(messages, p.Feed(raw)...)
	messages = append(messages, p.Flush()...)
	return messages
}

// ParseStream incrementally parses transcript chunks from a channel and closes
// the returned message channel after the input channel is exhausted and any
// buffered content has been flushed.
func ParseStream(ctx context.Context, chunks <-chan string) <-chan Message {
	out := make(chan Message)

	go func() {
		defer close(out)

		p := NewParser()
		send := func(messages []Message) bool {
			for _, msg := range messages {
				select {
				case <-ctx.Done():
					return false
				case out <- msg:
				}
			}
			return true
		}

		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-chunks:
				if !ok {
					_ = send(p.Flush())
					return
				}
				if !send(p.Feed(chunk)) {
					return
				}
			}
		}
	}()

	return out
}

// Feed consumes a transcript chunk and returns any completed messages.
func (p *Parser) Feed(chunk string) []Message {
	chunk = normalizeChunk(chunk)
	if chunk == "" {
		return nil
	}

	p.buf.WriteString(chunk)

	var messages []Message
	for {
		buffered := p.buf.String()
		idx := strings.IndexByte(buffered, '\n')
		if idx < 0 {
			break
		}

		line := buffered[:idx]
		rest := buffered[idx+1:]
		p.buf.Reset()
		p.buf.WriteString(rest)

		messages = append(messages, p.processLine(line)...)
	}

	return messages
}

// Flush emits any final message, including a trailing partial line.
func (p *Parser) Flush() []Message {
	var messages []Message

	if p.buf.Len() > 0 {
		messages = append(messages, p.processLine(p.buf.String())...)
		p.buf.Reset()
	}

	if msg, ok := p.flushCurrent(); ok {
		messages = append(messages, msg)
	}

	return messages
}

func (p *Parser) processLine(line string) []Message {
	var messages []Message

	if matches := markerRegexp.FindStringSubmatch(line); matches != nil {
		if msg, ok := p.flushCurrent(); ok {
			messages = append(messages, msg)
		}

		switch matches[1] {
		case string(MessageThinking):
			p.currentType = MessageThinking
		case string(MessageTool):
			p.currentType = MessageTool
		default:
			p.currentType = ""
		}

		if p.currentType != "" && matches[2] != "" {
			p.currentLines = append(p.currentLines, matches[2])
		}
		return messages
	}

	switch p.currentType {
	case MessageThinking, MessageTool:
		if line == "" || startsWithWhitespace(line) {
			p.currentLines = append(p.currentLines, line)
			return messages
		}

		if msg, ok := p.flushCurrent(); ok {
			messages = append(messages, msg)
		}
		p.currentType = MessageOutput
		p.currentLines = append(p.currentLines, line)
	case MessageOutput:
		p.currentLines = append(p.currentLines, line)
	default:
		if strings.TrimSpace(line) == "" {
			return nil
		}
		p.currentType = MessageOutput
		p.currentLines = append(p.currentLines, line)
	}

	return messages
}

func (p *Parser) flushCurrent() (Message, bool) {
	if p.currentType == "" {
		p.currentLines = nil
		return Message{}, false
	}

	content := trimBlankLines(strings.Join(p.currentLines, "\n"))
	msgType := p.currentType

	p.currentType = ""
	p.currentLines = nil

	if content == "" {
		return Message{}, false
	}

	return Message{
		Type:    msgType,
		Content: content,
	}, true
}

func normalizeChunk(raw string) string {
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
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsSpace(r)
}
