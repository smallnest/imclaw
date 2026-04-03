#!/bin/bash
# autoresearch/run.sh - 自动化处理 GitHub Issue
#
# 用法:
#   ./run.sh <issue_number> [max_iterations]
#
# 在项目根目录执行:
#   cd /path/to/your/github/project
#   /path/to/run.sh 42           # 处理 Issue #42，使用默认迭代次数 42
#   /path/to/run.sh 42 10        # 处理 Issue #42，最多迭代 10 次
#
# 要求:
#   - 当前目录必须是 git 仓库
#   - 当前目录必须有 GitHub remote (origin)
#
# 配置文件 (可选):
#   在项目根目录创建 .autoresearch/ 目录，可以放置:
#   - .autoresearch/agents/codex.md   自定义 Codex 指令
#   - .autoresearch/agents/claude.md  自定义 Claude 指令

set -e

# ==================== 环境变量处理 ====================
# 加载必要的环境变量（API keys）
# 不直接 source .zshrc，因为它可能包含交互式命令导致脚本退出
if [ -f "$HOME/.zshrc" ]; then
    # 只提取 API key 相关的环境变量
    eval "$(grep -E '^export (OPENROUTER_API_KEY|OPENAI_API_KEY|ANTHROPIC_API_KEY)=' "$HOME/.zshrc" 2>/dev/null)" || true
fi

# ==================== 配置 ====================
DEFAULT_MAX_ITERATIONS=42
PASSING_SCORE=9.0
MAX_CONSECUTIVE_FAILURES=3  # 连续失败最大次数
MAX_RETRIES=10              # 退火重试最大次数
RETRY_BASE_DELAY=2          # 退火重试初始等待时间（秒）
RETRY_MAX_DELAY=60          # 退火重试最大等待时间（秒）

# 脚本所在目录（用于查找默认 agents 配置）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 项目根目录 = 当前工作目录
PROJECT_ROOT="$(pwd)"

# ==================== 函数 ====================

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

error() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] ERROR: $1" >&2
}

# 退火重试函数
# 使用指数退避算法计算等待时间，并添加随机抖动
# 参数: $1 = 当前重试次数 (从1开始)
annealing_delay() {
    local retry=$1
    # 指数退避: 2^retry * base_delay
    local delay=$((RETRY_BASE_DELAY * (1 << (retry - 1))))
    # 添加随机抖动 (0-25%)
    local jitter=$((delay / 4))
    if [ $jitter -gt 0 ]; then
        jitter=$((RANDOM % jitter))
    fi
    delay=$((delay + jitter))
    # 不超过最大等待时间
    if [ $delay -gt $RETRY_MAX_DELAY ]; then
        delay=$RETRY_MAX_DELAY
    fi
    echo $delay
}

# 执行带退火重试的命令
# 参数: $1 = agent名称 (codex/claude), $2 = prompt
run_with_retry() {
    local agent=$1
    local prompt="$2"
    local log_file="$3"
    local retry=0
    local success=0

    while [ $retry -lt $MAX_RETRIES ]; do
        retry=$((retry + 1))

        if [ $retry -gt 1 ]; then
            local delay
            delay=$(annealing_delay $retry)
            log "第 $retry/$MAX_RETRIES 次重试，等待 ${delay} 秒..."
            sleep $delay
        fi

        log "调用 $agent (尝试 $retry/$MAX_RETRIES)..."

        # 执行命令
        if acpx --approve-all $agent "$prompt" 2>&1 | tee "$log_file"; then
            # 检查是否有错误
            if ! grep -q "\[error\]" "$log_file" 2>/dev/null; then
                # 检查是否有实际输出
                local content_lines
                content_lines=$(grep -v "^\[acpx\]" "$log_file" | grep -v "^\[client\]" "$log_file" | grep -v "^\[error\]" "$log_file" | grep -v "^$" | wc -l)
                if [ "$content_lines" -ge 5 ]; then
                    success=1
                    break
                else
                    log "警告: 输出内容过少 ($content_lines 行)"
                fi
            fi
        fi

        log "$agent 第 $retry 次调用失败"
    done

    if [ $success -eq 1 ]; then
        return 0
    else
        error "$agent 调用失败，已重试 $MAX_RETRIES 次"
        return 1
    fi
}

