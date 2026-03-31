# IMClaw

IMClaw 是一个支持 ACP 协议的 AI Agent 网关，通过 CLI 与 Agent 交互。

## 功能特性

- **ACP 协议**: 通过 acpx 支持 Claude、Codex 等 AI Agent
- **多会话管理**: 支持 `/new` 新建会话
- **多 Agent 支持**: 动态创建 Agent，通过命令切换
- **权限控制**: 支持多种权限模式
- **配置热更新**: JSON 配置文件，支持自动重载
- **统一网关**: HTTP 和 WebSocket 使用同一端口

## 快速开始

### 安装

```bash
# 克隆仓库
git clone https://github.com/smallnest/imclaw.git
cd imclaw

# 构建
make build

# 或者直接使用 go build
go build -o bin/imclaw ./cmd/imclaw
go build -o bin/imclaw-cli ./cmd/imclaw-cli
```

### 配置

1. 复制示例配置文件：

```bash
mkdir -p ~/.imclaw
cp config.example.json ~/.imclaw/config.json
```

2. 编辑配置文件：

```json
{
  "host": "0.0.0.0",
  "port": 8080,
  "timeout": 30,
  "auth_token": ""
}
```

配置说明：
- `host`: 服务监听地址
- `port`: 服务端口（HTTP 和 WebSocket 共用）
- `timeout`: 默认超时时间（秒）
- `auth_token`: 认证令牌，为空则不校验认证

### 运行

```bash
# 使用默认配置路径 (~/.imclaw/config.json)
./bin/imclaw

# 指定配置文件
./bin/imclaw -config /path/to/config.json
```

## CLI 工具

IMClaw 提供了命令行工具 `imclaw-cli`，可以直接与 Agent 交互。

### 构建 CLI

```bash
make build-cli
# 或
go build -o bin/imclaw-cli ./cmd/imclaw-cli
```

### 安装 CLI

```bash
make install-cli
# 安装后可以直接使用 imclaw-cli 命令
```

### 使用方式

#### 交互模式（REPL）

```bash
# 直接运行进入交互模式
./bin/imclaw-cli

# 指定 Agent
./bin/imclaw-cli --agent codex

# 使用指定的 Session ID
./bin/imclaw-cli --session my-session-123

# 指定权限模式和输出格式
./bin/imclaw-cli --approve-all --format json
```

#### 单条消息

```bash
# 使用 -p/--prompt 参数（推荐）
./bin/imclaw-cli -p "什么是 Go 语言？"
./bin/imclaw-cli --prompt "什么是 Go 语言？"

# 或直接传递消息
./bin/imclaw-cli "什么是 Go 语言？"

# 指定 Agent
./bin/imclaw-cli --agent codex -p "Hello"

# 使用指定 Session（可复用会话）
./bin/imclaw-cli --session my-session -p "继续之前的对话"

# JSON 输出格式
./bin/imclaw-cli --format json -p "Hello"

# 自动批准所有权限请求
./bin/imclaw-cli --approve-all -p "Hello"

# 只读模式（拒绝所有写操作）
./bin/imclaw-cli --deny-all -p "分析这段代码"
```

### CLI 参数

| 参数 | 说明 |
|------|------|
| `--server <url>` | IMClaw 服务器 WebSocket URL（默认：ws://localhost:8080/ws） |
| `--token <token>` | 认证令牌 |
| `-p, --prompt <message>` | 提示消息（单次执行模式） |
| `--session <id>` | 指定使用的 Session ID（为空则自动创建） |
| `--agent <type>` | Agent 类型（claude, codex 等） |
| `--cwd <dir>` | 工作目录 |
| `--auth-policy <policy>` | 认证策略：skip 或 fail |
| `--approve-all` | 自动批准所有权限请求 |
| `--approve-reads` | 自动批准读取请求，写入需要确认（默认） |
| `--deny-all` | 拒绝所有权限请求（只读模式） |
| `--non-interactive-permissions <policy>` | 非交互模式下的权限策略：deny 或 fail |
| `--format <fmt>` | 输出格式：text, json, quiet（默认：text） |
| `--suppress-reads` | 禁止输出原始读取文件内容 |
| `--model <id>` | Agent 模型 ID |
| `--allowed-tools <list>` | 允许的工具名称（逗号分隔）。不指定=允许所有，`""`=禁用所有 |
| `--max-turns <count>` | 会话最大轮次 |
| `--prompt-retries <count>` | 失败重试次数 |
| `--json-strict` | 严格 JSON 模式（需要 --format json） |
| `--timeout <seconds>` | 等待 Agent 响应的最大时间 |
| `--ttl <seconds>` | 队列所有者空闲 TTL |
| `--verbose` | 启用详细调试日志 |

### REPL 命令

在交互模式下，支持以下命令：

| 命令 | 说明 |
|------|------|
| `/new` | 创建新会话（清除上下文） |
| `/session` | 显示当前会话信息 |
| `/agent <name>` | 切换到指定的 Agent |
| `/agents` | 列出可用的 Agent |
| `/help` | 显示帮助 |
| `/quit` | 退出 CLI |

## API 端点

HTTP 和 WebSocket 服务共用同一端口：

| 端点 | 方法 | 说明 |
|------|------|------|
| `/health` | GET | 健康检查 |
| `/api/sessions` | GET | 获取所有会话 |
| `/api/agents` | GET | 获取所有 Agent |
| `/rpc` | POST | JSON-RPC 接口 |
| `/ws` | WebSocket | WebSocket 连接 |

## 项目结构

```
imclaw/
├── cmd/imclaw/           # 主程序入口
├── cmd/imclaw-cli/       # CLI 工具
├── internal/
│   ├── config/           # 配置管理
│   ├── session/          # 会话管理
│   ├── agent/            # ACP Agent 集成
│   └── gateway/          # HTTP/WebSocket 网关
├── config.example.json   # 示例配置
├── Makefile
└── README.md
```

## 依赖

- Go 1.21+
- acpx (用于 ACP 协议支持)

### 安装 acpx

```bash
npm install -g acpx@latest
```

## License

MIT License
