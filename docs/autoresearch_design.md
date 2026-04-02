# Autoresearch Design for imclaw

基于 [karpathy/autoresearch](https://github.com/karpathy/autoresearch) 的思想，实现自动化处理本项目 GitHub Issues 的系统。

## 核心理念

将传统的"人类写代码 → 运行测试 → 修复问题"流程，反转为：

**人类定义目标 → AI 自主迭代实现 → AI 自主验证 → 人类最终审核**

人类角色从"执行者"变为"指挥官"，通过 `program.md` 定义规则和目标，AI 自主完成具体工作。

---

## 系统架构

```
┌─────────────────────────────────────────────────────────────┐
│                      program.md                             │
│                   (人类定义的规则和目标)                       │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    Issue Selector                            │
│            (从 GitHub 获取并筛选待处理 Issue)                  │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    Agent Orchestrator                        │
│                    (使用 acpx 控制)                          │
│                                                              │
│   ┌──────────┐    批判     ┌──────────┐                     │
│   │  Codex   │ ──────────► │  Claude  │                     │
│   │ (实现者)  │ ◄────────── │ (审核者)  │                     │
│   └──────────┘    优化     └──────────┘                     │
│        │                                                    │
│        ▼                                                    │
│   ┌──────────────────────────────────────┐                  │
│   │         迭代循环 (最多 5 次)           │                  │
│   │                                      │                  │
│   │  实现 → 审核 → 优化 → 再审核 → ...    │                  │
│   └──────────────────────────────────────┘                  │
│        │                                                    │
│        ▼                                                    │
│   ┌──────────────────────────────────────┐                  │
│   │         质量检查点                     │                  │
│   │   - 测试通过？                        │                  │
│   │   - 代码规范？                        │                  │
│   │   - 无重大问题？                      │                  │
│   └──────────────────────────────────────┘                  │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    Human Review                              │
│              (人工最终验证后手动提交)                          │
└─────────────────────────────────────────────────────────────┘
```

---

## 核心文件

| 文件 | 作用 | 修改权限 |
|------|------|---------|
| `program.md` | 定义 Agent 行为规则、目标、约束 | 仅人类修改 |
| `issue-selector.md` | 定义 Issue 选择策略和优先级 | 仅人类修改 |
| `agents/codex.md` | Codex 角色定义和提示词 | 仅人类修改 |
| `agents/claude.md` | Claude 角色定义和提示词 | 仅人类修改 |
| `workflows/` | 各 Issue 的实现记录和结果 | Agent 修改 |
| `results.tsv` | 所有 Issue 处理结果日志 | Agent 修改 |

---

## 迭代工作流

### 单次迭代流程

```
┌─────────────────────────────────────────────────────────────┐
│ 步骤 1: Codex 实现                                          │
│   - 阅读 Issue 内容                                          │
│   - 分析相关代码                                             │
│   - 实现功能代码                                             │
│   - 编写测试代码                                             │
│   - 运行测试验证                                             │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│ 步骤 2: Claude 审核                                          │
│   提示词: "review 当前对 issue#n 的实现，提出批判意见"          │
│   审核维度:                                                   │
│   - 代码正确性                                               │
│   - 测试覆盖率                                               │
│   - 代码风格和规范                                           │
│   - 潜在问题和风险                                           │
│   - 性能考量                                                 │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│ 步骤 3: 质量判定                                             │
│                                                              │
│   通过标准 (满足以下全部):                                     │
│   ✓ 测试全部通过                                             │
│   ✓ 无重大代码问题                                           │
│   ✓ Claude 评分 ≥ 8.5/10                                    │
│                                                              │
│   如果通过 → 进入最终审核                                     │
│   如果不通过 → 继续迭代优化                                   │
└─────────────────────────────────────────────────────────────┘
                              │ 不通过
                              ▼
┌─────────────────────────────────────────────────────────────┐
│ 步骤 4: Codex 根据反馈优化                                    │
│   - 读取 Claude 的批判意见                                    │
│   - 针对性修复问题                                           │
│   - 重新运行测试                                             │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│ 步骤 5: Codex 自我审视                                       │
│   提示词: "review 当前对 issue#n 的实现，提出批判意见"          │
│   - 自我检查实现质量                                         │
│   - 评估是否需要继续优化                                     │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
                     返回步骤 2 (Claude 审核)
```

### 迭代终止条件

| 条件 | 处理 |
|------|------|
| 质量通过 | 停止迭代，等待人工审核 |
| 达到最大迭代次数 (5次) | 记录问题，标记为需人工介入 |
| 测试始终失败 | 记录错误，标记为阻塞 |
| 实现 Impossible | 标记 Issue 为无效/需要更多上下文 |

---

## Issue 选择策略

### 优先级规则

```yaml
# issue-selector.md 配置示例

priority_labels:
  - name: "priority: critical"
    weight: 100
  - name: "priority: high"
    weight: 50
  - name: "priority: medium"
    weight: 20
  - name: "priority: low"
    weight: 10

exclude_labels:
  - "wontfix"
  - "duplicate"
  - "invalid"
  - "blocked"
  - "needs discussion"

complexity_estimation:
  # 简单: 只涉及单个文件，改动 < 50 行
  simple:
    max_iterations: 3
    time_budget: 10m

  # 中等: 涉及 2-3 个文件，改动 < 200 行
  medium:
    max_iterations: 5
    time_budget: 30m

  # 复杂: 涉及多个模块，需要设计决策
  complex:
    max_iterations: 5
    time_budget: 60m
    requires_human_approval: true
```

### 选择流程

1. 从 GitHub 获取所有 Open 状态的 Issues
2. 过滤掉带有排除标签的 Issues
3. 按优先级权重排序
4. 评估复杂度，分配时间和迭代预算
5. 选择最高优先级的 Issue 开始处理

---

## 错误处理

### 测试失败

```
测试失败 → 查看错误日志 → 尝试修复 → 重新测试
                ↓
         修复失败超过 3 次 → 标记为 "test-blocked" → 等待人工介入
```

### API 错误

```
API 调用失败 → 等待 30 秒 → 重试
                   ↓
            重试 3 次失败 → 记录错误 → 暂停系统 → 通知人类
```

### 实现困难

```
Agent 报告无法实现 → 记录原因 → 标记 Issue 为 "needs-context"
                              → 等待人类提供更多信息
```

---

## 结果记录

### results.tsv 格式

```tsv
timestamp	issue_number	issue_title	status	iterations	tests_passed	claude_score	codex_score	branch_name	notes
2024-01-15T10:30:00	42	添加用户认证功能	completed	3	true	8	7	feature/auth-42	无
2024-01-15T14:20:00	43	修复分页bug	blocked	5	false	5	4	fix/pagination-43	测试持续失败
```

### 状态定义

| 状态 | 含义 |
|------|------|
| `completed` | 实现完成，等待人工审核 |
| `blocked` | 遇到阻塞，需要人工介入 |
| `impossible` | Issue 描述不清或无法实现 |
| `skipped` | 跳过（不符合处理条件） |

---

## 使用方法

### 0. 前置概念：acpx

**acpx** 是 Agent 控制工具，用于在命令行中调用 Codex 和 Claude。

```bash
# 检查是否已安装
which acpx

# 如果未安装，可以通过以下方式安装（示例）
# npm install -g acpx
# 或
# go install github.com/example/acpx@latest
```

acpx 基本用法：
```bash
# 调用 Codex
acpx codex "你的提示词"

# 调用 Claude
acpx claude "你的提示词"

# 注意: acpx 不支持 --config 选项
# 配置文件内容需要直接嵌入到提示词中，例如：
acpx codex "实现 Issue #42

请按照以下规则执行:
$(cat ./autoresearch/agents/codex.md)
"
```

---

### 1. 环境准备

```bash
# 1. 确保已配置 GitHub Token
export GITHUB_TOKEN=your_github_token

# 验证 token
gh auth status

# 2. 确保 acpx 可用
which acpx

# 3. 确保 Go 环境（本项目是 Go 项目）
go version

# 4. 克隆仓库（如果还没有）
git clone https://github.com/your-org/imclaw.git
cd imclaw

# 5. 确保测试可以运行
go test ./...
```

---

### 2. 配置规则

根据项目特点，编辑 `autoresearch/program.md`：

```bash
# 编辑实现规则
vim autoresearch/program.md
```

关键配置项：
```markdown
## 权限边界
- Agent 可以修改: internal/, cmd/
- Agent 不能修改: go.mod, .github/, Makefile

## 测试要求
- 覆盖率 ≥ 70%
- 使用表格驱动测试

## 迭代限制
- 最大迭代次数: 默认 42（可通过参数指定）
- 单次时间预算: 10-60 分钟
```

---

### 3. 手动运行（推荐先尝试）

#### 3.1 处理单个 Issue

```bash
# 步骤 1: 查看 Issue 内容
gh issue view 42

# 步骤 2: 创建工作分支
git checkout -b feature/issue-42

# 步骤 3: 启动 Codex 实现
acpx codex "实现 GitHub Issue #42

项目路径: \$(pwd)
Issue 内容: \$(gh issue view 42 --json title,body -q '.title + \": \" + .body')

请按照以下规则执行:
\$(cat ./autoresearch/agents/codex.md)
"

# 步骤 4: 运行测试验证
go test ./... -v

# 步骤 5: 启动 Claude 审核
acpx claude "审核 Issue #42 的实现

项目路径: \$(pwd)
Issue 内容: \$(gh issue view 42 --json title,body -q '.title + \": \" + .body')

请按照以下规则执行:
\$(cat ./autoresearch/agents/claude.md)
"

# 步骤 6: 如果 Claude 评分 < 8.5，根据反馈让 Codex 改进
acpx codex "根据审核反馈改进 Issue #42 的实现

审核反馈: [粘贴 Claude 的反馈]

请按照以下规则执行:
\$(cat ./autoresearch/agents/codex.md)
"

# 重复步骤 4-6，直到评分 ≥ 8.5
```

#### 3.2 完整手动流程图

```
┌─────────────────────────────────────────────────────────────┐
│  1. gh issue view N        # 查看 Issue                      │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  2. git checkout -b feature/issue-N   # 创建分支             │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  3. acpx codex "实现 Issue N..."      # Codex 实现           │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  4. go test ./...                     # 运行测试             │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  5. acpx claude "审核实现..."         # Claude 审核          │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
                     ┌────────┴────────┐
                     │  评分 ≥ 8.5?     │
                     └────────┬────────┘
                      │ 否          │ 是
                      ▼             ▼
┌─────────────────────────┐  ┌─────────────────────────────────┐
│ 6. Codex 改进           │  │ 7. 人工最终审核                  │
│    (返回步骤 4)          │  │    git diff, go test, review    │
└─────────────────────────┘  └─────────────────────────────────┘
                                              │
                                              ▼
                               ┌─────────────────────────────────┐
                               │ 8. git push && gh pr create     │
                               └─────────────────────────────────┘
```

---

### 4. 自动化脚本（进阶）

创建自动化脚本 `autoresearch/run.sh`：

```bash
#!/bin/bash
# autoresearch/run.sh - 自动化处理 Issue

set -e

ISSUE_NUMBER=$1
MAX_ITERATIONS=5
ITERATION=0

if [ -z "$ISSUE_NUMBER" ]; then
    echo "Usage: ./run.sh <issue_number>"
    exit 1
fi

# 获取 Issue 信息
ISSUE_INFO=$(gh issue view $ISSUE_NUMBER --json title,body)
ISSUE_TITLE=$(echo $ISSUE_INFO | jq -r '.title')
ISSUE_BODY=$(echo $ISSUE_INFO | jq -r '.body')

echo "处理 Issue #$ISSUE_NUMBER: $ISSUE_TITLE"

# 创建分支
BRANCH_NAME="feature/issue-$ISSUE_NUMBER"
git checkout -b $BRANCH_NAME 2>/dev/null || git checkout $BRANCH_NAME

# 迭代循环
while [ $ITERATION -lt $MAX_ITERATIONS ]; do
    ITERATION=$((ITERATION + 1))
    echo "=== 迭代 $ITERATION/$MAX_ITERATIONS ==="

    # Codex 实现/改进
    echo "Codex 正在实现..."
    acpx codex "实现 GitHub Issue #$ISSUE_NUMBER

    项目路径: \$(pwd)
    Issue 标题: $ISSUE_TITLE
    Issue 内容: $ISSUE_BODY

    迭代次数: $ITERATION

    请按照以下规则执行:
    \$(cat ./autoresearch/agents/codex.md)
    " 2>&1 | tee "./autoresearch/workflows/issue-$ISSUE_NUMBER/iteration-$ITERATION-codex.log"

    # 运行测试
    echo "运行测试..."
    go test ./... -v 2>&1 | tee "./autoresearch/workflows/issue-$ISSUE_NUMBER/test-$ITERATION.log"
    TEST_RESULT=$?

    if [ $TEST_RESULT -ne 0 ]; then
        echo "测试失败，继续改进..."
        continue
    fi

    # Claude 审核
    echo "Claude 正在审核..."
    REVIEW_RESULT=$(acpx claude "审核 Issue #$ISSUE_NUMBER 的实现

    项目路径: \$(pwd)
    Issue 标题: $ISSUE_TITLE

    请按照以下规则执行:
    \$(cat ./autoresearch/agents/claude.md)
    " 2>&1 | tee "./autoresearch/workflows/issue-$ISSUE_NUMBER/iteration-$ITERATION-claude.log")

    # 提取评分（兼容 macOS）
    SCORE=$(echo "$REVIEW_RESULT" | grep -E '评分:|Score:' | grep -oE '[0-9]+' | head -1)

    echo "审核评分: $SCORE"

    if [ "$SCORE" -ge 7 ]; then
        echo "审核通过！"
        echo "分支已准备好: $BRANCH_NAME"
        echo "请进行人工审核后手动提交。"
        exit 0
    fi

    echo "评分未达标，继续改进..."
done

echo "达到最大迭代次数，仍未通过审核。"
echo "请人工介入处理。"
exit 1
```

使用脚本：
```bash
chmod +x autoresearch/run.sh
./autoresearch/run.sh 42
```

---

### 5. 查看结果

```bash
# 查看结果汇总
cat autoresearch/results.tsv

# 查看特定 Issue 的详细记录
ls autoresearch/workflows/issue-42/
# 输出:
# log.md                  # 总日志
# iteration-1-codex.log   # 第1次 Codex 输出
# iteration-1-claude.log  # 第1次 Claude 审核
# iteration-2-codex.log   # 第2次 Codex 输出
# ...

# 查看代码改动
git diff main
```

---

### 6. 人工审核和提交

**重要：Agent 完成实现后不会自动提交，必须人工验证。**

```bash
# 1. 切换到实现分支
git checkout feature/issue-42

# 2. 运行完整测试
go test ./... -v
go test -race ./...
go test -cover ./...

# 3. 代码检查
golangci-lint run
gofmt -d .
go vet ./...

# 4. 详细代码审查
git diff main

# 5. 查看文件变更
git status
git log --oneline main..HEAD

# 6. 确认无误后，推送到远程
git push origin feature/issue-42

# 7. 创建 PR
gh pr create \
  --title "feat: 实现用户认证功能 (#42)" \
  --body "$(cat <<'EOF'
## Summary
- 实现了 JWT 认证功能
- 添加了认证中间件
- 增加了单元测试

## Test plan
- [x] 单元测试通过
- [x] 测试覆盖率 75%
- [x] golangci-lint 检查通过

Closes #42
EOF
)"

# 8. 合并 PR 后关闭 Issue
# PR 合并后 Issue 会自动关闭（如果使用了 "Closes #42"）
```

---

### 7. 常见场景

#### 场景 1: Bug 修复

```bash
# Bug 通常比较简单，迭代次数少
./autoresearch/run.sh 50  # 假设 #50 是一个 bug
```

#### 场景 2: 新功能开发

```bash
# 功能开发可能需要多轮迭代
# 先查看 Issue，评估复杂度
gh issue view 42

# 如果复杂度高，考虑先做设计评审
acpx claude "分析 Issue #42 的设计方案..."
```

#### 场景 3: 遇到阻塞

```bash
# 如果 Agent 报告无法实现
# 查看日志了解原因
cat autoresearch/workflows/issue-42/log.md

# 可能需要:
# 1. 补充 Issue 描述
# 2. 提供更多上下文
# 3. 人工介入决策
```

---

### 8. 最佳实践

1. **从小 Issue 开始**：先用简单的 Issue 测试流程
2. **保持 program.md 更新**：根据运行情况调整规则
3. **记录异常情况**：遇到问题时更新规则文件
4. **人工审核不可省略**：Agent 输出只是初版，必须人工把关
5. **关注测试覆盖率**：确保测试充分

---

## 注意事项

### Agent 权限控制

- Agent **不能**修改 `.github/`、`go.mod`、`Makefile` 等核心配置
- Agent **不能**删除文件
- Agent **不能**修改 `autoresearch/` 目录下的规则文件
- Agent **不能**推送到远程仓库
- Agent **不能**关闭 Issue

### 质量保障

- 每次 Codex 实现后必须运行测试
- Claude 审核必须给出具体改进建议
- 所有代码变更必须经过至少一轮 Claude 审核
- 复杂 Issue (预估改动 > 200 行) 需要先进行设计评审

### 人工干预点

系统会在以下情况暂停并等待人工决策：

1. 达到最大迭代次数但质量未达标
2. 测试连续失败超过阈值
3. 遇到无法理解的需求
4. 涉及架构级别的设计决策
5. 安全敏感的代码修改

---

## 文件结构

```
imclaw/
├── autoresearch/
│   ├── program.md              # 实现规则和约束
│   ├── issue-selector.md       # Issue 选择策略
│   ├── agents/
│   │   ├── codex.md            # Codex 角色定义
│   │   └── claude.md           # Claude 角色定义
│   ├── workflows/
│   │   └── issue-{n}/
│   │       ├── log.md          # 实现日志
│   │       ├── iterations/     # 各次迭代记录
│   │       └── final/          # 最终代码快照
│   └── results.tsv             # 结果汇总
├── internal/
├── cmd/
└── ...
```

---

## 参考资料

- [karpathy/autoresearch](https://github.com/karpathy/autoresearch) - 原始设计灵感
- [acpx 文档](https://github.com/anthropics/anthropic-cookbook) - Agent 控制工具