usage() {
    echo "用法: $0 <issue_number> [max_iterations]"
    echo ""
    echo "在 GitHub 项目根目录执行此脚本。"
    echo ""
    echo "参数:"
    echo "  issue_number     GitHub Issue 编号"
    echo "  max_iterations   最大迭代次数 (默认: $DEFAULT_MAX_ITERATIONS)"
    echo ""
    echo "配置:"
    echo "  PASSING_SCORE=9.0              达标评分线"
    echo "  MAX_CONSECUTIVE_FAILURES=3     连续失败最大次数"
    echo ""
    echo "自定义配置文件 (可选):"
    echo "  .autoresearch/agents/codex.md   Codex 指令"
    echo "  .autoresearch/agents/claude.md  Claude 指令"
    echo ""
    echo "示例:"
    echo "  cd /path/to/github/project"
    echo "  $0 42            # 处理 Issue #42"
    echo "  $0 42 10         # 处理 Issue #42，最多迭代 10 次"
    exit 1
}

check_project() {
    log "检查项目环境..."

    # 检查是否是 git 仓库
    if ! git rev-parse --is-inside-work-tree &> /dev/null; then
        error "当前目录不是 git 仓库: $PROJECT_ROOT"
        exit 1
    fi

    # 检查是否有 GitHub remote
    local remote_url
    remote_url=$(git remote get-url origin 2>/dev/null || true)

    if [ -z "$remote_url" ]; then
        error "未找到 git remote origin"
        exit 1
    fi

    if ! echo "$remote_url" | grep -qE 'github\.com|github\.baidu\.com'; then
        error "origin 不是 GitHub 仓库: $remote_url"
        exit 1
    fi

    log "项目目录: $PROJECT_ROOT"
    log "Git remote: $remote_url"
}

check_dependencies() {
    log "检查依赖..."

    local missing=0

    if ! command -v gh &> /dev/null; then
        error "gh (GitHub CLI) 未安装"
        missing=1
    fi

    if ! command -v acpx &> /dev/null; then
        error "acpx 未安装，请先安装 acpx"
        missing=1
    fi

    if ! command -v go &> /dev/null; then
        error "Go 未安装"
        missing=1
    fi

    if [ $missing -eq 1 ]; then
        exit 1
    fi

    log "依赖检查通过"
}

ensure_acpx_session() {
    log "准备 acpx session..."

    cd "$PROJECT_ROOT"

    # 先关闭可能存在的旧 session（避免缓存旧配置）
    log "关闭旧 session..."
    acpx codex sessions close 2>/dev/null || true
    acpx claude sessions close 2>/dev/null || true

    sleep 1

    # 创建新的 codex session
    log "创建 codex session..."
    acpx codex sessions new 2>&1

    # 创建新的 claude session
    log "创建 claude session..."
    acpx claude sessions new 2>&1

    log "acpx session 准备完成"
}

get_issue_info() {
    local issue_number=$1

    log "获取 Issue #$issue_number 信息..."

    ISSUE_INFO=$(gh issue view $issue_number --json number,title,body,state,labels 2>&1)

    if [ $? -ne 0 ]; then
        error "无法获取 Issue #$issue_number: $ISSUE_INFO"
        exit 1
    fi

    ISSUE_TITLE=$(echo "$ISSUE_INFO" | jq -r '.title')
    ISSUE_BODY=$(echo "$ISSUE_INFO" | jq -r '.body')
    ISSUE_STATE=$(echo "$ISSUE_INFO" | jq -r '.state')
    ISSUE_LABELS=$(echo "$ISSUE_INFO" | jq -r '.labels[].name' | tr '\n' ',' | sed 's/,$//')

    if [ "$ISSUE_STATE" != "OPEN" ]; then
        error "Issue #$issue_number 状态为 $ISSUE_STATE，不是 OPEN"
        exit 1
    fi

    log "Issue 标题: $ISSUE_TITLE"
    log "Issue 标签: $ISSUE_LABELS"
}

