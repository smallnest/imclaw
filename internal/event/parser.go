package event

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Parser parses IMClaw transcript output and emits structured events.
// It recognizes fine-grained tool lifecycle events like tool_start and tool_end.
type Parser struct {
	buf         bytes.Buffer
	currentType Type
	currentBuf  bytes.Buffer
	// Tool state
	toolName      string
	toolInput     bytes.Buffer
	toolOutput    bytes.Buffer
	pendingTool   string // Tool name waiting for completion
	collectOutput bool   // Collecting tool output after (completed)
}

// NewParser creates a new Parser.
func NewParser() *Parser {
	return &Parser{}
}

// Parse converts a full IMClaw transcript into structured events.
func Parse(raw string) []Event {
	p := NewParser()
	events := p.Feed(raw)
	events = append(events, p.Flush()...)
	return events
}

// Feed consumes a transcript chunk and returns any completed events.
func (p *Parser) Feed(chunk string) []Event {
	chunk = normalizeChunk(chunk)
	if chunk == "" {
		return nil
	}

	p.buf.WriteString(chunk)

	var events []Event
	for {
		line, found := p.readLine()
		if !found {
			break
		}
		events = append(events, p.processLine(line)...)
	}

	return events
}

// Flush emits any remaining events from buffered content.
func (p *Parser) Flush() []Event {
	var events []Event

	// Process any remaining buffered content
	if p.buf.Len() > 0 {
		events = append(events, p.processLine(p.buf.String())...)
		p.buf.Reset()
	}

	// Flush any pending tool
	if p.pendingTool != "" {
		events = append(events, Event{
			Type:   TypeToolEnd,
			Name:   p.pendingTool,
			Input:  strings.TrimSpace(p.toolInput.String()),
			Output: strings.TrimSpace(p.toolOutput.String()),
		})
		p.pendingTool = ""
		p.toolInput.Reset()
		p.toolOutput.Reset()
	}

	// Flush current block
	if event, ok := p.flushCurrent(); ok {
		events = append(events, event)
	}

	return events
}

func (p *Parser) readLine() (string, bool) {
	b := p.buf.Bytes()
	idx := bytes.IndexByte(b, '\n')
	if idx < 0 {
		return "", false
	}
	line := string(b[:idx])
	p.buf.Next(idx + 1)
	return line, true
}

func (p *Parser) processLine(line string) []Event {
	var events []Event

	// If collecting tool output, check if this line continues it
	if p.collectOutput {
		if line == "" || startsWithWhitespace(line) {
			p.toolOutput.WriteString(line)
			p.toolOutput.WriteByte('\n')
			return events
		}
		// Non-indented line ends tool output
		events = append(events, Event{
			Type:   TypeToolEnd,
			Name:   p.pendingTool,
			Input:  strings.TrimSpace(p.toolInput.String()),
			Output: strings.TrimSpace(p.toolOutput.String()),
		})
		p.pendingTool = ""
		p.toolInput.Reset()
		p.toolOutput.Reset()
		p.collectOutput = false
	}

	// Check for marker: [type] content
	if markerType, content, isMarker := parseMarker(line); isMarker {
		// Flush any current block first
		if event, ok := p.flushCurrent(); ok {
			events = append(events, event)
		}

		switch markerType {
		case "thinking":
			p.currentType = TypeThinking
			if content != "" {
				p.currentBuf.WriteString(content)
			}

		case "tool":
			events = append(events, p.parseToolMarker(content)...)

		case "done":
			p.currentType = ""

		default:
			p.currentType = ""
		}

		return events
	}

	// Handle continuation lines based on current type
	switch p.currentType {
	case TypeThinking:
		if line == "" || startsWithWhitespace(line) {
			if p.currentBuf.Len() > 0 {
				p.currentBuf.WriteByte('\n')
			}
			p.currentBuf.WriteString(line)
			return events
		}
		// Non-indented line ends thinking block
		if event, ok := p.flushCurrent(); ok {
			events = append(events, event)
		}
		p.currentType = TypeOutput
		p.currentBuf.WriteString(line)

	case TypeToolInput:
		// Collect tool input lines (indented after tool_start)
		if startsWithWhitespace(line) || line == "" {
			p.toolInput.WriteString(line)
			p.toolInput.WriteByte('\n')
			return events
		}
		// Non-indented line ends tool input, start output
		p.currentType = TypeOutput
		p.currentBuf.WriteString(line)

	case TypeOutput:
		if p.currentBuf.Len() > 0 {
			p.currentBuf.WriteByte('\n')
		}
		p.currentBuf.WriteString(line)

	default:
		if strings.TrimSpace(line) == "" {
			return events
		}
		p.currentType = TypeOutput
		p.currentBuf.WriteString(line)
	}

	return events
}

// parseToolMarker parses a tool marker line and emits appropriate events.
func (p *Parser) parseToolMarker(content string) []Event {
	var events []Event
	content = strings.TrimSpace(content)

	var toolName, state string
	if strings.HasSuffix(content, "(pending)") {
		toolName = strings.TrimSpace(strings.TrimSuffix(content, "(pending)"))
		state = "pending"
	} else if strings.HasSuffix(content, "(completed)") {
		toolName = strings.TrimSpace(strings.TrimSuffix(content, "(completed)"))
		state = "completed"
	} else if strings.HasSuffix(content, "(error)") {
		toolName = strings.TrimSpace(strings.TrimSuffix(content, "(error)"))
		state = "error"
	} else {
		// Unknown format
		return []Event{{Type: TypeToolStart, Content: content}}
	}

	switch state {
	case "pending":
		p.toolName = toolName
		p.toolInput.Reset()
		p.toolOutput.Reset()
		p.currentType = TypeToolInput
		events = append(events, Event{
			Type: TypeToolStart,
			Name: toolName,
		})

	case "completed":
		// Emit tool_end after collecting indented output lines
		p.pendingTool = toolName
		p.collectOutput = true
		p.currentType = ""

	case "error":
		events = append(events, Event{
			Type:  TypeToolError,
			Name:  toolName,
			Input: strings.TrimSpace(p.toolInput.String()),
		})
		p.toolName = ""
		p.toolInput.Reset()
		p.toolOutput.Reset()
		p.currentType = ""
	}

	return events
}

func (p *Parser) flushCurrent() (Event, bool) {
	if p.currentType == "" {
		p.currentBuf.Reset()
		return Event{}, false
	}

	content := trimBlankLines(p.currentBuf.String())
	eventType := p.currentType

	p.currentType = ""
	p.currentBuf.Reset()

	if content == "" {
		return Event{}, false
	}

	return Event{
		Type:    eventType,
		Content: content,
	}, true
}

func parseMarker(line string) (markerType string, content string, isMarker bool) {
	if len(line) == 0 || line[0] != '[' {
		return "", "", false
	}

	end := strings.IndexByte(line, ']')
	if end < 1 {
		return "", "", false
	}

	markerType = line[1:end]
	if !isKnownMarker(markerType) {
		return "", "", false
	}
	content = strings.TrimPrefix(line[end+1:], " ")
	return markerType, content, true
}

func isKnownMarker(markerType string) bool {
	switch markerType {
	case "thinking", "tool", "done", "client", "acpx":
		return true
	default:
		return false
	}
}

func normalizeChunk(raw string) string {
	raw = stripANSI(raw)
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	return raw
}

func stripANSI(s string) string {
	if strings.IndexByte(s, '\x1b') < 0 {
		return s
	}

	var result bytes.Buffer
	result.Grow(len(s))

	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) {
				c := s[i]
				if c >= 0x40 && c <= 0x7e {
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
