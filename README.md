# IMClaw

IMClaw 是一个支持 ACP 协议的 AI Agent 网关，通过 CLI 与 Agent 交互。

**重要**:
本项目演化出来的 autoresearch 独立成一个单独的项目:[smallnest/autoresearch](https://github.com/smallnest/autoresearch), 更智能更强大。

## 功能特性

- **ACP 协议**: 通过 acpx 支持 Claude、Codex 等 AI Agent
- **多会话管理**: 支持 `/new` 新建会话
- **多 Agent 支持**: 动态创建 Agent，通过命令切换
- **权限控制**: 支持多种权限模式
- **统一网关**: HTTP 和 WebSocket 使用同一端口

## 安装

### 方式一：下载预编译二进制

从 [Releases](https://github.com/smallnest/imclaw/releases) 页面下载对应平台的二进制文件。

### 方式二：使用 Go 安装

```bash
go install github.com/smallnest/imclaw/cmd/imclaw@latest
go install github.com/smallnest/imclaw/cmd/imclaw-cli@latest
```

### 方式三：从源码构建

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

## 快速开始

### 运行服务器

```bash
# 使用默认参数
./bin/imclaw

# 指定端口和认证令牌
./bin/imclaw --port 9000 --token my-secret-token

# 查看帮助
./bin/imclaw --help
```

### 服务器参数

| 参数 | 简写 | 默认值 | 说明 |
|------|------|--------|------|
| `--host` | `-H` | `0.0.0.0` | 服务监听地址 |
| `--port` | `-p` | `8080` | 服务端口 |
| `--timeout` | | `30` | 默认超时时间（秒） |
| `--token` | | `""` | 认证令牌（为空则不校验认证） |
| `--version` | | | 显示版本信息 |

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
| `--permission-preset <preset>` | 权限预设：safe-readonly, dev-default (默认), full-auto |
| `--allowed-tools <list>` | 允许的工具名称（逗号分隔）。默认：Bash,Read,Write。空字符串=允许所有 |
| `--denied-tools <list>` | 拒绝的工具名称（逗号分隔），优先级高于 allowed-tools |
| `--max-turns <count>` | 会话最大轮次 |
| `--prompt-retries <count>` | 失败重试次数 |
| `--json-strict` | 严格 JSON 模式（需要 --format json） |
| `--timeout <seconds>` | 等待 Agent 响应的最大时间 |
| `--ttl <seconds>` | 队列所有者空闲 TTL |
| `--verbose` | 启用详细调试日志 |


## 权限控制指南

IMClaw 提供六维权限控制参数，可以灵活组合实现从粗粒度到细粒度的权限管理。

### 六个权限参数概览

| 参数 | CLI Flag | 说明 | 示例值 |
|------|----------|------|--------|
| **Permissions** | `--approve-all` / `--approve-reads` / `--deny-all` | 权限模式，控制是否自动批准操作 | `approve-all` |
| **PermissionPreset** | `--permission-preset` | 预设策略，一组预配置的权限组合 | `safe-readonly` |
| **AllowedTools** | `--allowed-tools` | 工具白名单 | `Bash,Read,Write` |
| **DeniedTools** | `--denied-tools` | 工具黑名单，从白名单中剔除 | `Write,Bash` |
| **AuthPolicy** | `--auth-policy` | 认证策略，控制认证失败时的行为 | `skip` / `fail` |
| **NonInteractivePerms** | `--non-interactive-permissions` | 非交互模式权限策略 | `deny` / `fail` |

---

### 1. PermissionPreset（预设策略）

预设是一组开箱即用的权限组合，适合快速配置。

| 预设名 | Permissions | AllowedTools | 适用场景 |
|--------|-------------|--------------|----------|
| `safe-readonly` | `deny-all` | Glob, Grep, LS, Read | 安全阅读代码，无任何写操作风险 |
| `dev-default` | `approve-reads` | Bash, Read, Write | 日常开发，读写自动批准 |
| `full-auto` | `approve-all` | 所有已知工具 | 全自动模式，适合可信环境 |

**不指定预设时**，默认等效于 `dev-default`（`approve-reads` + `Bash,Read,Write`）。

```bash
# 安全只读模式
imclaw-cli --permission-preset safe-readonly -p "分析这段代码"

# 全自动模式
imclaw-cli --permission-preset full-auto -p "帮我重构代码"
```

---

### 2. Permissions（权限模式）

权限模式控制 Agent 执行操作时是否需要用户确认。

| Flag | 行为 | 适用场景 |
|------|------|----------|
| `--approve-all` | 所有操作自动批准，无需确认 | 可信环境、自动化脚本 |
| `--approve-reads` | 读取操作自动批准，写入操作需确认（默认） | 日常开发，防止误操作 |
| `--deny-all` | 拒绝所有需要权限的操作 | 只读分析、安全审计 |

**注意**：这三个 flag 是互斥的，后设置的会覆盖前面的。

```bash
# 自动批准所有操作
imclaw-cli --approve-all -p "帮我重构代码"

# 只读模式，拒绝所有写操作
imclaw-cli --deny-all -p "分析项目结构"
```

---

### 3. AllowedTools（工具白名单）

精确控制 Agent 可以使用的工具。不指定时使用预设的工具列表。

**支持的工具列表**：
```
Bash          # 执行 shell 命令
Edit          # 编辑文件
Glob          # 文件模式匹配
Grep          # 文件内容搜索
LS            # 列出目录
MultiEdit     # 批量编辑文件
NotebookEdit  # 编辑 Jupyter Notebook
Read          # 读取文件
TodoWrite     # 写入待办事项
WebFetch      # 抓取网页
WebSearch     # 搜索网页
Write         # 写入文件
```

```bash
# 只允许读取和搜索
imclaw-cli --allowed-tools Read,Grep,Glob -p "分析代码"

# 允许所有工具（空字符串）
imclaw-cli --allowed-tools "" -p "任意操作"

# 只允许执行命令
imclaw-cli --allowed-tools Bash -p "运行测试"
```

---

### 4. DeniedTools（工具黑名单）

从 AllowedTools 中剔除指定工具，实现"允许大部分但排除某些"的效果。

**优先级**：`DeniedTools` > `AllowedTools`

```bash
# 允许所有工具，但禁止写入文件
imclaw-cli --permission-preset full-auto --denied-tools Write -p "帮我分析代码"

# 允许所有工具，但禁止执行命令和写入
imclaw-cli --permission-preset full-auto --denied-tools Write,Bash -p "只读分析"

# 组合使用：白名单 + 黑名单
imclaw-cli --allowed-tools Bash,Read,Write,Edit --denied-tools Write -p "可以编辑但不能新建文件"
```

---

### 5. AuthPolicy（认证策略）

控制当 Agent 需要认证时的行为。

| 值 | 行为 |
|----|------|
| `skip` | 跳过认证要求，继续执行 |
| `fail` | 认证失败时报错停止 |

```bash
# 跳过认证
imclaw-cli --auth-policy skip -p "Hello"

# 认证失败时报错
imclaw-cli --auth-policy fail -p "Hello"
```

---

### 6. NonInteractivePerms（非交互模式权限）

控制在无法提示用户确认时的行为（如管道输入、脚本执行）。

| 值 | 行为 |
|----|------|
| `deny` | 自动拒绝所有权限请求 |
| `fail` | 报错停止执行 |

```bash
# 脚本中自动拒绝权限请求
echo "分析代码" | imclaw-cli --non-interactive-permissions deny

# 脚本中遇到权限请求时报错
imclaw-cli --non-interactive-permissions fail -p "Hello" < /dev/null
```

---

### 参数优先级与组合规则

参数按以下顺序解析，后者的设置会覆盖前者：

```
1. PermissionPreset    → 提供基准配置
2. Permissions         → 覆盖预设的权限模式
3. AllowedTools        → 覆盖预设的工具白名单
4. DeniedTools         → 从当前白名单中剔除
5. AuthPolicy          → 独立生效
6. NonInteractivePerms → 独立生效
```

**解析流程示例**：

```bash
imclaw-cli \
  --permission-preset full-auto \      # 1. 基准: approve-all, 所有工具
  --approve-reads \                    # 2. 覆盖权限模式为 approve-reads
  --denied-tools Write,Bash \          # 4. 从工具列表中剔除 Write 和 Bash
  -p "Hello"

# 最终结果:
# - Permissions: approve-reads (读取自动批准，写入需确认)
# - AllowedTools: 所有工具 - {Write, Bash}
# - Agent 可以读取、搜索、编辑，但不能新建文件或执行命令
```

---

### 典型使用场景

#### 场景 1：安全代码审查（只读）

```bash
# 最严格：只允许读取和搜索，拒绝所有操作确认
imclaw-cli --permission-preset safe-readonly -p "审查这段代码的安全性"

# 等效于
imclaw-cli --deny-all --allowed-tools Glob,Grep,LS,Read -p "审查代码"
```

#### 场景 2：日常开发（默认推荐）

```bash
# 读取自动批准，写入需确认
imclaw-cli --permission-preset dev-default -p "帮我实现这个功能"

# 等效于
imclaw-cli --approve-reads --allowed-tools Bash,Read,Write -p "实现功能"
```

#### 场景 3：自动化脚本（无交互）

```bash
# 全自动 + 非交互模式拒绝权限请求 = 纯自动执行
imclaw-cli \
  --permission-preset full-auto \
  --non-interactive-perms deny \
  -p "运行测试并修复失败的用例"
```

#### 场景 4：受限开发（可以编辑但不能执行命令）

```bash
imclaw-cli \
  --permission-preset full-auto \
  --denied-tools Bash \
  -p "帮我重构代码"

# Agent 可以编辑文件，但不能执行 shell 命令
```

#### 场景 5：CI/CD 环境

```bash
# 完全自动化 + 认证跳过
imclaw-cli \
  --approve-all \
  --auth-policy skip \
  --non-interactive-perms deny \
  --format json \
  -p "检查代码并生成报告"
```

---

### 权限决策流程图

```
Agent 请求执行操作
        │
        ▼
┌───────────────────┐
│ 检查 NonInteractive │
│ (非交互模式?)       │
└─────────┬─────────┘
          │
    ┌─────┴─────┐
    ▼           ▼
  是           否
    │           │
    ▼           ▼
┌─────────┐  ┌─────────────┐
│ deny    │  │ 检查 Auth   │
│ 或 fail │  │ Policy      │
└─────────┘  └──────┬──────┘
                    │
              ┌─────┴─────┐
              ▼           ▼
            skip        fail
              │           │
              ▼           ▼
       ┌─────────────────────┐
       │ 检查工具是否在      │
       │ AllowedTools 中     │
       └──────────┬──────────┘
                  │
            ┌─────┴─────┐
            ▼           ▼
          允许        拒绝
            │           │
            ▼           ▼
       ┌─────────────────────┐
       │ 根据 Permissions    │
       │ 决定是否需要确认     │
       └──────────┬──────────┘
                  │
         ┌────────┼────────┐
         ▼        ▼        ▼
    approve-all approve-reads deny-all
         │        │        │
         ▼        ▼        ▼
      自动批准  读自动    拒绝
               写确认
```

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


## 后台任务 API

IMClaw 支持后台任务（Job）执行，允许异步提交任务并查询状态。适用于长时间运行的 Agent 任务。

### Job 状态

| 状态 | 说明 |
|------|------|
| `queued` | 任务在队列中等待执行 |
| `running` | 任务正在执行中 |
| `completed` | 任务执行成功 |
| `failed` | 任务执行失败 |
| `canceled` | 任务被用户取消 |

### REST API

#### 提交任务

```bash
POST /api/jobs
Content-Type: application/json

{
  "prompt": "帮我分析项目结构",
  "agent": "codex"
}
```

**响应：**
```json
{
  "id": "019d527d-34ed-7272-b336-f67fd49024db",
  "status": "queued",
  "prompt": "帮我分析项目结构",
  "agent_name": "codex",
  "created_at": "2026-04-03T16:37:10Z"
}
```

#### 查询任务状态

```bash
GET /api/jobs/{job_id}
```

**响应：**
```json
{
  "id": "019d527d-34ed-7272-b336-f67fd49024db",
  "status": "completed",
  "prompt": "帮我分析项目结构",
  "agent_name": "codex",
  "created_at": "2026-04-03T16:37:10Z",
  "started_at": "2026-04-03T16:37:12Z",
  "finished_at": "2026-04-03T16:37:45Z",
  "result": "项目结构分析结果...",
  "logs": [
    {"timestamp": "2026-04-03T16:37:10Z", "level": "info", "message": "Job submitted"},
    {"timestamp": "2026-04-03T16:37:12Z", "level": "info", "message": "Job started"},
    {"timestamp": "2026-04-03T16:37:45Z", "level": "info", "message": "Job completed successfully"}
  ]
}
```

#### 列出所有任务

```bash
GET /api/jobs
```

**响应：**
```json
{
  "jobs": [
    {
      "id": "019d527d-34ed-7272-b336-f67fd49024db",
      "status": "completed",
      "prompt": "帮我分析项目结构",
      "agent_name": "codex",
      "created_at": "2026-04-03T16:37:10Z"
    }
  ]
}
```

#### 取消任务

```bash
DELETE /api/jobs/{job_id}
```

### JSON-RPC API

通过 WebSocket 连接使用 JSON-RPC 协议：

#### 提交任务

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "job.submit",
  "params": {
    "prompt": "帮我分析项目结构",
    "agent": "codex"
  }
}
```

#### 查询任务

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "job.get",
  "params": {
    "job_id": "019d527d-34ed-7272-b336-f67fd49024db"
  }
}
```

#### 列出任务

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "job.list"
}
```

#### 取消任务

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "job.cancel",
  "params": {
    "job_id": "019d527d-34ed-7272-b336-f67fd49024db"
  }
}
```