setup_work_directory() {
    local issue_number=$1

    # 工作目录在项目根目录下的 .autoresearch/
    WORK_DIR="$PROJECT_ROOT/.autoresearch/workflows/issue-$issue_number"
    mkdir -p "$WORK_DIR"

    log "工作目录: $WORK_DIR"

    # 初始化日志文件
    cat > "$WORK_DIR/log.md" << EOF
# Issue #$issue_number 实现日志

## 基本信息
- Issue: #$issue_number - $ISSUE_TITLE
- 开始时间: $(date '+%Y-%m-%d %H:%M:%S')
- 标签: $ISSUE_LABELS

## 迭代记录

EOF
}

# 获取 agent 指令文件路径
# 优先使用项目目录下的自定义文件，否则使用脚本目录下的默认文件
get_agent_instructions() {
    local agent_name=$1

    # 优先级1: 项目目录下的自定义文件
    local project_agent="$PROJECT_ROOT/.autoresearch/agents/$agent_name.md"
    if [ -f "$project_agent" ]; then
        echo "$project_agent"
        return
    fi

    # 优先级2: 脚本目录下的默认文件
    local default_agent="$SCRIPT_DIR/agents/$agent_name.md"
    if [ -f "$default_agent" ]; then
        echo "$default_agent"
        return
    fi

    # 没有找到，返回空
    echo ""
}

create_branch() {
    local issue_number=$1

    BRANCH_NAME="feature/issue-$issue_number"

    log "创建分支: $BRANCH_NAME"

    cd "$PROJECT_ROOT"

    # 检查分支是否已存在
    if git show-ref --verify --quiet "refs/heads/$BRANCH_NAME"; then
        log "分支已存在，切换到: $BRANCH_NAME"
        git checkout "$BRANCH_NAME"
    else
        git checkout -b "$BRANCH_NAME"
    fi
}

run_codex() {
    local issue_number=$1
    local iteration=$2
    local previous_feedback=$3

    log "迭代 $iteration: Codex 实现..."

    # 获取 codex 指令文件
    local codex_instructions_file
    codex_instructions_file=$(get_agent_instructions "codex")

    local codex_instructions=""
    if [ -n "$codex_instructions_file" ]; then
        codex_instructions=$(cat "$codex_instructions_file")
        log "使用指令文件: $codex_instructions_file"
    fi

    local prompt
    if [ -z "$previous_feedback" ]; then
        prompt="实现 GitHub Issue #$issue_number

项目路径: $PROJECT_ROOT
Issue 标题: $ISSUE_TITLE
Issue 内容: $ISSUE_BODY

迭代次数: $iteration

---
请按照以下指令执行:
$codex_instructions
"
    else
        prompt="根据审核反馈改进 Issue #$issue_number 的实现

项目路径: $PROJECT_ROOT
Issue 标题: $ISSUE_TITLE

审核反馈:
$previous_feedback

---
请按照以下指令执行:
$codex_instructions
"
    fi

    local log_file="$WORK_DIR/iteration-$iteration-codex.log"

    # 使用退火重试机制调用 codex
    cd "$PROJECT_ROOT"
    if ! run_with_retry codex "$prompt" "$log_file"; then
        return 1
    fi

    echo "" >> "$WORK_DIR/log.md"
    echo "### 迭代 $iteration - Codex (实现)" >> "$WORK_DIR/log.md"
    echo "" >> "$WORK_DIR/log.md"
    echo "详见: [iteration-$iteration-codex.log](./iteration-$iteration-codex.log)" >> "$WORK_DIR/log.md"
    return 0
}

