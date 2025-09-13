# 测试用例设计文档

> 本文档为项目测试编码的权威指导，遵循“数据优先、简洁至上”的原则，为各层级测试提供明确可执行的方案。

---

## 目录

1. [测试范围与目标](#测试范围与目标)
2. [单元测试](#单元测试)
3. [端到端集成测试](#端到端集成测试)
4. [性能测试](#性能测试)
5. [压力测试](#压力测试)
6. [执行命令汇总](#执行命令汇总)

---

## 测试范围与目标

- 覆盖所有原子组件与业务流程，单元测试覆盖率不得低于 **90%**。
- 集成测试验证字幕翻译流水线（含 CLI）的完整性与正确性。
- 基准与压力测试定位性能瓶颈并测定最大并发能力。

---

## 单元测试

针对每个原子组件设计最小可复用的测试用例。以下表格列出关键模块及对应测试点：

| 模块 | 测试编号 | 场景与数据 | 期望行为 |
|------|----------|------------|-----------|
| `pkg/contract` | UT-CTT-01 | `NormalizeFileID` 处理混合分隔符 | 输出统一正斜杠 |
| `internal/config` | UT-CFG-01 | 解析完整 `config.json` | 所有字段正确映射 |
| | UT-CFG-02 | ENV 覆盖部分字段 | 以环境变量为准（空值忽略；无效数字忽略） |
| | UT-CFG-03 | 含非法字段 | 返回错误 |
| `internal/prompt` | UT-PRM-01 | `MakeEstimator` 默认系数 | 返回正确估算值 |
| | UT-PRM-02 | 输入 0 token | 返回 0 且不报错 |
| `internal/rate` | UT-RTE-01 | 超过 RPM/TPM | `Gate.Try` 拒绝请求 |
| | UT-RTE-02 | 取消上下文 | `Gate.Wait` 立即退出 |
| `internal/pipeline` | UT-PIP-01 | 预算不足 | 返回 `ErrBudgetExceeded` |
| | UT-PIP-02 | 协议错误重试 | 触发 `shouldRetryDecode` 分支 |
| `internal/diag` | UT-DIAG-01 | 日志轮转写入 | 生成新文件且旧日志保留 |
| | UT-DIAG-02 | 指标计数 | 计数正确递增 |
| `pkg/registry` | UT-REG-01 | 注册后检索实现 | 返回匹配实例 |
| `cmd/llmspt` | UT-CLI-01 | 参数解析与 ENV/JSON 合并 | CLI 覆盖其他来源 |
| | UT-CLI-02 | `-` 与路径混用 | 返回错误码 `2` |
| | UT-CLI-03 | `--init-config <dir>` 生成模板 | 生成 `<dir>/config.json` 与 `<dir>/.env`，已存在跳过/报错 |
| | UT-CLI-04 | `.env` 自动加载 | 启动即加载工作目录 `.env`，不覆盖系统 ENV |
| `plugins/reader/filesystem` | UT-RFS-01 | 读取单个文件 | 正确生成记录 |
| | UT-RFS-02 | 跳过排除目录 | 不遍历被排除路径 |
| `plugins/splitter/srt` | UT-SSR-01 | 合法 SRT 分割 | 片段时间轴连续 |
| | UT-SSR-02 | 超出 `MaxFragmentBytes` | 返回错误 |
| `plugins/batcher/sliding` | UT-BSL-01 | 目标过大放不下 | 返回 `ErrRecordTooLarge` |
| `plugins/prompt/translate` | UT-PTR-01 | 默认模板构造窗口 | 包含源目标文本 |
| `plugins/llmclient/mock` | UT-LLM-01 | `translate_json_per_record` | 返回逐条翻译 JSON |
| `plugins/decoder/srtjson` | UT-DSJ-01 | 非法 JSON | 返回解码错误 |
| `plugins/assembler/linear` | UT-ALN-01 | FileID 不一致 | 返回不连续错误 |
| `plugins/writer/filesystem` | UT-WFS-01 | 原子写入 | 输出文件完整且临时文件删除 |

执行命令：

```bash
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out   # 覆盖率≥90%
```

---

## 端到端集成测试

使用 `cmd/llmspt` CLI 与 `testdata/test-100-line.srt` 走完整字幕翻译流水线：

### 场景一：成功路径

1. CLI 解析配置并启动流水线。
2. Reader 读取样例 SRT。
3. Splitter 分割并生成 `Record`。
4. Batcher 生成批处理请求。
5. Prompt 构建翻译提示词。
6. Mock LLM 以 `translate_json_per_record` 模式返回。
7. Decoder 解析为 `SpanResult`。
8. Assembler 按顺序拼接。
9. Writer 将结果写入目标文件。

**期望**：输出文件内容与占位翻译一致，顺序与输入完全一致。

### 场景二：预算不足

- 设置极小的 `MaxTokens`，期望流水线中断并向 CLI 返回 `ErrBudgetExceeded`。

### 场景三：限流重试

- 将 `rate.Gate` 配置为低 RPM/TPM，触发 `shouldRetryInvoke` 与 `shouldRetryDecode`。期望最终成功且重试次数受限。

执行命令：

```bash
go test ./testdata -run E2E
```

---

## 性能测试

目标是找出系统吞吐瓶颈与性能极限。

### 组件基准

对核心函数编写 `Benchmark`：

- `Batcher.Make`
- `Decoder.Decode`
- `Writer.Write`

输入规模从小到大递增，记录 `ns/op`、内存分配与分配次数。

### 流水线基准

- 使用 Mock LLM（可设置响应延迟）。
- 输入 `testdata/test-2283-line.srt`。
- 并发度从 1 逐渐提高到 CPU 核心数的数倍，直至吞吐不再增长或延迟显著上升，记录该并发度与耗时作为性能极限。

### 性能瓶颈分析

```bash
go test -run=^$ -bench=Pipeline -cpuprofile=cpu.out -memprofile=mem.out
```

- 使用 `go tool pprof` 分析热点函数，定位 I/O、JSON 解析或字符串处理的瓶颈。

---

## 压力测试

### 并发提升

- 在本地 Mock LLM 环境下关闭 `rate.Gate` 限流，逐步提高 `Settings.Concurrency`。
- 统计成功率、平均延迟与 95% 延迟。

### 资源耗尽

- 使用超大输入文件或持续循环运行，观察是否出现 `ErrInvariantViolation`、OOM 或 goroutine 泄漏。

### 最大并发评估

- 当错误率或延迟急剧上升时记录对应并发值，作为系统极限，并指出受限模块（LLM 调用或 I/O）。

---

## 执行命令汇总

| 测试类型 | 命令 |
|----------|------|
| 单元测试 | `go test ./... -coverprofile=coverage.out` |
| 覆盖率 | `go tool cover -func=coverage.out` |
| 集成测试 | `go test ./testdata -run E2E` |
| 基准测试 | `go test -bench=. ./...` |
| 流水线基准 + pprof | `go test -run=^$ -bench=Pipeline -cpuprofile=cpu.out -memprofile=mem.out` |
| 压力测试 | 自定义脚本或 `go test -run Stress` |
