package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ExportFormat represents the format for session export.
type ExportFormat string

const (
	ExportJSON     ExportFormat = "json"
	ExportMarkdown ExportFormat = "markdown"
)

// CurrentExportVersion is the version of the export format.
const CurrentExportVersion = "1.0"

// ExportData is the structured representation used for JSON exports.
type ExportData struct {
	ExportedAt time.Time `json:"exported_at"`
	Format     string    `json:"format"`
	Version    string    `json:"version"`
	Session    *Session  `json:"session"`
}

// ExportSession exports a session to the specified format, returning the serialized bytes.
func ExportSession(sess *Session, format ExportFormat) ([]byte, error) {
	if sess == nil {
		return nil, fmt.Errorf("session is nil")
	}

	switch format {
	case ExportJSON:
		return exportJSON(sess)
	case ExportMarkdown:
		return exportMarkdown(sess)
	default:
		return nil, fmt.Errorf("unsupported export format: %s", format)
	}
}

func exportJSON(sess *Session) ([]byte, error) {
	data := ExportData{
		ExportedAt: time.Now(),
		Format:     "json",
		Version:    CurrentExportVersion,
		Session:    sess,
	}
	return json.MarshalIndent(data, "", "  ")
}

func exportMarkdown(sess *Session) ([]byte, error) {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# Session: %s\n\n", sessionDisplayName(sess)))

	b.WriteString(fmt.Sprintf("- **ID**: %s\n", sess.ID))
	b.WriteString(fmt.Sprintf("- **Agent**: %s\n", sess.AgentName))
	if sess.Channel != "" {
		b.WriteString(fmt.Sprintf("- **Channel**: %s\n", sess.Channel))
	}
	b.WriteString(fmt.Sprintf("- **Status**: %s\n", sess.Status))
	b.WriteString(fmt.Sprintf("- **Created**: %s\n", sess.CreatedAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("- **Last Active**: %s\n", sess.LastActive.Format(time.RFC3339)))
	if len(sess.Tags) > 0 {
		b.WriteString(fmt.Sprintf("- **Tags**: %s\n", strings.Join(sess.Tags, ", ")))
	}
	if sess.Archived {
		b.WriteString("- **Archived**: yes\n")
	}

	b.WriteString("\n")

	if len(sess.Activity) > 0 {
		b.WriteString("## Activity\n\n")
		for _, a := range sess.Activity {
			ts := a.Timestamp.Format(time.RFC3339)
			switch a.Type {
			case ActivityPrompt:
				b.WriteString(fmt.Sprintf("### [%s] Prompt @ %s\n\n%s\n\n", a.RequestID, ts, a.Prompt))
			case ActivityResult:
				b.WriteString(fmt.Sprintf("### [%s] Result @ %s\n\n%s\n\n", a.RequestID, ts, a.Content))
			case ActivityError:
				b.WriteString(fmt.Sprintf("### [%s] Error @ %s\n\n%s\n\n", a.RequestID, ts, a.Error))
			case ActivityEvent:
				b.WriteString(fmt.Sprintf("### [%s] Event @ %s\n\n", a.RequestID, ts))
				if a.Event != nil {
					if a.Event.Name != "" {
						b.WriteString(fmt.Sprintf("- **Name**: %s\n", a.Event.Name))
					}
					if a.Event.Input != "" {
						b.WriteString(fmt.Sprintf("- **Input**: %s\n", a.Event.Input))
					}
					if a.Event.Output != "" {
						b.WriteString(fmt.Sprintf("- **Output**: %s\n", a.Event.Output))
					}
					if a.Content != "" {
						b.WriteString(fmt.Sprintf("\n%s\n", a.Content))
					}
				} else if a.Content != "" {
					b.WriteString(a.Content)
				}
				b.WriteString("\n")
			}
		}
	}

	return []byte(b.String()), nil
}

// ImportSession imports a session from JSON export data.
func ImportSession(data []byte) (*Session, error) {
	var exportData ExportData
	if err := json.Unmarshal(data, &exportData); err != nil {
		return nil, fmt.Errorf("invalid export data: %w", err)
	}
	if exportData.Session == nil {
		return nil, fmt.Errorf("export data contains no session")
	}
	if exportData.Version != "" && exportData.Version != CurrentExportVersion {
		return nil, fmt.Errorf("unsupported export version %q (expected %q)", exportData.Version, CurrentExportVersion)
	}
	return exportData.Session, nil
}

func sessionDisplayName(sess *Session) string {
	if sess.Name != "" {
		return sess.Name
	}
	return sess.ID
}