run_claude() {
    local issue_number=$1
    local iteration=$2
    local previous_feedback=$3

    log "迭代 $iteration: Claude 实现..."

    # 获取 claude 指令文件（作为实现者）
    local claude_instructions_file
    claude_instructions_file=$(get_agent_instructions "claude")

    local claude_instructions=""
    if [ -n "$claude_instructions_file" ]; then
        claude_instructions=$(cat "$claude_instructions_file")
        log "使用指令文件: $claude_instructions_file"
    fi

    local prompt
    if [ -z "$previous_feedback" ]; then
        prompt="实现 GitHub Issue #$issue_number

项目路径: $PROJECT_ROOT
Issue 标题: $ISSUE_TITLE
Issue 内容: $ISSUE_BODY

迭代次数: $iteration

---
请作为实现者执行（参考 codex.md 的实现者角色）:
$claude_instructions
"
    else
        prompt="根据审核反馈改进 Issue #$issue_number 的实现

项目路径: $PROJECT_ROOT
Issue 标题: $ISSUE_TITLE

审核反馈:
$previous_feedback

---
请作为实现者执行改进:
$claude_instructions
"
    fi

    local log_file="$WORK_DIR/iteration-$iteration-claude.log"

    # 使用退火重试机制调用 claude
    cd "$PROJECT_ROOT"
    if ! run_with_retry claude "$prompt" "$log_file"; then
        return 1
    fi

    echo "" >> "$WORK_DIR/log.md"
    echo "### 迭代 $iteration - Claude (实现)" >> "$WORK_DIR/log.md"
    echo "" >> "$WORK_DIR/log.md"
    echo "详见: [iteration-$iteration-claude.log](./iteration-$iteration-claude.log)" >> "$WORK_DIR/log.md"
    return 0
}

run_tests() {
    local iteration=$1

    log "迭代 $iteration: 运行测试..."

    cd "$PROJECT_ROOT"

    local log_file="$WORK_DIR/test-$iteration.log"

    # 检查是否有 Go 模块
    if [ -f "go.mod" ]; then
        if go test ./... -v 2>&1 | tee "$log_file"; then
            log "测试通过"
            echo "- 测试: ✅ 通过" >> "$WORK_DIR/log.md"
            return 0
        else
            log "测试失败"
            echo "- 测试: ❌ 失败" >> "$WORK_DIR/log.md"
            return 1
        fi
    else
        log "未找到 go.mod，跳过测试"
        echo "- 测试: ⏭️ 跳过 (无 go.mod)" >> "$WORK_DIR/log.md"
        return 0
    fi
}

run_claude_review() {
    local issue_number=$1
    local iteration=$2

    log "迭代 $iteration: Claude 审核..."

    # 获取 claude 指令文件
    local claude_instructions_file
    claude_instructions_file=$(get_agent_instructions "claude")

    local claude_instructions=""
    if [ -n "$claude_instructions_file" ]; then
        claude_instructions=$(cat "$claude_instructions_file")
        log "使用指令文件: $claude_instructions_file"
    fi

    local prompt="审核 Issue #$issue_number 的实现

项目路径: $PROJECT_ROOT
Issue 标题: $ISSUE_TITLE

---
请按照以下指令执行审核:
$claude_instructions
"

    local log_file="$WORK_DIR/iteration-$iteration-claude-review.log"

    # 使用退火重试机制调用 claude
    cd "$PROJECT_ROOT"
    if ! run_with_retry claude "$prompt" "$log_file"; then
        echo "0" > "$WORK_DIR/.last_score"
        return 1
    fi

    # 提取评分 - 兼容 macOS (不使用 grep -P)，支持小数
    local score=0
    local review_result
    review_result=$(cat "$log_file")

    # 尝试多种格式匹配
    # 格式1: 评分: X/10 或 Score: X/10 (支持小数如 8.5)
    local score_line
    score_line=$(echo "$review_result" | grep -E "评分:|Score:" | head -1)

    if [ -n "$score_line" ]; then
        # 提取数字（包括小数）
        score=$(echo "$score_line" | grep -oE '[0-9]+\.?[0-9]*' | head -1)
    fi

    # 格式2: X/10 或 X.Y/10 格式
    if [ -z "$score" ] || [ "$score" = "0" ]; then
        score_line=$(echo "$review_result" | grep -E '[0-9]+\.?[0-9]*/10' | head -1)
        if [ -n "$score_line" ]; then
            score=$(echo "$score_line" | grep -oE '[0-9]+\.?[0-9]*' | head -1)
        fi
    fi

    # 格式3: **评分: X/10** markdown 格式
    if [ -z "$score" ] || [ "$score" = "0" ]; then
        score_line=$(echo "$review_result" | grep -E '\*\*评分' | head -1)
        if [ -n "$score_line" ]; then
            score=$(echo "$score_line" | grep -oE '[0-9]+\.?[0-9]*' | head -1)
        fi
    fi

    if [ -z "$score" ] || [ "$score" = "0" ]; then
        log "警告: 无法从审核结果中提取评分，默认为 5"
        score=5
    fi

    echo "- 审核评分 (Claude): $score/10" >> "$WORK_DIR/log.md"

    log "审核评分: $score/10"

    echo "$review_result"
    # 通过文件传递评分（避免 return 值限制）
    echo "$score" > "$WORK_DIR/.last_score"
    return 0
}

