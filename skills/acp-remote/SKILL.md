---
name: acp-remote
description: Connect to remote ACP server and execute commands via imclaw-cli.
homepage: https://github.com/smallnest/imclaw
version: 1.0.0
metadata: {"clawdbot":{"emoji":"🤖","requires":{"bins":["imclaw-cli"]}}}
---

# ACP Remote

Connect to a remote ACP (Agent Client Protocol) server and execute commands using `imclaw-cli`.

## Prerequisites

1. Install `imclaw-cli`:
```bash
# Clone and build
git clone https://github.com/smallnest/imclaw.git
cd imclaw
make build-cli

# Or install to PATH
make install-cli
```

2. Ensure `imclaw-cli` is in your PATH or specify full path.

## Configuration

Set the remote server URL and optional auth token:

```bash
# Default server (can be omitted if using default)
export IMCLAW_SERVER="ws://your-server:8080/ws"

# Auth token (if server requires authentication)
export IMCLAW_TOKEN="your-secret-token"
```

Or pass directly via command line.

## Usage

### One-shot mode (recommended)

```bash
# Basic usage
imclaw-cli --server ws://your-server:8080/ws -p "Hello"

# With auth token
imclaw-cli --server ws://your-server:8080/ws --token your-token -p "Hello"

# Use specific agent
imclaw-cli --server ws://your-server:8080/ws --agent codex -p "Analyze this code"

# JSON output
imclaw-cli --server ws://your-server:8080/ws --format json -p "List files"
```

### Continue a session

```bash
# First message creates session, returns session ID
imclaw-cli --server ws://your-server:8080/ws -p "Read the README.md"

# Continue with same session
imclaw-cli --server ws://your-server:8080/ws --session <session-id> -p "Summarize it"
```

## Parameters

| Parameter | Description |
|-----------|-------------|
| `--server` | WebSocket URL of ACP server (default: ws://localhost:8080/ws) |
| `--token` | Authentication token (if required) |
| `-p, --prompt` | Prompt message (one-shot mode) |
| `--session` | Session ID to continue |
| `--agent` | Agent type: claude, codex, etc. (default: claude) |
| `--format` | Output format: text, json, quiet (default: text) |
| `--approve-all` | Auto-approve all permission requests |
| `--approve-reads` | Auto-approve reads, prompt for writes (default) |
| `--deny-all` | Deny all permission requests |
| `--allowed-tools` | Comma-separated tool names (default: Bash,Read,Write) |
| `--cwd` | Working directory |
| `--timeout` | Max wait time in seconds |
| `--model` | Agent model ID |

## Examples

### Execute shell commands on remote

```bash
imclaw-cli --server ws://remote-server:8080/ws \
  --agent claude \
  --approve-all \
  -p "Run 'df -h' and show disk usage"
```

### Analyze remote codebase

```bash
imclaw-cli --server ws://remote-server:8080/ws \
  --cwd /path/to/project \
  -p "Review the main.go file and suggest improvements"
```

### Use with SSH tunnel

```bash
# Create SSH tunnel first
ssh -L 8080:localhost:8080 user@remote-server -N &

# Connect via localhost
imclaw-cli --server ws://localhost:8080/ws -p "Hello from remote"
```

### Secure connection with token

```bash
# Server configured with auth_token
imclaw-cli \
  --server wss://secure-server:8080/ws \
  --token "your-secret-token" \
  -p "Execute analysis task"
```

## Environment Variables

For convenience, you can set these environment variables:

```bash
# In ~/.bashrc or ~/.zshrc
export IMCLAW_SERVER="ws://your-server:8080/ws"
export IMCLAW_TOKEN="your-token"
```

Then simply run:
```bash
imclaw-cli --server $IMCLAW_SERVER --token $IMCLAW_TOKEN -p "Hello"
```

## Tips

1. **Session reuse**: Save the session ID from first response to continue conversations
2. **Permission control**: Use `--approve-all` for non-interactive automation
3. **Output parsing**: Use `--format json` for programmatic consumption
4. **Tool restrictions**: Use `--allowed-tools` to limit what the agent can do
5. **Working directory**: Always specify `--cwd` for file operations
