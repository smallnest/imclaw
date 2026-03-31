# IMClaw：打造你的 AI Agent 远程网关

> 一行命令，让 AI Agent 在远程服务器上为你工作

## 为什么需要 IMClaw？

如果你是一名开发者，相信你已经体验过 Claude Code、Cursor 等 AI 编程助手的强大能力。但当你的项目在远程服务器上，或者你需要在多台机器之间协作时，问题就来了：

**痛点一：远程服务器上的 AI 助手怎么用？**

本地跑 Claude Code 很香，但生产环境在云端服务器上。想用 AI 分析线上日志、调试远程代码？抱歉，得先 SSH 上去，再想办法装 AI 工具——这一套下来，环境配置就能劝退不少人。

**痛点二：多个 AI Agent 怎么统一管理？**

Claude、Codex、各种 AI 工具轮番上阵，每个都有自己的 CLI 和配置方式。想在不同 Agent 之间切换？想复用会话上下文？想控制权限？这些都得自己折腾。

**痛点三：安全认证怎么办？**

把 AI Agent 暴露到网络上，安全问题怎么解决？谁都能调用的 AI Agent，跟裸奔的服务器有什么区别？

**IMClaw 就是为了解决这些问题而生的。**

## IMClaw 是什么？

IMClaw 是一个支持 **ACP 协议**的 AI Agent 网关，核心能力包括：

- 🚀 **远程访问**：通过 CLI 即可连接远程服务器上的 AI Agent
- 🔐 **安全认证**：支持 Token 认证，保护你的 AI Agent 不被滥用
- 💬 **多会话管理**：会话可复用，上下文不丢失
- 🤖 **多 Agent 支持**：Claude、Codex 等多种 Agent，一键切换
- ⚡ **轻量部署**：单个二进制文件，无需配置文件，开箱即用

架构非常简单：

```
┌─────────────┐     WebSocket      ┌─────────────┐     ACP      ┌─────────────┐
│ imclaw-cli  │ ◄─────────────────► │   imclaw    │ ◄──────────► │   acpx      │
│  (本地CLI)  │                     │  (网关服务) │              │ (AI Agent)  │
└─────────────┘                     └─────────────┘              └─────────────┘
```

## 快速安装

### 安装 acpx

acpx 是 ACP 协议的命令行工具，支持 Claude、Codex 等 AI Agent：

```bash
npm install -g acpx@latest
```

### 安装 IMClaw

三种方式任选：

**方式一：下载预编译二进制（推荐）**