run_codex_review() {
    local issue_number=$1
    local iteration=$2

    log "迭代 $iteration: Codex 审核..."

    # 获取 codex 指令文件（作为审核者）
    local codex_instructions_file
    codex_instructions_file=$(get_agent_instructions "codex")

    local codex_instructions=""
    if [ -n "$codex_instructions_file" ]; then
        codex_instructions=$(cat "$codex_instructions_file")
        log "使用指令文件: $codex_instructions_file"
    fi

    local prompt="审核 Issue #$issue_number 的实现

项目路径: $PROJECT_ROOT
Issue 标题: $ISSUE_TITLE

---
请作为审核者执行审核（参考 claude.md 的审核者角色），给出评分和改进建议:
$codex_instructions
"

    local log_file="$WORK_DIR/iteration-$iteration-codex-review.log"

    # 使用退火重试机制调用 codex
    cd "$PROJECT_ROOT"
    if ! run_with_retry codex "$prompt" "$log_file"; then
        echo "0" > "$WORK_DIR/.last_score"
        return 1
    fi

    # 提取评分 - 兼容 macOS (不使用 grep -P)，支持小数
    local score=0
    local review_result
    review_result=$(cat "$log_file")

    # 尝试多种格式匹配
    local score_line
    score_line=$(echo "$review_result" | grep -E "评分:|Score:" | head -1)

    if [ -n "$score_line" ]; then
        score=$(echo "$score_line" | grep -oE '[0-9]+\.?[0-9]*' | head -1)
    fi

    if [ -z "$score" ] || [ "$score" = "0" ]; then
        score_line=$(echo "$review_result" | grep -E '[0-9]+\.?[0-9]*/10' | head -1)
        if [ -n "$score_line" ]; then
            score=$(echo "$score_line" | grep -oE '[0-9]+\.?[0-9]*' | head -1)
        fi
    fi

    if [ -z "$score" ] || [ "$score" = "0" ]; then
        score_line=$(echo "$review_result" | grep -E '\*\*评分' | head -1)
        if [ -n "$score_line" ]; then
            score=$(echo "$score_line" | grep -oE '[0-9]+\.?[0-9]*' | head -1)
        fi
    fi

    if [ -z "$score" ] || [ "$score" = "0" ]; then
        log "警告: 无法从审核结果中提取评分，默认为 5"
        score=5
    fi

    echo "- 审核评分 (Codex): $score/10" >> "$WORK_DIR/log.md"

    log "审核评分: $score/10"

    echo "$review_result"
    echo "$score" > "$WORK_DIR/.last_score"
    return 0
}

# 比较浮点数评分是否达标
check_score_passed() {
    local score=$1
    local passing=$PASSING_SCORE

    # 使用 awk 进行浮点数比较
    awk -v score="$score" -v passing="$passing" 'BEGIN { exit (score >= passing) ? 0 : 1 }'
}

get_last_score() {
    if [ -f "$WORK_DIR/.last_score" ]; then
        cat "$WORK_DIR/.last_score"
    else
        echo "0"
    fi
}

