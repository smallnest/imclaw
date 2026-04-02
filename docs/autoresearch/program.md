# Program - 实现规则与约束

本文档定义 Agent 在实现 Issues 时必须遵循的规则和约束。人类通过修改本文档来控制 Agent 的行为。

---

## 核心目标

实现 GitHub Issues 中的功能需求或 Bug 修复，确保代码质量和测试覆盖。

---

## 权限边界

### Agent 可以做的事情

```
✓ 修改 internal/ 目录下的代码
✓ 修改 cmd/ 目录下的代码
✓ 创建新的测试文件
✓ 修改现有测试文件
✓ 在 workflows/ 目录下记录工作日志
✓ 运行测试命令
✓ 运行 lint 命令
✓ 创建本地 git 分支
✓ 提交本地 git commit
```

### Agent 不可以做的事情

```
✗ 修改 go.mod 或 go.sum（除非 Issue 明确要求）
✗ 修改 .github/ 目录下的任何文件
✗ 修改 Makefile
✗ 修改 Dockerfile 或 docker-compose.yml
✗ 修改 CI/CD 配置文件
✗ 删除任何现有文件
✗ 推送到远程仓库
✗ 关闭 GitHub Issue
✗ 创建 GitHub PR
✗ 修改 autoresearch/ 目录下的规则文件
✗ 执行任何需要 --force 的 git 命令
```

---

## 代码规范

### Go 代码规范

```
1. 遵循 Effective Go (https://golang.org/doc/effective_go)
2. 遵循 Go Code Review Comments (https://github.com/golang/go/wiki/CodeReviewComments)
3. 使用 gofmt 格式化代码
4. 使用 goimports 管理导入
5. 使用 golangci-lint 检查代码质量
```

### 命名规范

```
- 包名: 小写单词，不使用下划线或驼峰
- 文件名: 小写，使用下划线分隔
- 导出函数/类型: 大写开头，有意义名称
- 私有函数/类型: 小写开头
- 接口: 动词或名词 + er 后缀 (Reader, Writer, Handler)
- 常量: 驼峰命名，不使用全大写
```

### 注释规范

```go
// Package parser 提供事件流解析功能。
// 支持多种事件格式的解析和转换。
package parser

// Event 表示一个事件对象。
type Event struct {
    // Type 事件类型，如 "start", "end", "error"
    Type string

    // Data 事件携带的数据
    Data []byte
}

// Parse 解析输入字节流并返回事件列表。
//
// input 必须是有效的 JSON 格式，否则返回 ErrInvalidInput。
//
// 如果输入为空，返回空列表和 nil 错误。
func Parse(input []byte) ([]Event, error) {
    // ...
}
```

### 错误处理

```go
// 使用 fmt.Errorf 包装错误，提供上下文
if err != nil {
    return fmt.Errorf("failed to parse input: %w", err)
}

// 定义哨兵错误
var (
    ErrInvalidInput = errors.New("input is invalid")
    ErrEmptyResult  = errors.New("result is empty")
)

// 自定义错误类型
type ParseError struct {
    Line int
    Msg  string
}

func (e *ParseError) Error() string {
    return fmt.Sprintf("parse error at line %d: %s", e.Line, e.Msg)
}
```

---

## 测试规范

### 测试要求

```
- 所有新增功能必须有单元测试
- 测试覆盖率目标: ≥ 70%
- 使用表格驱动测试
- 测试函数命名: Test<FunctionName>_<Scenario>
- 使用 t.Run 组织子测试
```

### 测试模板

```go
func TestParse(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    []Event
        wantErr error
    }{
        {
            name:  "valid input with single event",
            input: `{"type":"start","data":"test"}`,
            want:  []Event{{Type: "start", Data: []byte("test")}},
            wantErr: nil,
        },
        {
            name:    "invalid json",
            input:   `{invalid}`,
            want:    nil,
            wantErr: ErrInvalidInput,
        },
        {
            name:    "empty input",
            input:   ``,
            want:    []Event{},
            wantErr: nil,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Parse([]byte(tt.input))

            if !errors.Is(err, tt.wantErr) {
                t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
            }

            if !reflect.DeepEqual(got, tt.want) {
                t.Errorf("Parse() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

### 测试禁止事项

```
✗ 不要在测试中使用 time.Sleep（使用 channel 同步）
✗ 不要依赖外部服务（使用 mock）
✗ 不要修改全局状态
✗ 不要使用 t.Skip 逃避测试
✗ 不要在测试中硬编码端口号
```

---

## 迭代控制

### 迭代限制

```
最大迭代次数: 默认 42 次（可通过参数指定）

