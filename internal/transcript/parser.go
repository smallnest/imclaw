package transcript

import (
	"bytes"
	"context"
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

// Parser incrementally parses IMClaw transcript output.
type Parser struct {
	buf         bytes.Buffer // 使用 bytes.Buffer 提高性能
	currentType MessageType
	currentBuf  bytes.Buffer // 当前消息内容的缓冲区
}

// NewParser creates a new Parser.
func NewParser() *Parser {
	return &Parser{}
}

// Parse converts a full IMClaw transcript into structured messages.
// It keeps only thinking, tool, and assistant output blocks.
func Parse(raw string) []Message {
	p := NewParser()
	messages := p.Feed(raw)
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
	// Normalize the chunk: remove ANSI escapes and normalize line endings
	chunk = normalizeChunk(chunk)
	if chunk == "" {
		return nil
	}

	p.buf.WriteString(chunk)

	var messages []Message

	// 逐行处理，避免频繁的字符串转换
	for {
		// 查找换行符
		line, found := p.readLine()
		if !found {
			break
		}
		messages = append(messages, p.processLine(line)...)
	}

	return messages
}

// readLine reads a single line from the buffer without allocating a new string
// for the entire buffer contents each time.
func (p *Parser) readLine() (line string, found bool) {
	b := p.buf.Bytes()
	idx := bytes.IndexByte(b, '\n')
	if idx < 0 {
		return "", false
	}

	line = string(b[:idx])
	// 保留换行符后的内容
	p.buf.Next(idx + 1)
	return line, true
}

// Flush emits any final message, including a trailing partial line.
func (p *Parser) Flush() []Message {
	var messages []Message

	// 处理缓冲区中剩余的内容（没有换行符结尾）
	if p.buf.Len() > 0 {
		messages = append(messages, p.processLine(p.buf.String())...)
		p.buf.Reset()
	}

	if msg, ok := p.flushCurrent(); ok {
		messages = append(messages, msg)
	}

	return messages
}

// processLine processes a single line and returns any completed messages.
func (p *Parser) processLine(line string) []Message {
	var messages []Message

	// Check for marker: [type] content
	if markerType, content, isMarker := parseMarker(line); isMarker {
		if msg, ok := p.flushCurrent(); ok {
			messages = append(messages, msg)
		}

		switch markerType {
		case "thinking":
			p.currentType = MessageThinking
		case "tool":
			p.currentType = MessageTool
		default:
			p.currentType = ""
		}

		if p.currentType != "" && content != "" {
			p.currentBuf.WriteString(content)
		}
		return messages
	}

	switch p.currentType {
	case MessageThinking, MessageTool:
		if line == "" || startsWithWhitespace(line) {
			if p.currentBuf.Len() > 0 {
				p.currentBuf.WriteByte('\n')
			}
			p.currentBuf.WriteString(line)
			return messages
		}

		// Non-indented line ends thinking/tool block, starts output
		if msg, ok := p.flushCurrent(); ok {
			messages = append(messages, msg)
		}
		p.currentType = MessageOutput
		p.currentBuf.WriteString(line)

	case MessageOutput:
		if p.currentBuf.Len() > 0 {
			p.currentBuf.WriteByte('\n')
		}
		p.currentBuf.WriteString(line)

	default:
		if strings.TrimSpace(line) == "" {
			return nil
		}
		p.currentType = MessageOutput
		p.currentBuf.WriteString(line)
	}

	return messages
}

// parseMarker parses a line for transcript markers like [thinking], [tool], etc.
// Returns the marker type, content after marker, and whether it was a marker.
func parseMarker(line string) (markerType string, content string, isMarker bool) {
	// Fast path: check if line starts with '['
	if len(line) == 0 || line[0] != '[' {
		return "", "", false
	}

	// Find closing bracket
	end := strings.IndexByte(line, ']')
	if end < 1 {
		return "", "", false
	}

	markerType = line[1:end]
	// Skip '] ' prefix
	content = strings.TrimPrefix(line[end+1:], " ")
	return markerType, content, true
}

func (p *Parser) flushCurrent() (Message, bool) {
	if p.currentType == "" {
		p.currentBuf.Reset()
		return Message{}, false
	}

	content := trimBlankLines(p.currentBuf.String())
	msgType := p.currentType

	p.currentType = ""
	p.currentBuf.Reset()

	if content == "" {
		return Message{}, false
	}

	return Message{
		Type:    msgType,
		Content: content,
	}, true
}

func normalizeChunk(raw string) string {
	// Remove ANSI escape sequences
	raw = stripANSI(raw)
	// Normalize line endings
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	return raw
}

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	// Fast path: check if there are any escape sequences
	if strings.IndexByte(s, '\x1b') < 0 {
		return s
	}

	var result bytes.Buffer
	result.Grow(len(s))

	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			// Skip ANSI sequence: ESC [ ... letter
			i += 2
			for i < len(s) {
				c := s[i]
				if c >= 0x40 && c <= 0x7e { // '@' to '~'
					i++
					break
				}
				i++
			}
		} else {
			result.WriteByte(s[i])
			i++
		}
	}

	return result.String()
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