record_final_result() {
    local issue_number=$1
    local status=$2
    local iterations=$3
    local final_score=$4

    cd "$PROJECT_ROOT"

    local tests_passed="false"
    if [ -f "go.mod" ] && go test ./... &> /dev/null; then
        tests_passed="true"
    fi

    # 追加到 results.tsv (在项目目录下)
    local results_file="$PROJECT_ROOT/.autoresearch/results.tsv"
    printf "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n" \
        "$(date -Iseconds)" \
        "$issue_number" \
        "$ISSUE_TITLE" \
        "$status" \
        "$iterations" \
        "$tests_passed" \
        "$final_score" \
        "$final_score" \
        "$BRANCH_NAME" \
        "" >> "$results_file"

    # 更新日志
    cat >> "$WORK_DIR/log.md" << EOF

## 最终结果
- 总迭代次数: $iterations
- 最终评分: $final_score/10
- 状态: $status
- 分支: $BRANCH_NAME
- 结束时间: $(date '+%Y-%m-%d %H:%M:%S')
EOF
}

# ==================== 主流程 ====================

if [ -z "$1" ]; then
    usage
fi

ISSUE_NUMBER=$1
MAX_ITERATIONS=${2:-$DEFAULT_MAX_ITERATIONS}

log "=========================================="
log "开始处理 Issue #$ISSUE_NUMBER"
log "最大迭代次数: $MAX_ITERATIONS"
log "=========================================="

# 检查项目环境
check_project

# 检查依赖
check_dependencies

# 获取 Issue 信息
get_issue_info "$ISSUE_NUMBER"

# 设置工作目录
setup_work_directory "$ISSUE_NUMBER"

# 确保 acpx session 存在 (仅用于 claude)
ensure_acpx_session

# 创建分支
create_branch "$ISSUE_NUMBER"

# 迭代循环（轮流模式）
# 奇数轮: Codex 实现 → Claude 审核
# 偶数轮: Claude 实现 → Codex 审核
ITERATION=0
PREVIOUS_FEEDBACK=""
FINAL_SCORE=0
CONSECUTIVE_FAILURES=0

while [ $ITERATION -lt $MAX_ITERATIONS ]; do
    ITERATION=$((ITERATION + 1))

    log ""
    log "=========================================="
    log "迭代 $ITERATION/$MAX_ITERATIONS"
    # 判断奇偶轮
    if [ $((ITERATION % 2)) -eq 1 ]; then
        log "本轮: Codex 实现 → Claude 审核"
    else
        log "本轮: Claude 实现 → Codex 审核"
    fi
    log "=========================================="

    # 根据奇偶轮选择实现者
    if [ $((ITERATION % 2)) -eq 1 ]; then
        # 奇数轮: Codex 实现
        if ! run_codex "$ISSUE_NUMBER" "$ITERATION" "$PREVIOUS_FEEDBACK"; then
            CONSECUTIVE_FAILURES=$((CONSECUTIVE_FAILURES + 1))
            log "Codex 执行失败 (连续失败: $CONSECUTIVE_FAILURES/$MAX_CONSECUTIVE_FAILURES)"

            if [ $CONSECUTIVE_FAILURES -ge $MAX_CONSECUTIVE_FAILURES ]; then
                error "连续失败 $CONSECUTIVE_FAILURES 次，停止运行"
                error "请检查 acpx codex 配置"

                record_final_result "$ISSUE_NUMBER" "agent_failed" "$ITERATION" "$FINAL_SCORE"
                exit 1
            fi

            log "等待 10 秒后重试..."
            sleep 10
            continue
        fi
    else
        # 偶数轮: Claude 实现
        if ! run_claude "$ISSUE_NUMBER" "$ITERATION" "$PREVIOUS_FEEDBACK"; then
            CONSECUTIVE_FAILURES=$((CONSECUTIVE_FAILURES + 1))
            log "Claude 执行失败 (连续失败: $CONSECUTIVE_FAILURES/$MAX_CONSECUTIVE_FAILURES)"

            if [ $CONSECUTIVE_FAILURES -ge $MAX_CONSECUTIVE_FAILURES ]; then
                error "连续失败 $CONSECUTIVE_FAILURES 次，停止运行"
                error "请检查 acpx claude 配置"

                record_final_result "$ISSUE_NUMBER" "agent_failed" "$ITERATION" "$FINAL_SCORE"
                exit 1
            fi

            log "等待 10 秒后重试..."
            sleep 10
            continue
        fi
    fi

    # 实现成功，重置连续失败计数
    CONSECUTIVE_FAILURES=0

    # 运行测试
    if ! run_tests "$ITERATION"; then
        PREVIOUS_FEEDBACK="测试失败，请检查测试输出并修复问题。"
        continue
    fi

    # 根据奇偶轮选择审核者
    if [ $((ITERATION % 2)) -eq 1 ]; then
        # 奇数轮: Claude 审核
        run_claude_review "$ISSUE_NUMBER" "$ITERATION"
        REVIEW_LOG_FILE="$WORK_DIR/iteration-$ITERATION-claude-review.log"
    else
        # 偶数轮: Codex 审核
        run_codex_review "$ISSUE_NUMBER" "$ITERATION"
        REVIEW_LOG_FILE="$WORK_DIR/iteration-$ITERATION-codex-review.log"
    fi

    SCORE=$(get_last_score)
    FINAL_SCORE=$SCORE

    # 检查是否通过（支持小数评分）
    if check_score_passed "$SCORE"; then
        log "审核通过！评分: $SCORE/10 (达标线: $PASSING_SCORE)"

        record_final_result "$ISSUE_NUMBER" "completed" "$ITERATION" "$SCORE"

        echo ""
        log "=========================================="
        log "处理完成！"
        log "=========================================="
        log "分支: $BRANCH_NAME"
        log "评分: $SCORE/10"
        log "迭代次数: $ITERATION"

        # 自动提交 PR 并合并
        log ""
        log "=========================================="
        log "自动提交 PR 并合并..."
        log "=========================================="

        cd "$PROJECT_ROOT"

        # 提交所有更改
        log "提交更改..."
        git add -A
        git commit -m "feat: implement issue #$ISSUE_NUMBER - $ISSUE_TITLE

