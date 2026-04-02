# Autoresearch - 自动化 Issue 处理系统

基于 [karpathy/autoresearch](https://github.com/karpathy/autoresearch) 思想实现的 GitHub Issue 自动处理系统。

## 快速开始

### 1. 前置条件

```bash
# 检查 GitHub CLI
gh auth status

# 检查 acpx（Agent 控制工具）
which acpx

# 检查 Go 环境
go version
```

### 2. 使用自动化脚本

**推荐方式**：从项目根目录运行

```bash
# 从项目根目录运行，使用默认迭代次数 42
cd /path/to/imclaw
./docs/autoresearch/run.sh 42

# 指定最大迭代次数
./docs/autoresearch/run.sh 42 10   # 最多迭代 10 次
./docs/autoresearch/run.sh 15 5    # 处理 Issue #15，最多迭代 5 次
```

脚本会自动：
1. 创建 acpx session（如果不存在）
2. 获取 Issue 信息
3. 创建工作分支
4. 循环执行 Codex 实现 → 测试 → Claude 审核
5. 直到评分 ≥ 7 或达到最大迭代次数

### 3. 手动处理（可选）

如果需要手动控制每一步：

```bash
# 1. 确保有 acpx session
cd /path/to/imclaw
acpx codex sessions new
acpx claude sessions new

# 2. 查看 Issue
gh issue view 42

# 3. 创建分支
git checkout -b feature/issue-42

# 4. Codex 实现
acpx codex "实现 Issue #42: [Issue标题]

请按照以下规则执行:
$(cat ./docs/autoresearch/agents/codex.md)
"

# 5. 运行测试
go test ./...

# 6. Claude 审核
acpx claude "审核 Issue #42 的实现

请按照以下规则执行:
$(cat ./docs/autoresearch/agents/claude.md)
"

# 7. 如果评分 < 7，让 Codex 改进，然后重复 5-6
```

## 文件说明

| 文件 | 用途 |
|------|------|
| `program.md` | 实现规则与约束 |
| `issue-selector.md` | Issue 选择策略 |
| `agents/codex.md` | Codex（实现者）提示词 |
| `agents/claude.md` | Claude（审核者）提示词 |
| `results.tsv` | 处理结果日志 |
| `workflows/` | 各 Issue 详细记录 |

## 工作流程

```
Issue → Codex实现 → 测试 → Claude审核 → 改进 → ... → 人工审核 → 提交
```

## 核心规则

- **最大迭代次数**: 默认 42 次，可通过参数指定
- **通过标准**: 测试通过 + 无严重问题 + 评分 ≥ 8.5
- **人工审核**: 所有代码必须人工审核后才能提交
- **权限限制**: Agent 不能推送代码、关闭 Issue、修改核心配置

## 更多信息

详细文档请参考 [autoresearch_design.md](../autoresearch_design.md)