从 [GitHub Releases](https://github.com/smallnest/imclaw/releases) 下载对应平台的压缩包，解压即可使用。

**方式二：一键安装脚本**

```bash
curl -fsSL https://raw.githubusercontent.com/smallnest/imclaw/main/scripts/install.sh | bash
```

**方式三：Go 安装**

```bash
go install github.com/smallnest/imclaw/cmd/imclaw@latest
go install github.com/smallnest/imclaw/cmd/imclaw-cli@latest
```

## 五分钟上手

### 第一步：启动网关服务

在远程服务器上启动 imclaw：

```bash
# 默认配置启动（监听 0.0.0.0:8080）
imclaw

# 指定端口和认证 Token
imclaw --port 9000 --token your-secret-token

# 查看所有参数
imclaw --help
```

服务启动后会显示：

```
╔═══════════════════════════════════════╗
║          IMClaw dev                    ║
║   AI Agent Gateway with ACP Protocol  ║
╚═══════════════════════════════════════╝

Gateway started on 0.0.0.0:8080
  HTTP:      http://0.0.0.0:8080
  WebSocket: ws://0.0.0.0:8080/ws

Use 'imclaw-cli' to interact with the server.
```

### 第二步：本地连接远程

在你的本地机器上，使用 imclaw-cli 连接：

```bash
# 连接远程服务器
imclaw-cli --server ws://your-server:8080/ws

# 带认证 Token
imclaw-cli --server ws://your-server:8080/ws --token your-secret-token
```

进入交互模式后，就可以像使用本地 AI 助手一样与远程 Agent 对话了：

```
IMClaw CLI dev
Connected to ws://your-server:8080/ws
Session: abc123 | Agent: claude
Permissions: approve-reads | Format: text

Type your message and press Enter. Use /help for commands, /quit to exit.

> 帮我分析一下 /var/log/nginx/error.log 里的错误
```

### 第三步：单命令模式（推荐）

如果只是想执行单条命令，直接用 `-p` 参数：

```bash
# 一次性执行
imclaw-cli --server ws://your-server:8080/ws -p "查看服务器磁盘使用情况"

# 自动批准所有操作（适合自动化场景）
imclaw-cli --server ws://your-server:8080/ws --approve-all -p "分析代码并给出优化建议"

# JSON 输出（方便程序解析）
imclaw-cli --server ws://your-server:8080/ws --format json -p "列出当前目录文件"
```

## 进阶用法

### 会话复用

IMClaw 支持会话复用，让 AI 记住上下文：

```bash
# 第一次对话，会返回 session_id
imclaw-cli --server ws://remote:8080/ws -p "阅读 main.go 文件"
# 输出包含 session_id: xxx-xxx-xxx

# 继续同一个会话
imclaw-cli --server ws://remote:8080/ws --session xxx-xxx-xxx -p "这个函数有什么问题？"
```

### 多 Agent 切换

不同任务用不同的 Agent：

```bash
# 使用 Claude
imclaw-cli --server ws://remote:8080/ws --agent claude -p "帮我写一个 Go 函数"

# 使用 Codex
imclaw-cli --server ws://remote:8080/ws --agent codex -p "分析这段代码的性能瓶颈"
```

### 权限控制

三种权限模式，灵活控制：

```bash
# 默认模式：读取自动批准，写入需要确认
imclaw-cli --server ws://remote:8080/ws -p "读取配置文件"

# 全自动批准（自动化脚本推荐）
imclaw-cli --server ws://remote:8080/ws --approve-all -p "帮我重构这个模块"

# 只读模式（安全审计推荐）
imclaw-cli --server ws://remote:8080/ws --deny-all -p "分析代码安全性"
```

### 指定工作目录

```bash
imclaw-cli --server ws://remote:8080/ws --cwd /path/to/project -p "分析这个项目"
```

### 通过 SSH 隧道访问

如果服务器在内网，可以用 SSH 隧道：

```bash
# 建立 SSH 隧道
ssh -L 8080:localhost:8080 user@remote-server -N &

# 通过 localhost 访问
imclaw-cli --server ws://localhost:8080/ws -p "Hello"
```

## 在 Clawdbot 中使用 acp-remote Skill

如果你是 **Clawdbot（OpenClaw）** 用户，还可以通过 `acp-remote` Skill 更便捷地连接远程 IMClaw 服务，无需手动输入命令。

### 什么是 acp-remote Skill？

`acp-remote` 是一个专为 Clawdbot 设计的 Skill，它封装了 `imclaw-cli` 的调用，让你可以直接在 Clawdbot 对话中与远程 AI Agent 交互。

### 安装 Skill

确保已安装依赖：

```bash
# 安装 acpx
npm install -g acpx@latest

# 安装 imclaw-cli
curl -fsSL https://raw.githubusercontent.com/smallnest/imclaw/main/scripts/install.sh | bash
```

### 配置环境变量

在 `~/.bashrc` 或 `~/.zshrc` 中配置远程服务器：

```bash
export IMCLAW_SERVER="ws://your-server:8080/ws"
export IMCLAW_TOKEN="your-secret-token"
```

### 使用示例

在 Clawdbot 中，直接告诉 AI 使用 acp-remote：

```
请使用 acp-remote skill 连接远程服务器，帮我分析 /var/log/app.log 中的错误
```

AI 会自动：

1. 检测并安装必要的依赖（imclaw-cli、acpx）
2. 连接到配置的远程 IMClaw 服务器
3. 执行你的请求并返回结果

### Skill 参数

acp-remote 支持所有 imclaw-cli 的参数：

| 参数 | 说明 |
|------|------|
| `--server` | 远程服务器地址 |
| `--token` | 认证 Token |
| `--agent` | Agent 类型（claude、codex 等）|
| `--cwd` | 工作目录 |
| `--approve-all` | 自动批准所有操作 |
| `--deny-all` | 只读模式 |
| `--format` | 输出格式（text、json、quiet）|

### 实际场景

**场景一：远程日志分析**

```
用 acp-remote 分析远程服务器上的 nginx 错误日志，找出最常见的 5 种错误
```

**场景二：远程代码审查**

```
用 acp-remote 连接生产服务器，审查 /app/src 目录下的代码，找出潜在的性能问题
```

**场景三：远程调试**

```
用 acp-remote 帮我在远程服务器上调试这个内存泄漏问题，进程 PID 是 12345
```

这样，你就可以在 Clawdbot 的对话中无缝操作远程服务器，无需切换到终端，大大提升工作效率！

## 交互模式命令

进入交互模式后，支持以下命令：

| 命令 | 说明 |
|------|------|
| `/new` | 创建新会话，清除上下文 |
| `/session` | 显示当前会话信息 |
| `/agent <name>` | 切换 Agent |
| `/agents` | 列出可用 Agent |
| `/help` | 显示帮助 |
| `/quit` | 退出 CLI |

## API 端点

IMClaw 同时提供 HTTP 和 WebSocket 接口：

| 端点 | 方法 | 说明 |
|------|------|------|
| `/health` | GET | 健康检查 |
| `/api/sessions` | GET | 获取所有会话 |
| `/api/agents` | GET | 获取所有 Agent |
| `/rpc` | POST | JSON-RPC 接口 |
| `/ws` | WebSocket | WebSocket 连接 |

这意味着你可以用任何语言调用 IMClaw 的 API，轻松集成到自己的应用中。

## 写在最后

IMClaw 的设计哲学是 **简单、实用、安全**：

- **简单**：无需配置文件，命令行参数即可启动
- **实用**：支持单命令模式和交互模式，满足不同场景
- **安全**：Token 认证，权限控制，让你放心地远程调用 AI

项目完全开源，欢迎 Star、PR 和反馈！

**GitHub**: https://github.com/smallnest/imclaw

---

*如果这篇文章对你有帮助，欢迎点赞、转发。有任何问题，欢迎在评论区留言交流！*