Implemented by autoresearch with score $SCORE/10 after $ITERATION iterations.

Closes #$ISSUE_NUMBER" 2>/dev/null || log "没有需要提交的更改"

        # 推送分支
        log "推送分支 $BRANCH_NAME..."
        git push -u origin "$BRANCH_NAME"

        # 创建 PR
        log "创建 Pull Request..."
        PR_URL=$(gh pr create --title "feat: $ISSUE_TITLE (#$ISSUE_NUMBER)" --body "$(cat <<EOF
## Summary
- Implements #$ISSUE_NUMBER
- Score: $SCORE/10
- Iterations: $ITERATION

## Test plan
- [x] All tests pass
- [x] Code review completed with score >= $PASSING_SCORE

Closes #$ISSUE_NUMBER
EOF
)" 2>&1)

        if echo "$PR_URL" | grep -q "https://github.com"; then
            PR_NUMBER=$(echo "$PR_URL" | grep -oE '[0-9]+$')
            log "PR 已创建: $PR_URL"

            # 合并 PR
            log "合并 PR #$PR_NUMBER..."
            gh pr merge "$PR_NUMBER" --merge --delete-branch

            log ""
            log "=========================================="
            log "完成！Issue #$ISSUE_NUMBER 已自动处理"
            log "=========================================="
            log "PR: $PR_URL"
            log "状态: 已合并"
        else
            log "警告: PR 创建失败或已存在"
            log "$PR_URL"
        fi

        exit 0
    fi

    log "评分未达标 ($SCORE/$PASSING_SCORE)，准备下一轮迭代..."

    # 获取审核结果作为下次反馈
    PREVIOUS_FEEDBACK=$(cat "$REVIEW_LOG_FILE")
done

# 达到最大迭代次数
log ""
log "=========================================="
log "达到最大迭代次数，仍未通过审核"
log "=========================================="
log "最终评分: $FINAL_SCORE/10"
log "请人工介入处理"

record_final_result "$ISSUE_NUMBER" "blocked" "$ITERATION" "$FINAL_SCORE"

exit 1