每次迭代时间预算:
- 简单 Issue: 10 分钟
- 中等 Issue: 30 分钟
- 复杂 Issue: 60 分钟
```

### 终止条件

```
停止迭代的条件:
1. 审核者评分 ≥ 8.5 分，且无严重问题
2. 达到最大迭代次数
3. 测试连续失败 3 次
4. Agent 报告无法实现
5. 发现需要人工决策的设计问题
```

### 迭代报告

每次迭代后，Agent 必须记录：

```
- 迭代序号
- 本次改动内容
- 测试结果
- 审核反馈
- 改进行动
```

---

## 质量检查点

### 提交前检查

Agent 在每次提交前必须确保：

```bash
# 1. 代码编译
go build ./...

# 2. 测试通过
go test ./... -v

# 3. 测试覆盖率
go test -cover ./...

# 4. 代码检查
golangci-lint run

# 5. 格式化
gofmt -s -w .
goimports -w .
```

### 质量标准

```
必须满足:
- [ ] 所有测试通过
- [ ] 无编译警告
- [ ] 无 lint 错误
- [ ] 测试覆盖率 ≥ 70%

建议满足:
- [ ] 无 lint 警告
- [ ] 测试覆盖率 ≥ 80%
- [ ] 所有公共 API 有文档注释
```

---

## 简单性原则

> "简单的实现优于复杂的设计"

### 判断标准

```
在以下情况下，选择更简单的方案:
1. 性能差异可忽略（< 5%）
2. 可读性提升显著
3. 维护成本降低
4. 不影响核心功能
```

### 复杂度评估

```
如果一个改动:
- 新增超过 100 行代码 → 考虑拆分
- 引入新的依赖 → 评估必要性
- 增加新的抽象层 → 说明理由
- 改变现有架构 → 需要人工确认
```

---

## 安全约束

### 敏感操作

以下操作需要人工确认：

```
- 涉及认证/授权的代码修改
- 涉及加密/解密的代码修改
- 涉及数据库操作的代码修改
- 涉及文件系统操作的代码修改
- 涉及网络请求的代码修改
```

### 安全检查

```
Agent 必须确保:
✓ 无 SQL 注入风险
✓ 无命令注入风险
✓ 无路径遍历风险
✓ 无敏感信息硬编码
✓ 无不安全的数据反序列化
✓ 输入验证完整
✓ 错误信息不泄露敏感数据
```

---

## 日志和记录

### 工作日志格式

每个 Issue 在 `workflows/issue-{N}/log.md` 中记录：

```markdown
# Issue #N 实现日志

## 基本信息
- Issue: #N - [标题]
- 类型: feature / bugfix / refactor / docs
- 开始时间: 2024-01-15 10:00:00
- 结束时间: 2024-01-15 11:30:00
- 状态: completed / blocked / impossible

## 迭代记录

### 迭代 1
- 时间: 10:00 - 10:20
- 改动:
  - 新增 internal/auth/jwt.go
  - 新增 internal/auth/jwt_test.go
- 测试: 通过
- 审核评分: 6/10
- 反馈: 存在密钥硬编码问题

### 迭代 2
- 时间: 10:25 - 10:35
- 改动:
  - 修改 internal/auth/jwt.go
- 测试: 通过
- 审核评分: 8/10
- 反馈: 通过

## 最终结果
- 总迭代次数: 2
- 最终评分: 8/10
- 状态: completed
- 分支: feature/auth-42
```

---

## 异常处理

### 遇到阻塞

```markdown
## 阻塞报告

### 阻塞原因
- [ ] 需求不明确
- [ ] 技术限制
- [ ] 依赖问题
- [ ] 测试环境问题
- [ ] 其他

### 详细说明
[描述具体原因]

### 尝试过的解决方案
1. [方案1] - [结果]
2. [方案2] - [结果]

### 建议操作
[建议人类如何处理]
```

### 无法理解需求

```markdown
## 需求澄清请求

### 当前理解
[描述 Agent 对需求的理解]

### 不确定的地方
1. [问题1]
2. [问题2]

### 需要的信息
- [需要补充的信息]
```

---

## 持续改进

本文档会根据实际运行情况持续更新。

### 更新规则

```
1. 当发现新的常见问题时，添加检查规则
2. 当项目规范变化时，更新代码规范
3. 当迭代策略需要调整时，更新迭代控制
4. 所有更新需要人工确认
```
