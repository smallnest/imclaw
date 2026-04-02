# Feature Issue Drafts

This file contains draft GitHub issues for the next major IMClaw feature set.

## 1. Structured event stream for gateway and CLI

**Title**
`feat: add structured event stream for gateway and CLI`

**Body**
IMClaw currently relies on transcript-like text output for many interactive features. This works, but it limits downstream consumers such as the CLI, web UI, and API clients.

We should introduce a structured event stream model that emits typed events instead of forcing clients to parse terminal transcript text.

### Goals
- Define typed events such as `thinking`, `tool_start`, `tool_end`, `output_delta`, `output_final`, and `error`
- Expose the structured event stream through the gateway
- Update `imclaw-cli` to consume structured events directly when available
- Preserve backward compatibility for current transcript-based consumers during migration

### Non-goals
- Rewriting all agent backends at once
- Removing transcript output immediately

### Deliverables
- Event schema definition
- Gateway streaming protocol update
- CLI integration
- Tests for event ordering and error handling

### Acceptance criteria
- A client can render agent execution without parsing transcript text
- Tool execution boundaries are explicit in the stream
- Final output and errors are distinguishable at the protocol level

---

## 2. Web UI for session and stream inspection

**Title**
`feat: add web UI for sessions, streams, and tool activity`

**Body**
IMClaw needs a first-class UI for observing sessions, prompts, streaming output, tool activity, and errors. This would make the gateway significantly easier to use in remote and collaborative environments.

### Goals
- Show active and historical sessions
- Show real-time streaming output
- Visualize `thinking`, tool execution, and final output separately
- Allow switching agents and creating new sessions
- Surface errors and permission requests clearly

### Deliverables
- Lightweight web frontend
- Real-time subscription to session events
- Session detail page
- Minimal responsive design for desktop and mobile

### Acceptance criteria
- A user can inspect and follow a live session from the browser
- A user can review previous sessions and their outputs
- Tool execution is visible and distinguishable from final output

---

## 3. Permission policy presets and tool-level controls

**Title**
`feat: add permission policy presets and tool-level controls`

**Body**
Current permission handling is useful but still too coarse for production or shared environments. IMClaw should support reusable policy presets and more explicit tool-level restrictions.

### Goals
- Add named presets such as `safe-readonly`, `dev-default`, and `full-auto`
- Allow tool-level allow/deny rules
- Make policy selection available in CLI and gateway requests
- Improve permission-related error messages and observability

### Deliverables
- Policy model
- CLI flags and gateway request fields
- Enforcement logic
- Tests for policy resolution and denial behavior

### Acceptance criteria
- A user can choose a named permission preset
- Tool execution can be restricted beyond the current coarse modes
- Policy failures are clearly reported

---

## 4. Session lifecycle features: rename, tag, archive, export

**Title**
`feat: improve session lifecycle with rename, tag, archive, and export`

**Body**
Sessions are one of IMClaw’s strongest primitives. They should be easier to organize, recover, and reuse over time.

### Goals
- Rename sessions
- Add tags and metadata
- Archive sessions without deleting them
- Export session history to markdown or JSON
- Re-import sessions where feasible

### Deliverables
- Session metadata model update
- CLI commands and API support
- Export formats
- Tests for lifecycle operations

### Acceptance criteria
- A session can be renamed and tagged
- Archived sessions remain retrievable
- A session can be exported for backup or review

---

## 5. Multi-subscriber session streaming

**Title**
`feat: support multiple subscribers for the same live session`

**Body**
A single IMClaw session should be observable by multiple clients at the same time, such as CLI, web UI, and monitoring components.

### Goals
- Allow more than one client to subscribe to the same live session stream
- Ensure event ordering remains stable for all subscribers
- Avoid coupling session lifetime to a single WebSocket connection

### Deliverables
- Session stream fan-out design
- Subscription management in gateway
- Backpressure and disconnect handling
- Tests for multi-subscriber behavior

### Acceptance criteria
- Multiple clients can observe the same session concurrently
- One subscriber disconnecting does not terminate the session
- Message ordering remains consistent

---

## 6. Background jobs and queued task execution

**Title**
`feat: add background jobs and queued task execution`

**Body**
IMClaw should support long-running or disconnected workflows by turning prompts into managed jobs instead of only foreground interactive requests.

### Goals
- Submit prompts as jobs
- Track queued, running, completed, failed, and canceled states
- Reattach to running jobs after disconnect
- Fetch logs and final results after completion

### Deliverables
- Job model and persistence
- CLI commands for submit/status/logs/cancel
- Gateway APIs for job lifecycle
- Tests for job transitions

### Acceptance criteria
- A user can submit a job and disconnect safely
- A user can later inspect status and retrieve output
- Jobs can be canceled and retried

---

## 7. Observability and execution metrics

**Title**
`feat: add observability for sessions, tools, and agent execution`

**Body**
IMClaw currently lacks strong visibility into performance and failure patterns. We should add metrics and execution tracing that make tuning and debugging practical.

### Goals
- Track request latency
- Track tool call counts and durations
- Track permission denials and agent failures
- Track output sizes and session activity

### Deliverables
- Metrics collection points
- Structured logs for key events
- Optional metrics endpoint or exporter
- Dashboard-friendly naming

### Acceptance criteria
- Operators can identify slow sessions and noisy tools
- Permission and execution failures are measurable
- Session activity is observable over time

---

## 8. Artifact-oriented result model

**Title**
`feat: support artifact-oriented results for files, reports, and generated outputs`

**Body**
Many useful agent tasks produce more than plain text. IMClaw should support structured result artifacts such as files, markdown reports, JSON outputs, and logs.

### Goals
- Let requests return structured artifacts in addition to text
- Represent artifact metadata in a consistent schema
- Support downloading or retrieving stored artifacts

### Deliverables
- Artifact schema
- Result transport updates
- Storage strategy for artifacts
- CLI and API support for inspecting artifacts

### Acceptance criteria
- A request can return files or structured outputs as first-class results
- Clients can distinguish plain text from attached artifacts
- Artifact retrieval is stable and documented

---

## 9. Agent adapter abstraction for multi-backend support

**Title**
`feat: formalize agent adapter layer for multiple backends`

**Body**
IMClaw already supports multiple agent types conceptually, but the adapter model can be made more explicit so additional backends remain maintainable.

### Goals
- Define a cleaner adapter contract for agent backends
- Isolate backend-specific session and stream behavior
- Make it easier to support non-`acpx` backends in the future

### Deliverables
- Adapter interface review
- Backend capability model
- Shared conformance tests for adapters

### Acceptance criteria
- Adding a new backend does not require invasive gateway or CLI changes
- Session and stream semantics are consistent across adapters where possible

---

## 10. Productized public API and SDK direction

**Title**
`feat: define productized API surface and SDK direction`

**Body**
If IMClaw is going to be integrated into other systems, it needs a clearer public API contract and a supported client experience.

### Goals
- Clarify stable gateway API boundaries
- Version the streaming and request/response schema
- Define SDK priorities for Go and JavaScript
- Improve documentation for integration use cases

### Deliverables
- Public API documentation
- Versioning strategy
- SDK design notes
- Compatibility guidance for existing clients

### Acceptance criteria
- External users can integrate without reverse engineering transcript behavior
- Breaking API changes are easier to manage
- SDK work has a clear starting point