#### 删除任务

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "job.delete",
  "params": {
    "job_id": "019d527d-34ed-7272-b336-f67fd49024db"
  }
}
```

### 使用示例

#### curl 调用

```bash
# 提交任务
JOB_ID=$(curl -s -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{"prompt": "分析项目结构", "agent": "codex"}' | jq -r '.id')

echo "Job ID: $JOB_ID"

# 查询状态
curl -s http://localhost:8080/api/jobs/$JOB_ID | jq .

# 取消任务
curl -X DELETE http://localhost:8080/api/jobs/$JOB_ID
```

#### Python 调用

```python
import requests
import time

# 提交任务
response = requests.post('http://localhost:8080/api/jobs', json={
    'prompt': '分析项目结构',
    'agent': 'codex'
})
job = response.json()
job_id = job['id']
print(f"Job ID: {job_id}")

# 轮询等待完成
while True:
    response = requests.get(f'http://localhost:8080/api/jobs/{job_id}')
    job = response.json()
    status = job['status']
    print(f"Status: {status}")

    if status in ['completed', 'failed', 'canceled']:
        break

    time.sleep(2)

# 获取结果
if job['status'] == 'completed':
    print("Result:", job['result'])
else:
    print("Error:", job.get('error', 'Unknown error'))
```

#### JavaScript 调用

```javascript
// WebSocket 连接
const ws = new WebSocket('ws://localhost:8080/ws');

ws.onopen = () => {
  // 提交任务
  ws.send(JSON.stringify({
    jsonrpc: '2.0',
    id: 1,
    method: 'job.submit',
    params: {
      prompt: '分析项目结构',
      agent: 'codex'
    }
  }));
};

ws.onmessage = (event) => {
  const response = JSON.parse(event.data);
  console.log('Response:', response);

  if (response.id === 1) {
    const jobId = response.result.id;
    // 定期查询状态
    setInterval(() => {
      ws.send(JSON.stringify({
        jsonrpc: '2.0',
        id: 2,
        method: 'job.get',
        params: { job_id: jobId }
      }));
    }, 2000);
  }
};
```

### Job 数据结构

```go
type Job struct {
    ID         string     `json:"id"`
    Status     JobStatus  `json:"status"`
    Prompt     string     `json:"prompt"`
    AgentName  string     `json:"agent_name"`
    CreatedAt  time.Time  `json:"created_at"`
    StartedAt  *time.Time `json:"started_at,omitempty"`
    FinishedAt *time.Time `json:"finished_at,omitempty"`
    Result     string     `json:"result,omitempty"`
    Error      string     `json:"error,omitempty"`
    Logs       []LogEntry `json:"logs,omitempty"`
}

type LogEntry struct {
    Timestamp time.Time `json:"timestamp"`
    Level     string    `json:"level"`
    Message   string    `json:"message"`
}
```

### 状态转换

```
         ┌─────────┐
         │ queued  │
         └────┬────┘
              │
    ┌─────────┼─────────┐
    ▼         ▼         │
┌────────┐ ┌────────┐   │
│running │ │canceled│   │
└───┬────┘ └────────┘   │
    │                    │
    ├─────────┬─────────┤
    ▼         ▼         ▼
┌──────────┐ ┌───────┐ ┌────────┐
│completed │ │failed │ │canceled│
└──────────┘ └───────┘ └────────┘
              │
              ▼ (允许重试)
          ┌────────┐
          │ queued │
          └────────┘
```

- Go 1.25.0+
- acpx (用于 ACP 协议支持)

### 安装 acpx

```bash
npm install -g acpx@latest
```

## License

MIT License
