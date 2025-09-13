# 架构设计文档（主题大纲）

> 本文档为主题大纲阶段，仅列出章节标题，不包含实现性内容。

---

## 第一部分：基础架构

### 1.1 目标与边界

#### 核心目标

本架构旨在构建一个**通用的文本处理流水线框架**，具备以下核心能力：

1. **数据转换本质**：将文本输入通过大语言模型进行转换，输出处理结果
   - 支持翻译、润色、关键词提取等多种处理模式
   - 基于统一的输入 → 处理 → 输出数据流

2. **原子化操作**：识别并定义最小可复用的处理单元（见 2.1 原子操作与接口契约）

3. **单点并发控制**：在流水线编排层实现唯一的并发入口
   - 避免多处并发操作的复杂性
   - 通过调度器统一管理原子操作的并发执行

4. **扩展性架构**：提供插件机制和扩展点
   - 用户通过实现预设接口即可添加功能
   - 架构定义骨架和填充规则，业务代码完善具体实现

5. **结果导向（最小必需）**：仅约束 IR（数据契约）与阶段边界，关心“结果形状”而非业务协议
   - 架构不规定具体解码/格式/模板；仅要求输出满足统一的 `[]SpanResult` 不变式
   - 解析协议交由 Decoder 插件实现；架构不做兜底与回退

#### 非目标

基于最小必需原则，以下内容**明确排除**在架构范围外：

1. **过度工程化**：不为经典处理场景提供特殊处理
2. **极端场景预设**：不预先考虑大型、超大型或极端使用场景
3. **特殊情况兜底**：不为边缘情况反复提供回退和补丁机制
4. **冗余接口**：不包含可省略的数据结构或接口定义
5. **相互排斥功能**：不设计功能重叠或相互冲突的组件
6. **具体业务逻辑**：架构本身不实现任何具体的文本处理业务功能

#### 技术约束

1. **实现语言**：Golang，利用其原生并发模型和内存管理特性
2. **接口约束**：CLI 命令行界面，支持管道和批处理操作
3. **内存模型**：流式处理优先，确保内存安全，避免大文件全量加载
4. **API 限制**：支持 RPM、TPM、单请求 token 限制的多重限流策略

#### 架构边界

1. **数据流边界**：
   - 输入边界：文件/目录路径，流式文本输入
   - 处理边界：批次化数据集，上下文窗口管理
   - 输出边界：索引化内容组装，流式写出

2. **并发边界**：
   - 原子操作内部：无并发，保证操作简单性
   - 流水线层面：统一调度，单点并发控制
   - 系统边界：遵循 API 限流约束

3. **错误处理边界**：
   - 快速失败原则：遇到不可恢复错误立即退出
   - 明确错误分类：可恢复 vs 不可恢复错误
   - 错误传播规则：沿调用栈向上传播，不在底层组件处理业务逻辑错误

### 1.2 项目结构与目录规范

> 用最小必要的结构把数据流打通；接口外露给扩展，具体实现留给业务。保持简单、扁平、连续，单点并发只在流水线层。

#### 设计目标（数据优先）

- 扁平：减少多层跳转与指针追逐，易于定位与替换
- 连续：相关代码物理上相邻，提升局部性与缓存友好
- 最小：仅暴露扩展所需的公共接口，其余内聚在 internal
- 直接：单一并发入口在流水线调度器，原子操作保持无并发

#### 顶层目录（Minimal Go Layout）

```text
.
├── cmd/                    # 可执行入口（仅组装与启动）
│   └── llmspt/            # 主 CLI 子命令
│       └── main.go
├── pkg/                    # 对外可复用/被插件实现的契约
│   ├── contract/           # 原子操作接口、IR 类型与错误语义（对外）
│   └── registry/           # 可选：插件注册器（基于接口名/版本）
├── internal/               # 框架内聚实现（不对外）
│   ├── pipeline/           # 流水线编排与单点并发调度
│   ├── io/                 # 输入/输出的流式读写适配层
│   ├── prompt/             # 可选：模板工具与公共函数（PromptBuilder 由 plugins/prompt 提供）
│   ├── rate/               # RPM/TPM/Token 限流与节流器
│   ├── observability/      # 最小化日志与指标（结构化）
│   └── config/             # 配置解析与校验（与 CLI 解耦）
├── plugins/                # 可选：内置参考实现（非业务、可删除）
│   ├── reader/
│   ├── splitter/
│   ├── batcher/
│   ├── prompt/
│   ├── llmclient/
│   ├── decoder/
│   ├── assembler/
│   └── writer/
├── scripts/                # 调试包/构建脚本
├── testdata/               # 用于契约与流水线集成测试的数据
├── docs/                   # 文档与示意
└── go.mod
```

说明：

- `pkg/contract` 包含接口、IR 类型与错误分类，插件只需实现这些接口即可接入；
- `internal/` 聚合框架逻辑，保持可替换但不可被外部依赖；
- `plugins/` 非必需，仅提供最小参考（架构不内置业务功能）。

#### 包职责与边界

- `pkg/contract`
  - 定义原子操作接口：`Reader`、`Splitter`、`Batcher`、`PromptBuilder`、`LLMClient`、`Decoder`、`Assembler`、`Writer`
  - 定义错误语义：可恢复/不可恢复、限流触发、校验失败
  - 定义 IR 类型：`FileID`、`Index`、`Meta`、`Record`、`Batch`、`Raw`、`SpanResult`（统一数据契约）

- `pkg/registry`（可选最小实现）
  - 按接口名+实现名注册与检索（Map/工厂）
  - 不做反射魔法，不隐藏依赖注入，保持显式构造

- `internal/pipeline`
  - 单点并发编排：工作池/通道/背压在此层集中管理
  - 阶段定义：输入 → 拆分 → 批 → Prompt → LLM → 解码 → 校验 → 组装 → 写出
  - 统一上下文传递、取消、超时、错误向上传播
  - 原子操作在此被顺序调用或并行调度，但原子实现本身不启动并发

- `internal/io`
  - Reader/Writer 的适配：文件/目录/STDIN/STDOUT 的流式接口
  - 统一缓冲策略与零拷贝边界标注

- 其他内聚包
  - `prompt`（可选工具层）：仅提供模板工具/通用函数；具体 PromptBuilder 作为插件位于 `plugins/prompt`；
  - `rate`：简单令牌桶/漏桶，支持 RPM/TPM/Token；
  - `observability`：结构化日志与最小指标；
  - `terminal`：终端状态提示（非日志，stderr，TTY 动态刷新）。
  - `config`：从 CLI/环境/文件装配配置，并做最小校验。

#### 依赖与分层规则

- `cmd` → 仅依赖 `internal/*` 与 `pkg/contract`（组装依赖，不含业务）
- `internal/*` → 只能依赖 `pkg/contract` 与同级内部包；禁止反向依赖 `cmd`/`plugins`
- `plugins/*` → 仅依赖 `pkg/contract`（实现与注册），禁止依赖 `internal/*`
- 任何包禁止循环依赖；数据类型统一来源于 `pkg/contract`

#### 命名与文件规范

- 包名：短小、单词小写（如 `pipeline`、`types`）
- 文件名：`snake_case.go`；测试文件以 `_test.go` 结尾
- 接口命名：以能力为名（`Reader`、`Splitter` 等），避免冗余后缀
- 公开类型仅在 `pkg/contract` 暴露；内部类型不导出

#### 配置与 CLI 约定（最小集）

- `cmd/llmspt` 仅负责：参数解析 → 构造依赖 → 调用 `pipeline.Run`
- 配置项最小化：输入路径、并发度、上下文条数 C、限流参数、模板路径；输出定位与策略由 Writer.Options 提供（不在顶层暴露输出路径）。
- 所有参数均可由环境变量覆盖；默认值在 `internal/config` 统一定义

#### 测试与样例

- 契约测试：在 `pkg/contract` 提供接口级测试样例与模拟器
- 流水线集成基线：使用 `testdata` 的小文件进行端到端验证
- 不引入庞大数据或极端场景用例，遵循最小必要原则

#### 迁移与扩展路径

- 新增能力：在 `pkg/contract` 增加接口；在 `internal/pipeline` 增加阶段映射
- 自定义实现：在 `plugins/xxx` 提供实现并通过 `pkg/registry` 注册后由配置选择
- 替换实现：通过构造函数/工厂在组装期注入（显式依赖，不做隐式魔法）

### 1.3 数据模型与类型定义

> 统一的数据结构是流水线的“共同语言”。坚持：扁平 > 嵌套、连续 > 分散、简单 > 复杂、直接 > 间接。

#### 1.3.1 数据建模（最小必需）

- 核心原子单位：Record（输入片段）
- 上下文承载单元：Batch（同一来源的有序片段集合，带批序）
- 请求原文：Raw（LLMClient 返回的一段文本，万能容器）
- 结果表示：SpanResult（区间结果，覆盖逐条与整段两种主流形态）
- 轻量元信息：Meta（可选，供插件读取；核心流程不依赖）

示例（Go 结构，仅做形状与语义说明）：

```go
// 逻辑文档ID（通常为路径，需要规范化，确保跨平台统一）
// 在 pkg/contract 提供唯一的规范化函数。
type FileID string
// 单文件内稳定递增的索引，用于重组与去并发化
type Index int64
// 供插件或特定格式（如 SRT）使用；核心流程不读取具体键。
type Meta map[string]string

// 原子输入片段（不可跨文件）。扁平结构，便于拷贝与缓存友好。
type Record struct {
    Index  Index  // 0..n-1，单文件内稳定且唯一
    FileID FileID // 跟踪逻辑文档ID
    Text   string // 原始文本内容（最小必需）
    Meta   Meta  // 可选轻量扩展，核心流水线不依赖其键值，nil 表示无
}

// 上下文批。保证同源文件、按 Index 严格升序，便于 Prompt 装配。
// 仅有中间目标区间（Target）需要产出结果；两侧上下文仅用于提供语境。
type Batch struct {
    FileID     FileID   // 跟踪逻辑文档ID
    BatchIndex int64    // 同一 FileID 内 0..n-1，严格递增，用于提交顺序
    Records    []Record // 连续内存切片，Index 严格递增，形如 [L 上下文][Target][R 上下文]

    // 目标区间（闭区间，基于全局 Index，避免相对坐标歧义）
    // 约定：TargetFrom <= TargetTo，且均落在 Records 的 Index 范围内；
    // 边界处允许 Target 收缩为单点（TargetFrom == TargetTo）。
    TargetFrom Index
    TargetTo   Index
}

// 最小目标区间：从 Batch 的 TargetFrom/TargetTo 提取的只读视图。
// 用于解耦“解码/校验”对 Batch 的可见性，避免额外上下文诱因。
type Target struct {
    FileID FileID
    From   Index
    To     Index
}

// LLM 客户端返回的原始文本载荷（万能容器）。
type Raw struct {
    Text string
}

// 区间结果：由 Decoder（内含或调用校验库）生成，用于装配。
// 约束：From/To 必须位于本批 Target 区间内；同批内不重叠；按 From 升序。
type SpanResult struct {
    FileID FileID
    From   Index // 闭区间下界（全局 Index）
    To     Index // 闭区间上界（全局 Index）
    Output string
}
```

#### 1.3.2 关系与约束

- 关系
  - File 1:N Record：同一来源的片段按 Index 排序。
  - Batch 1:N Record：Batch 内只包含同一 File 的连续 Record；跨批由 BatchIndex 串联顺序。
  - Decoder 产出 `[]SpanResult`（内部可使用校验库）：
    - 逐条对齐：若干 `[i,i]` 的 Span；
    - 整段输出：单个 `[TargetFrom,TargetTo]` 的 Span。
- 约束（统一消除“特殊分支”）
  - Record 不跨文件；Batch 不跨文件；禁止交叉来源混合。
  - Index 在单文件内稳定且不重用；批间顺序由 BatchIndex 单调递增保证。
  - Span 的 From/To 必须落在本批 Target 区间内；同一批内 Span 不重叠；推荐完全覆盖 Target（逐条或整段二选一）。
  - IR 为纯数据结构，不携带业务方法；并发与调度只出现在 Pipeline 层。
  - Meta 为可选透传，核心算法不得依赖具体键，避免间接耦合。
  - Meta 的消费属于业务插件（例如 Decoder 重建结构化容器字段：SRT 序号/时间轴等）；Assembler/Writer 不读取 Meta。
  - 建议：为便于生成“JSONL 双语对照表”边车，Decoder 若能提供“仅目标文本（不含容器头部）”，可在 `SpanResult.Meta["dst_text"]` 放入纯译文文本；边车生成器在存在该键时优先使用；缺省回退到 `SpanResult.Output`。

#### 1.3.3 生命周期（CRUD 与阶段映射）

- Create：Reader 读取 -> Splitter 产出 Record 序列（Index 赋值）
- Read：Batcher 按策略切片（赋 BatchIndex）-> PromptBuilder 读取 Batch
- Update：LLMClient 生成 Raw → Decoder 解码并（可调用校验库）产出 `[]SpanResult`
- Delete：Assembler/Writer 基于 Span 顺序装配并写出

统一规则保证：流水线对所有数据以相同方式处理，边界情况（如最后一批不足、空文本）在数据结构层面自然消解，无需分支。

#### 1.3.4 复杂度与局部性

- 主承载均为切片（[]Record、[]SpanResult），顺序遍历为 O(n)，装配为 O(n)。
- 不引入映射表作为主路径；仅在必要时（如去重）由上层暂时性构建。
- 扁平字段避免指针追逐；Batch 连续存储提升缓存命中率。
- 内存回收由上层控制：批处理边界即生命周期边界，释放可预测。

#### 1.3.5 错误与校验（快速失败）

- 错误快速失败：协议/限流/超时等错误由 LLMClient 映射并上抛；内容/格式问题由 Decoder 内部（含校验）上抛，不做兜底修补。
- 校验为库函数：默认提供“逐条/整段”两套纯函数；Decoder 可直接调用，或自定义等价校验函数。

### 1.4 性能与内存模型

> 流式、单次拷贝/复用边界与对象池使用准则，避免大文件全量加载。

#### 1.4.1 目标与原则（最小集）

- 核心目标：用尽可能少的内存完成端到端处理；时间与空间的权衡围绕“批”为单位进行。
- 单点并发：仅在流水线调度器统一控制并发与背压；原子实现内部不启并发。
- 流式优先：输入/输出阶段使用流式读写，避免整文件加载；中间态只保留“当前批”。
- 简单直接：不设计复杂缓存与热路径分支；失败快速向上抛出。

#### 1.4.2 流式 I/O 边界

- 输入：`internal/io` 使用 `bufio.Reader`/固定大小缓冲块（默认 64KiB）顺序读取；格式解析在读取过程中增量进行。
- 输出：`bufio.Writer` 逐批写出，按文件维度单写者（single writer），避免锁竞争。
- 单次拷贝边界：I/O 层内部以 `[]byte` 处理，进入 IR（`Record`）时一次性转换为 `string`；核心路径不再进行二次转换或拼接拷贝。

#### 1.4.3 批处理与内存上界

- 仅保留“当前批”的 `[]Record` 与该批产出的 `[]SpanResult` 于内存中；上一批一旦写出立即可回收。
- 内存上界近似：`峰值 ≈ 并发度 × (单批字节数 + 常数开销)`；单批字节数由 `MaxTokens` 与估算系数决定。
- 批宽度由 `MaxTokens` 与固定上下文半径（ContextRadius，左右各 N 条）决定；并发度（默认 4）是上界的主要乘数；其余组件不得引入额外全量缓存。

#### 1.4.4 并发与背压（唯一入口在 Pipeline）

- Goroutine 数量由调度器按“并发度”派发，阶段串联通过有界通道连接；每条通道容量默认为 `2 × 并发度`，提供自然背压。
- 禁止在 Reader/Splitter/Writer 内部再起并发；需要并行时仅通过调度器对批进行并行。
- 取消与超时：统一用 `context.Context` 自上而下传递；收到取消时尽快排空通道并停止取数。
- 并发度 < 1 时归一为 1；通道容量下界为 1（避免零容量造成意外阻塞）。

#### 1.4.5 对象复用与池化（可选、最小）

- 仅提供两个可选的 `sync.Pool`：
  - `bufferPool`：固定大小 `[]byte` 读缓冲复用（与 I/O 块大小一致）。
  - `slicePool`：复用承载 `[]Record`/`[]SpanResult` 的切片（容量按典型窗口宽度预留，依据 C 与 `MaxTokens/平均片段 tokens` 估算）。
- 复用仅在热点路径上生效；不做跨阶段复杂生命周期管理。无法复用时直接让 GC 回收，避免过度工程化。

#### 1.4.6 限流耦合与并发夹紧

- 并发度由限流器夹紧：`effectiveConcurrency = min(userConcurrency, RPM/reqPerSec, TPM/tokensPerSec)` 的离散近似；实际实现取地板值。
- token 使用轻量启发式估算 `tokens ≈ ceil(utf8_bytes/4)`，可通过配置调整系数，`Batcher` 需保证单批 token 预算不超过 `token_per_request`。
- 若实际请求超限，由 `LLMClient` 快速报错。

#### 1.4.7 复杂度与局部性

- 时间复杂度：顺序处理为 O(n)；装配按 Index 线性合并为 O(n)。
- 空间复杂度：O(W × 并发度)，其中 W 为窗口宽度（由 C 与 `MaxTokens` 夹紧）。核心结构为切片，连续内存，缓存友好；避免在热路径上使用 map。

#### 1.4.8 失败与回收

- 快速失败：遇到不可恢复错误（解析失败、预算超限、响应无效）立即上抛并触发取消；不做多重兜底重试。
- 写出幂等：以文件为单位采用“临时文件 → 原子替换”策略可选启用；默认直接覆盖以保持最小化。

不做的事情：

- 不做 mmap 与内存映射文件（跨平台一致性与复杂度不值得）。
- 不做全局大对象缓存或跨文件交叉缓存。
- 不在架构层实现指数退避重试；重试策略留给具体 `LLMClient` 插件选择实现。

---

## 第二部分：核心能力层

### 2.1 原子操作与接口契约

> 明确最小可复用能力与输入/输出、幂等与错误语义。

#### 设计目标（最小必需）

- 数据优先：统一以 IR（`Record/Batch/Raw/SpanResult` 等）作为输入输出的唯一载体。
- 幂等可替换：同样输入必然产生同样输出；实现可随时替换而不影响编排。
- 无内部并发：原子操作实现不得自起并发；并发仅由流水线调度器统一控制。
- 仅做一件事：不跨越职责边界，不做隐藏 I/O、缓存或重试。
- 快速失败：错误立即上抛；不提供多层兜底与补丁。

#### 统一契约与约束

- `context.Context`：所有接口首参为 `ctx`，用于取消与超时；收到取消应尽快返回。
- 输入/输出：仅接受并返回 IR 与最小必要类型；严禁返回实现私有的具体结构体指针以形成耦合。
- 顺序与对齐：按 `FileID` 串联；跨批顺序由 `BatchIndex` 保证；Decoder 产出的 `SpanResult` 必须位于本批 Target 区间内且同批内不重叠；装配按 `From` 升序线性合并。
- 内存边界：每次仅处理“当前批”；不得构建跨文件/跨批的全量缓存。
- 错误语义：错误需可判定与分类（如：输入无效/限流/响应无效/IO/取消）。
- 错误语义：错误需可判定与分类（如：输入无效/限流/响应无效/装配序列无效/IO/取消）。
- 日志与指标：原子操作内部不强制打日志；可通过可选的观测接口上报，默认无副作用。

#### 错误分类（建议）

以下为建议分类与示例命名，不构成强制常量名规范；实现可采用自定义错误类型/哨兵值，但需保持语义一致，便于上层策略处理。

- `ErrInvalidInput`：输入不符合契约（例如空内容、非法编码）。
- `ErrRateLimited`：达到 RPM/TPM/Token 限制或服务端速率限制。
- `ErrResponseInvalid`：响应缺失所需字段或无法按 `Index` 对齐。
- `ErrSeqInvalid`：装配阶段序列违规（`FileID` 混入、逆序、区间重叠）。
- `ErrIO`：底层读写失败（Reader/Writer）。
- `ErrCanceled`：由上层 `ctx` 取消触发，需立即返回。

#### 接口形状（Go 形状示意，仅表意）

说明：以下接口建议位于 `pkg/contract`（位置为建议并非强制），仅定义能力与数据契约，不包含任何业务实现；默认实现（可选）位于 `plugins/*`，并可被用户实现替换。

```go
package contract

import (
    "context"
    "io"
)

// Reader: 输入源抽象（文件/目录/STDIN）。
// 约束：
// 1) 流式读取，按文件维度回调；
// 2) FileID 稳定且去平台差异化；
// 3) 不做解码/业务解析，仅提供字节流；
// 4) 不在内部起并发。
type Reader interface {
    Iterate(ctx context.Context, roots []string, yield func(fileID FileID, r io.ReadCloser) error) error
}

// Splitter: 将单文件字节流拆分为有序 Record 序列，并分配 Index（0..n-1）。
// 约束：
// 1) 不跨文件合并；
// 2) Index 严格递增且稳定；
// 3) 不改变文本语义（Text 原样透传或做最小必要清洗）。
type Splitter interface {
    Split(ctx context.Context, fileID FileID, r io.Reader) ([]Record, error)
}

// Batcher: 将 Record 切分为 Batch（同一 FileID，按 Index 连续且有序），并为每批赋 `BatchIndex`。
// 约束：
// 1) 仅在同一 FileID 内成批；
// 2) 遵循 token 上限（由配置传入），上下文条数 C 固定（由实现配置提供）；
// 3) 不重排、不丢失；批内排列约定为 [L 上下文][Center 目标][R 上下文]（滑动窗口）。
// 4) 每个 Batch 必须设置 TargetFrom/TargetTo，仅 Target 区间的结果会被上游保留并参与装配；上下文区间的结果将被丢弃。
type Batcher interface {
    Make(ctx context.Context, records []Record, limit BatchLimit) ([]Batch, error)
}

// BatchLimit: 最小必要限制集合（滑动窗口模型）。
type BatchLimit struct {
    MaxTokens  int   // 每批最大 token 预算（近似估算）；上下文条数 C 由 Batcher 实现配置提供
}

// PromptBuilder: 将 Batch 装配为请求载荷（Prompt）。
// 约束：
// 1) 不访问外部状态；
// 2) 仅基于 Batch 构造确定性的 Prompt；
// 3) 不做网络与磁盘 I/O。
type PromptBuilder interface {
    Build(ctx context.Context, b Batch) (Prompt, error)
}

// Prompt: 不透明载荷，形状由实现定义；
// 约束：必须可被 LLMClient 消费，且可从同一 Batch 确定性重建；
// 架构不规定任何具体字段与编码形式。
type Prompt any

// LLMClient: 以 Batch+Prompt 为单位与大模型交互，返回原始文本 Raw。
// 约束：
// 1) 不在内部实现并发与全局重试；
// 2) 命中限流返回 ErrRateLimited；
// 3) 不做内容/格式解析与拆分。
type LLMClient interface {
    Invoke(ctx context.Context, b Batch, p Prompt) (Raw, error)
}

// Decoder: 将 Raw 解码为候选区间（协议多样性在此收敛；架构不规定字段/格式）。
// 约束：
// 1) 强依赖业务协议与 Prompt 约定；
// 2) 无内部并发；错误快速返回 ErrResponseInvalid；
// 3) 返回位于 Target 的非重叠、按 From 升序的 []SpanResult（建议使用校验库函数保证不变式）。
type Decoder interface {
    // 解码并返回最终的 []SpanResult；实现可调用架构提供的校验库函数，或完全自定义等价校验。
    Decode(ctx context.Context, tgt Target, raw Raw) ([]SpanResult, error)
}

// 校验库函数（默认实现，供 Decoder 直接调用）：
//  - ValidatePerRecord: 要求候选形如 [i,i] 且连续覆盖 [TargetFrom..TargetTo]
//  - ValidateWhole:    要求候选为单个区间，且恰为 [TargetFrom,TargetTo]
// 实现说明：纯计算、无 I/O、错误快速返回 ErrResponseInvalid；返回值为最终 `[]SpanResult`。

// Assembler: 基于 SpanResult 的 From/To 线性装配为最终文本（单文件）。
// 约束：
// 1) 仅对同一 FileID 的 Span 进行装配；
// 2) 按 From 严格升序拼接；
// 3) 不引入跨文件状态；
// 4) 序列违规（FileID 混入/逆序/重叠）返回 ErrSeqInvalid（由 pkg/contract 定义）。
type Assembler interface {
    Assemble(ctx context.Context, fileID FileID, spans []SpanResult) (io.Reader, error)
}

// Writer: 将装配后的单文件内容以流式方式写出。
// 约束：
// 1) 单写者原则；
// 2) 默认直接覆盖写（最小化），可选原子替换由上层选择；
// 3) 不在内部缓存跨文件数据。
type Writer interface {
    Write(ctx context.Context, fileID FileID, content io.Reader) error
}
```

#### 统一行为语义

- 幂等性：对同一 `FileID` 与相同 `Batch`/`Prompt` 输入，多次调用产出等价的 `Raw/SpanResult`（LLM 不可控差异除外）。
- 流式与内存：Reader/Writer 必须流式；其余组件使用切片承载，仅处理“当前批”。
- 并发唯一性：上述接口均为同步调用；并发调度仅存在于 `internal/pipeline`。
- 容器保证：`LLMClient` 返回 Raw；`Decoder` 返回位于 Target 的非重叠 `[]SpanResult`；Pipeline 以 `BatchIndex` 保证跨批提交顺序。
- 失败策略：遇到不可恢复错误立即上抛；不做业务回退与补丁。

本节仅定义架构目标与契约，不涉及任何具体业务实现或策略细节。

### 2.2 原子操作清单（能力矩阵）

> Reader/Splitter/Batcher/PromptBuilder/LLMClient/Decoder/Assembler/Writer 的职责边界与扩展点（校验为库函数，不作为插件）。

以下组件均遵循 2.1 的统一契约与约束（幂等、无内部并发、对齐、快速失败、流式 I/O 边界），此处仅列出各自的职责、输入/输出与参考配置。

注：下文出现的“可配置”项均为常见选项示例，非强制规范，不构成接口契约的一部分。

- Reader（输入源）
  - 职责：遍历输入路径集合，按文件维度产出字节流与规范化 `FileID`。
  - 输入：`roots []string`
  - 输出：`(fileID, io.ReadCloser)` 回调式迭代
  - 可配置：忽略模式、最大深度、隐藏文件策略

- Splitter（拆分器）
  - 职责：将单文件字节流解析为有序 `[]Record`，并赋予稳定 `Index`
  - 输入：`fileID, io.Reader`
  - 输出：`[]Record`
  - 可配置：编码/换行规则、最大片段长度、格式模式（如通用行分割/标注分段）

- Batcher（批处理器）
  - 职责：在同一 `FileID` 内将 `[]Record` 切分为若干 `[]Batch`（Index 连续、有序）
  - 输入：`[]Record, BatchLimit`
  - 输出：`[]Batch`（每个 Batch 标注 `BatchIndex` 与 `TargetFrom/TargetTo`；仅 Target 区间参与装配）
  - 可配置：固定上下文条数 C、`MaxTokens`（估算系数）

- PromptBuilder（提示词装配）
  - 职责：基于 `Batch` 构造确定性的 `Prompt`
  - 输入：`Batch`
  - 输出：`Prompt`
  - 可配置：模板路径/字符串、占位符策略、语言/风格参数

- LLMClient（大模型客户端）
  - 职责：将 `Batch+Prompt` 发送至大模型，返回原始文本 `Raw`
  - 输入：`Batch, Prompt`
  - 输出：`Raw`（不做内容/格式解析与拆分）
  - 可配置：模型名、温度/最大 tokens、RPM/TPM、超时

- Decoder（解码器）
  - 职责：将 `Raw` 解码并（可调用校验库）产出最终 `[]SpanResult`（协议自定义）
  - 输入：`Target, Raw`
  - 输出：`[]SpanResult`
  - 可配置：协议/字段名/宽松度（由业务决定；架构不设默认）；默认可复用架构提供的两种校验函数

  - 校验库函数（非插件，仅供复用）：
    - ValidatePerRecord(tgt, cands) → []SpanResult
    - ValidateWhole(tgt, cands) → []SpanResult

- Assembler（装配器）
  - 职责：按 Span 的 `From` 升序线性合并为最终内容流
  - 输入：`fileID, []SpanResult`（来自各批 Target 的区间结果）
  - 输出：`io.Reader`
  - 可配置：分隔符/拼接策略、尾随换行规则

- Writer（输出器）
  - 职责：将装配后的内容以流式方式写回目标介质
  - 输入：`fileID, io.Reader`
  - 输出：无（错误上抛）
  - 可配置：输出根目录、覆盖/原子替换策略、权限/编码

### 2.3 插件与扩展点

> 扩展接口、注册机制。

本节仅描述扩展机制；并发/错误等通用约束请参见 2.1，不在此重复。

#### 注册与发现（参考示例，非规范）

以下代码为参考示意，用于说明扩展机制目标；具体键名/API 以参考实现为准，不构成架构规范的一部分。位于 `pkg/registry` 的最小注册器（可选）：

```go
package registry

import "github.com/your/mod/pkg/contract"

// 分类键（固定枚举）
const (
    KReader         = "reader"
    KSplitter       = "splitter"
    KBatcher        = "batcher"
    KPromptBuilder  = "promptbuilder"
    KLLMClient      = "llmclient"
    KDecoder        = "decoder"
    KAssembler      = "assembler"
    KWriter         = "writer"
)

type Factory func(cfg map[string]any) (any, error)

// 显式注册/获取（Map + 互斥保护，省略实现细节）
func Register(kind, name string, f Factory)
func Get(kind, name string) (Factory, bool)

// 组装期由 cmd/internal 调用：
//   fac, _ := registry.Get(registry.KSplitter, cfg.Name)
//   x, _ := fac(cfg.Options)
//   splitter := x.(contract.Splitter)
```

说明：

- 显式依赖：通过工厂函数创建实例，不做反射与隐式注入。
- 轻量注册：以“接口类别 + 实现名”注册与选择，实现可替换。
- 编译期保证：不做运行时版本协商；接口兼容由编译期约束。
- 规范要求仅包括：显式注册 + 工厂创建 + 编译期类型检查；不固定键名/函数签名与包路径。

#### 配置与装配约定（结果导向）

- 选择器：Reader/Splitter/Batcher/PromptBuilder/Decoder/Writer 按实现名选择；LLM 通过顶层 `llm` 选择命名 provider，再由 `provider.<name>.client` 指定实现、`provider.<name>.options` 透传参数（示例：`llm=fast_openai` 且 `provider.fast_openai={ client:"openai", options:{...} }`）。
- 选项：每个插件有自己的 `Options` 子树（最小必要）。
- 依赖：共享对象（如 `*http.Client`、限流器）由组装层显式注入 `Options`。
- 必选项：Decoder 必须显式配置（与 Prompt/协议强相关，架构不提供默认）；未配置视为装配错误。校验作为库函数提供，不需要配置。

#### 生命周期与资源管理（可选）

- 插件持有资源时可实现 `io.Closer`，由流水线在结束时统一 `Close()`。
- 不定义 `Start()`/`Stop()` 之类复杂生命周期。

注：扩展路径与替换方式参见 1.2 的“迁移与扩展路径”。

---

## 第三部分：处理流程层

### 3.1 输入源与文件读取

> 抽象文件/目录/流式输入，统一路径收集与迭代。

#### 目标（最小必需）

- 统一输入抽象：文件、目录、STDIN 三类输入统一为 `(FileID, io.ReadCloser)` 的有序流。
- 流式读取：不整文件加载；按固定缓冲块顺序读取，便于下游增量解析。
- 可复现顺序：同一输入集产生稳定遍历顺序，利于结果一致性与调试。
- 简单直接：不在 Reader 内部做并发、缓存或复杂过滤；错误快速上抛。

#### 数据与接口

- `FileID`：使用 1.3 中的规范化函数，确保跨平台一致（路径分隔符归一）。
- `Reader`（契约见 2.1）：输入 `roots []string`，按序回调产出 `(fileID FileID, r io.ReadCloser)`。
- 数据形态：Reader 仅产出字节流；文本解码/拆分由 `Splitter` 负责（见 3.2）。

#### 行为规范

1. 输入来源
   - 文件路径：直接 `open` 返回单一 `(FileID, ReadCloser)`。
   - 目录路径：递归遍历目录树；每个常规文件各产出一次流。
   - STDIN：当 `roots` 为空或包含单一 `"-"` 时，产出一次 `(FileID="stdin", os.Stdin)`；禁止与其他根混用。

2. 遍历与顺序
   - 目录遍历采用“逐目录读取 -> 对该目录下条目按字典序排序 -> 依次处理”的策略；既保证稳定顺序，又避免全量收集再排序的全局内存占用。
   - 对同级条目：目录优先于文件（均按字典序），确保路径顺序一致且可预测。

3. 过滤与范围（默认最小化）
   - 仅处理常规文件与指向常规文件的符号链接；不跟随目录符号链接（避免环）。
   - 契约层不规定忽略/过滤；允许具体 Reader 插件通过 Options 提供“最小过滤能力”，且默认关闭以保持最小化。
     - 示例：filesystem.Reader 支持 `ExcludeDirNames []string`（按目录基名、小写匹配）。递归时命中即跳过，不影响单文件 root。

4. I/O 与缓冲
   - 打开文件后直接返回 `io.ReadCloser`；调用方负责消费与关闭。
   - 底层使用 `bufio.Reader`/固定块（默认 64KiB）以提升吞吐与缓存友好；Reader 自身不做二次拷贝。

5. 错误处理
   - 不存在的路径、权限拒绝、损坏的符号链接等：触发首错取消并停止推进。
   - 单文件读取中途失败：触发首错取消；不重试/不兜底。

6. 资源与生命周期
   - Reader 不持久化跨文件状态；每个文件句柄在交付给下游后由下游负责 `Close()`。
   - Reader 自身可实现 `io.Closer`，用于结束时释放内部可选资源（如自定义缓冲池）。

### 3.2 文本拆分与数据集构建

> 拆分策略、索引标注与上下文保留的边界。

#### 目标（最小必需）

- 将单文件字节流在不引入并发的前提下，解码并拆分为有序、稳定的 `[]Record` 数据集。
- 对每个片段赋予单文件内稳定递增的 `Index`，为后续批处理、装配提供唯一对齐依据。
- 拆分策略由插件实现决定；架构仅规定输入/输出形状与行为边界，不介入任何业务语义。

#### 数据与接口

- 输入：`fileID FileID, r io.Reader`
- 输出：`[]Record`（按 `Index` 严格升序，来源一致且不跨文件）
- 契约：实现自 `pkg/contract.Splitter`，遵循 2.1 的幂等、无内部并发、快速失败与对齐约束。

#### 行为规范（拆分与数据集构建）

1. 解码与规范化
   - 文本解码在 Splitter 内完成，优先使用 UTF-8；遇非法字节快速失败返回解码错误。
   - 换行归一：将 CRLF 归一为 `\n`；禁止保留平台相关差异进入 IR，作为 IR 统一性的最小必需规范。
   - 业务性归一不设默认：如裁剪、压缩空白等由具体 Splitter 插件自行定义与配置；核心流程不设默认，也不读取其配置。

2. 拆分策略（由实现决定）
   - 典型模式：按行、按空行分段、按标记块、按结构化格式（如 SRT）的段落。
   - 架构不规定“如何拆”，只规定“拆完之后是什么样的数据”。

3. 索引与顺序
   - `Index` 自 0 起，单文件内严格递增，且与输入顺序一致；禁止回填、禁止跨文件复用。
   - `Record.FileID = fileID` 必须一致；禁止跨源混排。

4. 片段大小与上界（求结果所需的唯一约束）
   - 为保证后续批处理可行，`Record.Text` 应不超过可配置的最大字节或字符上限（例如 `MaxFragmentBytes` 或 `MaxRunes`）。
   - 若单个片段已超过上限，Splitter 应快速失败并提示调小拆分粒度或增大预算（这是为“能产出结果”设置的必要边界，非业务逻辑）。
   - 参数语义：`MaxFragmentBytes`/`MaxRunes` 支持 0 表示“不限制”；非 0 时严格执行并快速失败。
   - 推荐默认：给出保守默认（例如 512KB），并建议常见范围 256KB~1MB；明确该值可按输入特征与资源调优。
   - Token 预算属于 3.3 的批处理职责；Splitter 不引入分词器/模型相关依赖。

5. 内存与数据局部性
   - 使用顺序读取与线性写入 `[]Record` 的方式构建数据集；避免中途全量复制与 map 结构作为主路径。
   - 构建完成后一次性返回 `[]Record`；上游/下游以“当前文件”为生命周期边界，释放可预测。

6. 错误处理（快速失败）
   - 按最小分类返回错误（如解码错误、片段过大、I/O 失败），触发首错取消；不兜底、不多重重试。
   - 结构化格式的“格式错误”（如 SRT）属于具体 Splitter 的业务错误，不在通用契约中定义。
   - 对空文件或经拆分后为空的情形，返回长度为 0 的切片而非错误。

7. 元数据 `Meta` 的使用
   - `Record.Meta` 为可选透传，供业务型 Splitter（如 SRT：序号、时间轴）写入；核心流程不读取其键值。
   - Meta 不影响索引与顺序；任何依赖 Meta 的行为属于上层业务插件的责任。

8. 文件范围与扩展名（实现可选）
   - 契约层不规定“允许的文件后缀/扩展名”；具体 Splitter 可通过 Options 提供最小过滤能力。
   - 例如：某些 Splitter 实现可提供 `AllowExts []string`（大小写不敏感、含点）；默认值与是否允许“显式空切片表示不限制”等策略由具体实现自行定义，架构不作规定。
   - 当文件扩展名不匹配时，Splitter 应直接返回空结果且不报错，以统一数据流、消除不必要的特判。

#### 不做的事情

- 不在架构层做启发式语言检测、复杂分词、纠错与内容清洗。
- 不引入跨文件的全局状态或缓存来优化拆分。
- 不为极端异常文本提供多重补丁或降级路径；遇到不可恢复错误快速失败。

### 3.3 批处理与上下文窗口

> 用最小必要的滑动窗口把“可变目标 + 固定上下文”装入单批，确保对齐与可装配；架构只关注能产出结果的边界，不干预任何业务规则。

#### 3.3.1 目标与范围（Span-first）

- 目标：把同一 `FileID` 的 `[]Record` 切成若干 `Batch`，每个 `Batch` 携带固定上下文与一个连续“目标区间（Target）”；仅 Target 区间的结果进入最终输出，上下文只提供语境。
- 范围：不处理任何业务语义（如翻译/润色/关键词）；仅保证批内顺序、整体 token 预算与结果对齐可装配。
- 统一性：所有批遵循相同规则；边界情况（首尾不足 C 条上下文）自然收缩，无需分支。

#### 3.3.2 输入/输出

- 输入：`[]Record`（同一 `FileID`、`Index` 严格递增）、`BatchLimit{ MaxTokens }`
- 输出：`[]Batch`（每个 `Batch.Records` 为连续切片，设置 `BatchIndex` 与 `TargetFrom/TargetTo` 为全局 Index 闭区间）
- 对齐：`LLMClient` 返回原始文本 `Raw`；`Decoder` 产出位于 `Index ∈ [TargetFrom,TargetTo]` 的 `[]SpanResult`；上下文不产出 Span。
- 校验：若输入 `[]Record` 出现跨 `FileID` 或 `Index` 非严格递增，Batcher 必须立即报错（快速失败）。

#### 3.3.3 必要数据与参数（最小集）

- 固定上下文条数：ContextRadius（左右各 N 条；边界不足即为可用条数）。该值不属于公共接口契约，仅由 Batcher 实现配置提供；架构不规定其来源与取值策略。
- Token 预算：`MaxTokens`（单请求上限，估算值）
- 预算预留：由编排层根据 Prompt 模板与固定规则在进入批处理前“预扣预算”，将扣减后的 `MaxTokens` 传入 `Batcher.Make`（Batcher 不再暴露开销参数）。
- Token 估算器（可选，构造时传入 Batcher，非接口契约）：`TokenEstimator(text string) int`
  - 默认可使用字符近似（如 `ceil(len(runes)/4)`）；架构不绑定分词器实现

#### 3.3.4 成批算法（O(n)；滑动窗口 + 前缀和）

设计要点：一次线性扫描生成不重叠的 Target 区间；每条 `Record` 恰好作为 Target 覆盖一次，从而消除“结果去重”的特殊分支。

步骤：

1) 预估 tokens：对每个 `Record` 估算 `ti = TokenEstimator(r.Text)`，构建前缀和 `prefix[i] = t0 + ... + t(i-1)`；查询任意区间开销为 O(1)。
2) 双指针贪心：从 `l = 0` 开始，将左上下文 `L = max(0, l-C) .. l-1` 固定；在预算内尽可能扩张“目标区间” `center = l .. r`；右上下文 `R = r+1 .. min(n-1, r+C)` 随 r 变化。
3) 预算校验：`budget = MaxTokens`（该 MaxTokens 已由编排层预扣减“固定开销”）；若 `tokens(L)+tokens(center)+tokens(R) <= budget` 则推进 `r++`，否则停止扩张。
4) 产出批：
   - `Records = [L .. R]`（连续切片，按 `Index` 升序）
   - `TargetFrom = records[l].Index`，`TargetTo = records[r].Index`
   - 追加到结果集
5) 推进窗口：`l = r + 1`，重复步骤 2~4 直到覆盖完所有记录。

边界与失败：

- 若仅 `L+R` 已超过 `budget`，快速失败：提示减小上下文半径或增大 `MaxTokens`/减少模板开销。
- 若 `1 条目标 + L + R` 仍超限，快速失败：提示细化 Splitter 粒度或调整 `budget`（架构不做二次拆分）。

复杂度与局部性：

- 时间 O(n)，空间 O(n)（仅 `ti/prefix` 两个整型切片）；`Records` 为连续切片引用，不复制底层数据。
- 规模提示：实现可按需采用分段/分页前缀和以降低峰值占用（非契约，默认无需预优化）。

注：`tokens(a,b)` 基于 `prefix` 常数时间求和；真实实现需处理越界与空区间为 0。

#### 3.3.5 区间结果与装配（去并发歧义）

- 选择规则（全局、唯一）：Pipeline 仅接受位于 `Index ∈ [TargetFrom,TargetTo]` 的 `SpanResult`；上下文不产出 Span。
- 覆盖保证：Target 区间是对 `[]Record` 的一次性覆盖分割；Decoder 产出的 Span 在同批内不重叠，装配无需去重或“最后写入获胜”。
- 业务无关：不读取 `Meta`，不根据内容做差异选择；仅按 Span 的区间与顺序拼接。
- 顺序门闩：跨批写出按 `BatchIndex` 单调提交；批内按 `From` 升序线性拼接。

#### 3.3.6 预算与估算（解耦业务）

- 架构只关心“是否可装入批”的布尔结果与上限约束；估算细节通过 `TokenEstimator` 注入，默认可采用字符近似。
- 固定开销在编排层计算并预扣；架构不推断模板长度/业务输出规模。
- 任何分词器/模型耦合只可出现在实现层，而非架构契约中。

#### 3.3.7 失败策略（快速失败）

- 分类：`上下文过大`、`单目标不可装载`、`预算小于最小开销`；命中即返回错误并触发首错取消。
- 不做：不自动重试；不自动调整上下文宽度/批大小；不回退到“逐条无上下文”或“剪裁文本”；不对 `Record` 进行二次细分。需调用方调整预算或拆分策略后重试。

#### 3.3.8 并发与内存（与 1.4 保持一致）

- 并发：批次并发仅由 Pipeline 调度；Batcher 自身不启并发。
- 内存：仅保留“当前批”的 `[]Record`/`[]SpanResult`；上批写出后立即可回收；`ti/prefix` 为整型切片，常量级内存。
- 顺序与回收：Batch 列表按 `BatchIndex` 递增产出，便于顺序回收与流式写出；实现不保留历史批的 `[]Record`/`[]SpanResult`。

### 3.4 上下文管理与传递

> Context 传递机制、取消信号、超时控制与元数据携带。

#### 3.4.1 目标与范围（最小必需）

- 唯一机制：使用 `context.Context` 自上而下贯穿流水线，统一取消/超时/少量关联系统元数据。
- 结果优先：架构只保证“能及时终止/能按期返回/可关联日志”；不承载任何业务语义，不通过 Context 传业务数据。
- 非目标：不在架构层设定业务重试/退避、不把 Logger 或复杂对象塞进 Context、不做跨组件的隐式参数传递。

#### 3.4.2 基本规则（Contract-level）

- 统一签名：所有契约方法均以 `ctx context.Context` 为首参（已体现在 `Reader`/`Splitter`/`Batcher`，其余组件沿用）。
- 只做控制面：Context 仅用于取消、超时与极少的相关性 ID；禁止承载数据面信息（如 `FileID/Index/Text`），这些都在 IR 中显式传参。
- 禁止策略透传：禁止从 Context 读取任何业务配置或策略参数（如语言、阈值、模板 ID、并发度、配额、超时策略等）；此类信息必须通过 IR 或组件 Options 显式传入。
- 只传引用：不复制 Context；派生 Context 仅在需要更短的生命周期或更严格的时限时进行。
- 快速响应：所有实现需在长循环/阻塞点检查 `ctx.Done()`，尽快返回 `ctx.Err()`。

#### 3.4.3 生命周期与传播边界

- 源头：`cmd/llmspt` 使用外部 Context（如 `signal.NotifyContext`）作为根 Context 传入 `pipeline.Run`。
- 统一取消：`pipeline.Run` 以 `context.WithCancelCause` 包装根 Context；首次不可恢复错误触发取消，作为“失败原因”向下游传播。
- 批级派生（可选）：为单批处理派生 `batchCtx := context.WithCancel(parentCtx)`；用于在单批触发早停时尽快释放资源（非强制）。
- 请求级派生（可选）：`LLMClient` 可在其实现内为单次 API 请求派生 `WithTimeout`（若实现的 Options 指定）；架构不强制默认超时。

- 批级约束：批级派生仅用于提前释放资源；不得延长上游截止时间；不得在批级 Context 添加任何业务键或控制量。

#### 3.4.4 取消与超时策略

- 第一错即取消：任一阶段出现不可恢复错误，调度器调用 `cancel(cause)`；其他 Goroutine 应尽快感知并返回。
- 无默认超时：架构本身不强制设定全局/批级默认超时；如需限定，由调用方在根 Context 设置 Deadline，或由具体实现（如 `LLMClient`）在其 Options 中启用请求级超时。
- 有效时限取最小：若根 Context 已设 Deadline，派生时不得延长；实现层若叠加 `WithTimeout`，生效的 Deadline 为二者的最小值。
- 清理与退出：收到取消信号后，生产者停止投递、消费者尽快退出循环；Writer 在保证输出一致性的前提下完成已接收数据的最小化落盘。

- 因果权威：以 `cancel(err)`（来自 `context.WithCancelCause`）作为权威的取消原因；`errgroup` 仅用于 fan-out 取消。工作协程遇到致命错误时先调用 `cancel(err)`，再 `return err`；若为取消/超时，直接返回 `ctx.Err()`，不做二次包装。

#### 3.4.5 元数据携带（Correlation-only）

- 最小键集：仅允许携带“相关性 ID”（例如 `corr_id`），用于日志与观测性关联；类型安全，使用私有 key 避免冲突：
- 单一来源：ctx key 类型应私有化并集中定义在单一包中，避免多处定义与冲突。
- 禁止：在 Context 中传递业务元信息（语言、模板、阈值等）；此类信息应体现在 IR（如 `Record.Meta`）或实现的 Options 中，由组件显式读取。
- 观测解耦：日志/指标组件仅读取 `corr_id`；不要求在 Context 中注入 Logger 句柄或追踪器对象。

#### 3.4.6 与并发/背压的衔接（单点控制）

- 有界通道：调度器以有界通道连接阶段；当 `ctx.Done()` 关闭时，生产端停止写入，消费端在 drain 最小必要数据后退出。
- 资源释放：批级 Context 取消应释放 LLM 连接/HTTP 请求；Reader/Splitter 停止读取并关闭当前文件句柄。
- 无额外状态：不在 Context 存放“并发度/速率/配额”等控制量；并发与限流均由调度器与限流器显式管理（见 1.4/3.6）。

#### 3.4.7 契约补充与一致性

- 契约一致性：除已定义的 `Reader/Splitter/Batcher` 之外，其余组件保持同步、无内部并发的最小签名：
  - `PromptBuilder.Build(ctx, batch) (Prompt, error)`
  - `LLMClient.Invoke(ctx, batch, prompt) (Raw, error)`
  - `Decoder.Decode(ctx, target, raw) ([]SpanResult, error)`
  - `Assembler.Assemble(ctx, fileID, spans) (io.Reader, error)`
  - `Writer.Write(ctx, fileID, r io.Reader) error`
- 错误传播：所有实现遇到取消/超时必须返回 `ctx.Err()` 或带因的错误；不得吞错或内部重试到不可预测程度。校验错误由 Decoder 内部抛出。

- 禁止持久化：实现不得将 `ctx` 存入结构体字段或跨调用保存；仅以参数形式在调用栈中向下传递。
- 取消顺序：致命错误先调用 `cancel(err)` 再 `return err`；如为取消/超时场景，直接 `return ctx.Err()`，不进行二次包装。

#### 3.4.8 不做的事（边界收紧）

- 不将业务决策（重试、回退、内容降级）放入 Context；架构不读取此类信号。
- 不在 Context 中传输数据面负载（文本、索引、结果）；仅用 IR 显式传参。
- 不在架构层生成或要求全局追踪系统；仅提供可选的相关性 ID 钩子。
- 不通过 Context 集成日志/追踪/限流器；如需观测性集成，在应用边界建立适配，不改动核心契约。

### 3.5 Prompt 模板与组装

> 最小必需：以 Batch 为唯一输入，确定性地产生 Prompt（`any`），不做网络/磁盘 I/O，不隐式改写业务内容；仅定义模板绑定与渲染规则，业务语义完全由模板决定。

#### 3.5.1 目标与边界（结果优先）

- 目标：将 `Batch` 装配为可被 `LLMClient` 消费的 `Prompt` 载荷（形状由实现定义），以便执行“一次请求 -> 一次返回”。
- 边界：
  - 不读取外部状态（网络/磁盘/环境变量）；
  - 不推断/改写业务意图（不追加系统指令、不清洗/截断文本、不合并/拆分批次）；
  - 不进行 token 预算与限流计算（见 3.3/3.7）；
  - 仅对 `Batch` 做只读、确定性的格式化与拼装；
  - 失败快速返回：模板解析/绑定错误统一归类为“输入无效”错误（最小分类）。

#### 3.5.2 数据绑定（Batch → View）

为消除模板中的条件分支与复杂逻辑，定义最小且通用的绑定视图（供模板引用）：

```go
// 仅为模板数据形状的说明，非强制导出类型
type PromptView struct {
    FileID     FileID
    BatchIndex int64
    TargetFrom Index
    TargetTo   Index

    // 三段式视图：左上下文 / 目标区间 / 右上下文
    Left   []RecordView // Index < TargetFrom，按 Index 升序
    Target []RecordView // TargetFrom <= Index <= TargetTo，按 Index 升序
    Right  []RecordView // Index > TargetTo，按 Index 升序

    All    []RecordView // = Left + Target + Right（连续切片，按 Index 升序）
}

type RecordView struct {
    Index Index
    Text  string
    Meta  map[string]string // 可为 nil；模板可选引用
}
```

绑定规则：

- 来源：完全由 `Batch` 派生（`b.Records` 与 `b.TargetFrom/TargetTo`），不读取额外上下文。
- 顺序：所有切片均按 `Index` 严格升序，便于线性遍历与拼接（缓存友好）。
- 只读：视图构造不修改 `Batch`；模板渲染对视图的访问为只读。
- 一致性：`Target` 非空（闭区间）；若 `TargetFrom == TargetTo` 则为单点目标。

#### 3.5.3 模板形态（最小两类）

为覆盖主流上游协议，同时保持“Prompt=any”的解耦，推荐但不强制两类模板产物：

- 文本型（`type=text`）：输出单一字符串，适配 completion 类接口。
- 会话型（`type=chat`）：输出 `[]Message{ {role, content}, ... }` 的结构，用于 chat 类接口；消息条数不限，常见为两条（system + user）或单条（user）。

示意结构（仅表意，非强制导出）：

```go
// 文本型 Prompt
type TextPrompt string

// 会话型 Prompt（最小集合）
type Message struct { Role, Content string }
type ChatPrompt []Message
```

配对关系：`PromptBuilder` 的产物形状需与 `LLMClient` 的实现显式配对（见 3.6.3 “配对约定”）。架构不为二者建立隐式转换。

#### 3.5.4 占位符与渲染（最小集合）

- 模板引擎：采用 Go `text/template` 的子集能力（无需自定义 DSL）。
- 占位符：`{{ .Field }}` 访问 `PromptView` 字段；`range` 遍历切片。
- 函数：架构契约不定义任何自定义函数。`text/template` 内建（如 `len`/`printf`）可用。参考实现可提供通用函数（如 `joinTexts`）以减少模板样板，但这不构成接口要求。
- 编码：模板层不负责 JSON/HTTP 编码；`LLMClient` 在 3.6 中执行传输层编码。若业务需要 JSON 片段，建议模板直接产生最终字符串或由 PromptBuilder 在返回形状内组装字段，而非在模板中手写转义逻辑。

渲染约束：

- 渲染必须纯内存完成，运行期禁止任何 I/O（模板文件如需加载仅在构造期完成）。
- 不隐式附加系统指令/后缀；模板写什么，Prompt 就是什么。
- 失败即返回：解析失败/缺少必需字段/函数调用错误 → 输入无效错误（最小分类）。

#### 3.5.5 构建流程（确定性）

基于 Batch 的只读数据绑定，纯计算渲染为 Prompt；运行期无 I/O；失败即返回输入无效错误（最小分类）。

#### 3.5.6 与预算/限流协作

- PromptBuilder 必须实现“固定开销估算”接口：`EstimateOverheadTokens(estimate TokenEstimator) int`，
  仅估算与批无关的固定提示开销（system/glossary/固定规则/schema），用于编排层预扣预算；运行期仍不测量/不 I/O。
- 当实际请求命中上游限额，由 3.6 的 `LLMClient` 返回“限流/节流”错误类别；PromptBuilder 不做回退或二次拆分。

#### 3.5.8 错误与分类（快速失败）

- 输入无效：模板不存在/解析失败/渲染失败/占位符引用缺失。
- 仅做分类并返回；不重试/不兜底填充默认内容。

#### 3.5.9 不做的事（边界收紧）

- 不自动注入系统指令/样式后缀；
- 不做 JSON/协议编码（传输层职责）；
- 不做动态 token 预算、截断或内容清洗（仅提供固定开销估算接口）；
- 不读取网络/磁盘（除构造期加载模板外，运行期不 I/O）；
- 不维护跨批/跨文件状态；
- 不对模板函数/消息角色做任何契约级默认规定（相关能力仅可在参考实现中提供）。

### 3.6 LLM 客户端

> 用最小必要抽象把 Batch+Prompt 变成 Raw；不做并发与策略，不介入业务；只对外保证可用性、可替换性与错误可判定性。

#### 3.6.1 目标与范围（结果优先）

- 目标：给定 `Batch` 与 `Prompt`，以同步调用方式获得 `Raw` 文本载荷；正确传播取消/超时；将上游可识别的错误映射为最小分类并上抛。
- 范围：不实现内部并发、重试、缓存、限流与内容解析；只负责“请求一次 → 返回一次”。
- 统一性：隐藏传输细节（HTTP/gRPC/SSE 等），对上只暴露契约 `Invoke(ctx, b, p) (Raw, error)`；`Prompt` 的具体形状由与之配套的 `PromptBuilder` 决定，架构不介入二者的匹配细节。

#### 3.6.2 输入/输出（契约复述）

- 输入：
  - `Batch`：仅读取（含 `Records` 与 `TargetFrom/TargetTo`），不修改；`Target` 信息供上游/下游对齐使用，LLM 客户端无需感知其语义。
  - `Prompt`：不透明载荷（`any`），由具体实现解释；架构不规定字段。
- 输出：
  - `Raw`：原始文本（字符串），不做内容解析与结构化；供下游 `Decoder` 使用。

约束：

- 单次调用、同步返回；不在内部启动额外 goroutine。
- 尊重 `ctx` 取消/超时；请求应及时中止并释放连接/流。
- 若实际请求命中模型上限（token/字节），快速返回错误（见 3.6.4），不做自动截断与降级。
- 不读取/依赖 `b.Records.Text` 与 `Target` 语义；最多用于记录 `FileID/BatchIndex` 等上下文信息以便观测，禁止据此做任何业务决策。
- `Raw.Text` 原样返回（no-trim/no-normalize/no-cleanup）；所有规范化/对齐/清洗由下游组件负责。

#### 3.6.3 行为规则（最小必需）

- 幂等边界：不保证 LLM 语义幂等，仅保证无隐藏重试与缓存；相同输入不因客户端层面副作用产生差异。
- 内存与局部性：可在实现内部使用流式传输以减小峰值占用，但对上仍聚合为单个 `Raw` 返回；不保留跨批/跨文件状态。
- 配对约定：`PromptBuilder` 与具体 `LLMClient` 通过“实现名 + Options”在装配层显式配对；若形状不匹配，客户端应返回输入无效错误。
- 限流协作：不在客户端内部做全局限流；如命中上游限流，返回“限流/节流”错误类别，可附带可选的重试提示信息。限流策略位于 3.7。

#### 3.6.4 错误映射（最小分类）

客户端需将上游与传输错误映射为最小集合，并保留底层 cause：

- 取消/超时：由 `ctx` 取消/超时导致（直接返回 `ctx.Err()` 或带因包装）。
- 限流/节流：如 HTTP 429 或提供方返回的配额限制，建议包含可选的重试提示（若可获取）。
- 协议/解码错误：响应缺失主体、解析失败或与预期字段不符（包括空结果且非业务允许的情形）。
- 输入无效：输入载荷非法或与实现约定不符（例如 `Prompt` 形状/编码失败）。
- 其他传输错误：如网络中断、TLS 失败、DNS 失败、5xx 等；无需再细分专用常量，但语义需明确。

说明：

- 错误仅做分类并返回；不重试/不上报；上层根据分类决定后续动作。
- 不吞错、不循环重试至不可预测；一次失败即返回。

#### 3.6.5 传输与协议（解耦）

- 传输自适配：允许 HTTP+JSON、gRPC、SSE/WebSocket 等实现形态；架构不限定协议，仅要求输出 `Raw.Text`。
- 请求组装：将 `Prompt` 原样或按实现约定转换为上游请求体；不擅自改写业务内容（如自动追加系统提示/后缀）。
- 协议解码：允许仅为提取文本字段进行协议层解码（如 JSON `choices[0].message.content`）；但禁止内容清洗/裁剪/截断与结果对齐（业务责任）。
- 资源释放：请求结束必须关闭响应体/流；遇取消应利用底层 `http.Request.WithContext(ctx)` 等机制及时中断。

#### 3.6.6 可选扩展：流式接口（非契约）

- 可选接口（供实现与上层自愿协商使用，非核心契约）：

```go
type LLMStreamer interface {
    InvokeStream(ctx context.Context, b Batch, p Prompt) (RawStream, error)
}
type RawStream interface {
    Next() (chunk string, done bool, err error) // 单向只读；实现需保证取消可及时生效
    Close() error                               // 释放底层连接/解码器
}
```

- 管理边界：当前流水线以批为单位处理与装配，对上仍以聚合后的 `Raw` 为准；是否消费流式接口由上层策略决定。架构不要求 Pipeline 感知流式。
  默认路径仍以一次性 `Raw` 为准；未消费流式接口不影响核心编排。

#### 3.6.7 安全与配置（数据载体优先）

- 认证：通过命名 provider 的 `Options` 传入令牌/密钥/端点（原样 JSON）；架构不规定键名与字段含义，字段示例如 `api_key_env`/`extra_headers`/`endpoint_path` 等由实现定义。
- 敏感信息：实现不得在日志中输出密钥/请求体；默认仅输出状态码与最小必要上下文。
- 超时：默认不强制；若实现提供可选请求级超时，应从自身 Options 读取并在 `Invoke` 内部派生 `WithTimeout`，仍以入参 `ctx` 为最高优先级（见 3.4）。

#### 3.6.8 集成关系与边界

- 与 3.3：若 `Batcher` 预算估算与实际不符，导致上游拒绝，客户端直接返回错误；不自动回退或二次拆分。
- 与 3.5：`Prompt` 的具体形状由模板/组装决定；客户端只做传输层序列化，不介入模板逻辑。
- 与 3.7：限流由外部闸门统一控制；客户端只在命中上游限流时返回“限流/节流”错误类别。
- 与 4.2：并发由调度器统一控制；客户端实现内部不得再启并发或维护共享工作池。

#### 3.6.9 不做的事（边界收紧）

- 不做模型选择/多路复用策略；
- 不做指数退避重试与缓存；
- 不做内容解析/JSON 结构化与结果对齐；
- 不在内部保存跨请求状态（会话记忆、RAG 缓存等）；
- 不隐式修改 Prompt（追加系统指令/截断/清洗）。

#### 3.6.10 Provider 入口（统一配置）

- 统一入口：LLM 的实现与参数仅通过“命名 provider”配置提供；配置结构：

```json
{
  "llm": "fast_openai",           // 当前使用的 provider 名称
  "provider": {
    "fast_openai": {
      "client": "openai",        // 实现名：openai|gemini（等）
      "options": {                 // 原样 JSON，字段由实现定义
        "model": "gpt-4o-mini",
        "base_url": "https://api.openai.com/v1"
      },
      "limits": { "rpm": 300, "tpm": 500000 } // 仅承载，执行由 3.7 闸门负责
    }
  }
}
```

- 单一配置源：不在 components/options 下暴露直连 LLM 的入口，避免多入口导致的分歧；所有 LLM 选项统一位于 `provider.<name>.options`。
- 可替换性：`client` 与实现名一一对应；新增实现只需在注册表中增加工厂映射。

### 3.7 API 限流

> 以“闸门”抽象在 LLM 客户端之前统一执行上游配额（RPM/TPM/MaxTokensPerReq）。最小必需：只对“是否可放行、何时放行”负责；不计算业务 token，不负责重试与退避，不感知 Prompt 细节。

#### 3.7.1 目标与范围（结果优先）

- 统一约束：按“提供者分组”共享限额（通常等价于同一 API key），一次调用视为 1 个请求；TPM 以“请求中预计消耗的 token（输入+预期输出）”计费。
- 最小职责：给定（分组 Key、请求数、tokens），判断是否可立即放行或需等待，或因单请求上限直接拒绝。
- 非目标：
  - 不计算 tokens（由编排层结合 Batcher/PromptBuilder 的估算提供）。
  - 不做指数退避/重试策略（交由 3.6、4.2 或上层策略）。
  - 不感知业务内容与模型差异（只消费数字化预算）。

#### 3.7.2 输入/输出（契约）

- 输入（一次“申请”）
  - key: string                // 限流分组（如 provider 名称/逻辑 key）
  - requests: int              // 默认为 1；并发合并时可>1
  - tokens: int                // 预计计费 token（输入+预期输出）
- 配置（每分组）
  - rpm: int                   // Requests Per Minute；0 表示无限制
  - tpm: int                   // Tokens Per Minute；0 表示无限制
  - max_tokens_per_req: int    // 单请求 token 上限；0 表示无限制
- 输出
  - 由接口行为体现：`Wait` 阻塞至额度可用或 `ctx` 取消；`Try` 不足立即返回 false；拒绝用于违反单请求上限 / 非法输入。

接口形状（Go 形状示意，仅表意）：

```go
type LimitKey string

type Limits struct { RPM, TPM, MaxTokensPerReq int }

type Ask struct {
    Key      LimitKey
    Requests int // default 1
    Tokens   int // >=0；由上层估算传入
}

// Gate: 限流闸门（并发安全）。
type Gate interface {
    // Wait: 阻塞直到额度可用或 ctx 取消；违反单请求上限时快速失败。
    Wait(ctx context.Context, a Ask) error
    // Try: 非阻塞尝试；不足时返回 false。
    Try(a Ask) bool
}

// 构造：一次性从配置装配，不做热更新。
func NewGate(m map[LimitKey]Limits, clk func() time.Time) Gate

// 可选诊断扩展：返回当前可用配额估值（仅诊断，不参与决策）。
type Snapshoter interface {
    Snapshot(key LimitKey) (rpmAvail, tpmAvail int)
}
```

契约要点：

- a.Tokens 必须已包含“固定提示开销 + 动态窗口内容 + 预期输出预算”的合计；闸门不做估算。
- 超过 `MaxTokensPerReq` 直接返回错误（快速失败），避免无效排队。
- 当 `RPM==0` 或 `TPM==0` 表示该维度不启用，仅以另一维度裁剪。
- `requests` 为纯计数；Gate 不感知合并/批处理的业务语义，只做计数扣减。

#### 3.7.3 数据结构与状态（扁平、最小）

- 每个 LimitKey 维护两个独立桶：reqBucket（RPM）与 tokBucket（TPM）。
- Token-Bucket 形态（简单易懂、常见）：
  - 容量 = 限额；补充速率 = 限额 / 60（每秒续杯）。
  - 状态：`level`（当前令牌数，范围 [0, capacity]），`lastRefill`（上次补充时间）。
  - 计算在申请时按需补充（懒补），不常驻 goroutine。
- 不引入额外“突发（burst）”参数；默认突发能力等于 capacity（即 1 分钟额定额度）。
- 并发安全与数据局部性：使用 `map[LimitKey]*entry` 保存状态，“一 Key 一锁”；每个 entry 含互斥锁与两个桶状态，避免全局锁与分片复杂度。

#### 3.7.4 行为与算法（统一判定）

- 申请 a：
  1) 基本校验：`requests>=1 && tokens>=0`；若 `MaxTokensPerReq>0 && tokens>MaxTokensPerReq` 则返回错误（资源/预算不足最小分类）。
  2) 对 reqBucket/tokBucket 分别按当前时间懒补充。
  3) 计算两桶能否同时满足：
     - 若均可覆盖（`reqLevel>=requests && tokLevel>=tokens`），立即扣减并放行。
     - 否则计算分别满足所需的等待时间，取两者中的最大值作为必要等待时间（夹紧确保两个维度同时满足）。
  4) Wait(ctx)：若需要等待，则按计算出的等待时长 sleep（向上取整到实现定义的最小粒度，例如 10ms，并分片睡眠以尊重 ctx），醒后再次循环评估与扣减；Try(a)：若不足立即返回 false。
- 近似与安全：
  - 时间计算采用单调时钟；扣减与补充在同一互斥临界区完成，避免 TOCTOU。
  - 补充分数残余应本地累积避免精度丢失；等待时间采用向上取整（ceil）以避免额度泄露。
  - 不保证严格公平（避免复杂排队）；如需 FIFO 公平性由 4.2 编排层在 Gate 外实现。

#### 3.7.5 配置与装配（与 5.1 映射）

- 配置来源：`config.provider[<name>].limits = {rpm, tpm, max_tokens_per_req}`。
- 分组键（LimitKey）的默认策略：使用 `config.llm` 指向的 provider 名称；若未来需要更细粒度（如按模型、按业务域），编排层可在 Ask.Key 中自定义，Gate 无需变更。
- 装配：应用启动时一次性构造 `Gate`，运行期只读；不做热更新与动态添加 Key。

#### 3.7.6 与上下游协作（单点控制）

- 与 Batcher/PromptBuilder（3.3/3.5）：
  - 上游负责计算单请求 tokens（含固定提示开销），并确保不超过 `max_tokens_per_req`；若超过需拆批或缩窗，闸门不介入。
- 与 LLMClient（3.6）：
  - Gate 位于 `LLMClient.Invoke` 之前；放行后才进行网络调用。
  - 上游若收到 `429`（限流/节流错误类别），可在编排层选择退避/重试；Gate 不自动学习或动态调整速率。
- 与并发/背压（4.2）：
  - 并发唯一入口在编排层；Gate 仅做配额判定，不新增 goroutine 或全局队列。
  - 诊断接口（如 Snapshot）与计数器仅用于观测，不参与放行决策，避免策略耦合。

#### 3.7.7 可观测性与诊断（最小集）

- 可选 Snapshot：若实现了 `Snapshoter` 扩展接口，可返回当前可用 req/tok 估值，便于在调试脚本中打印（仅诊断，不参与决策）。
- 计数器（可选）：成功放行次数、累计等待时长、拒绝次数；实现可留挂钩函数但非契约必需。

#### 3.7.8 错误与边界

- 输入无效/资源预算不满足：`requests<=0`、`tokens<0`、超过 `MaxTokensPerReq`。
- ctx 取消/超时：Wait 立即返回 ctx 错误，保持资源可及时释放。
- “不限额”语义：当某一维度为 0 时视为关闭该维度，Gate 对该维度不做扣减与等待。

#### 3.7.9 不做的事（边界收紧）

- 不计算 token（不读取 Prompt，不估算输出）。
- 不做重试/退避策略；不根据 429 自适应调整速率。
- 不做跨进程/分布式协同（单进程内存 Gate，足以满足当前需求）。
- 不做动态热更新与观测指标的持久化。

### 3.8 响应解析（含校验）

> 目标：把上游的 `Raw` 经 Decoder 转为统一的 `[]SpanResult`，并在 Decoder 内部完成“逐条/整段”的严格校验。架构只关心结果形状与不变式，不介入具体解析协议。

#### 3.8.1 目标与范围（最小必需）

- 统一语义：以 `Target` 为上下文与唯一产出区间，产出 `[]SpanResult`。
- 职责收敛：解析/解码由外部 `Decoder` 实现；校验以“库函数”的形式提供默认实现（逐条/整段），由 Decoder 自行调用或自定义等价校验函数。
- 快速失败：对齐失败/范围非法/覆盖不完整立即报错；不启用启发式修复或自动回退。

#### 3.8.2 数据契约（IR）

```go
// 位于 pkg/contract（形状参考）：
type SpanResult struct {
    FileID contract.FileID // 来源文件标识（与 Batch 一致）
    From   contract.Index  // 闭区间下界（全局 Index）
    To     contract.Index  // 闭区间上界（全局 Index）
    Output string          // 目标区间对应的输出文本
}

// 候选区间（解码中间态，供校验库函数使用；非流水线对外契约）。
type SpanCandidate struct {
    From   contract.Index
    To     contract.Index
    Output string
}

// 目标区间最小载体：等价于 Batch 的 TargetFrom/TargetTo 只读视图。
type Target struct {
    FileID contract.FileID
    From   contract.Index
    To     contract.Index
}

// 解码协议：由业务/编排层实现并注入具体策略（JSON、行对齐等）；返回最终结果。
type Decoder interface {
    // Decode: 将 Raw 解码并（可调用校验库函数）产出最终 []SpanResult；字段名/格式/回退策略由实现自决。
    Decode(ctx context.Context, tgt contract.Target, raw contract.Raw) ([]contract.SpanResult, error)
}
```

约束：

- 区间定位：`From/To` 必须落在 `Index ∈ [TargetFrom,TargetTo]`。
- 覆盖形态（二选一）：
  - 逐条对齐：若干个 `[i,i]`，覆盖 Target 内每一个 `Index`（一一对应）。
  - 整段输出：单个 `[TargetFrom,TargetTo]` 的区间（整体替换）。
- 扁平与顺序：同一 `FileID` 内同批 `Span` 不重叠、按 `From` 严格升序；跨批按 `BatchIndex` 提交顺序由编排保证。
- 纯数据：`SpanResult/SpanCandidate` 不承载方法；校验库为纯函数，不修改输入，且不依赖 `Batch.Records`。

#### 3.8.3 职责与默认校验库

- Decoder：负责把 `Raw` 解码为候选或直接构建 `[]SpanResult`；允许任意实现（严格 JSON、行对齐、结构化模式等），由编排层选择，架构不设默认、不做兜底回退。
- 校验库（默认实现，非插件）：
  - ValidatePerRecord(tgt, cands) → []SpanResult（要求 [i,i] 连续覆盖）
  - ValidateWhole(tgt, cands) → []SpanResult（要求单区间恰为 [TargetFrom,TargetTo]）
  - 纯函数、无 I/O；解码器可直接调用或替换为自定义等价校验。

#### 3.8.4 校验规则（统一消除“特殊分支”）

- 范围与覆盖：
  - 逐条对齐：必须覆盖 `Target` 内所有 `Index`，不允许缺失/额外索引。
  - 整段输出：`from/to` 必须严格等于 `TargetFrom/TargetTo`。
- 顺序与唯一性：
  - 逐条模式：`From` 严格升序，且各区间为 `[i,i]` 唯一覆盖。
  - 整段模式：单一区间；不得与逐条并存。
- 错误分类：
  - 越界/重叠/顺序错误/未完全覆盖 → 协议/解码错误（最小分类）。
  - `ctx` 取消 → 直接返回 `ctx.Err()`。

#### 3.8.5 算法与复杂度（线性）

- 校验：线性扫描检查单调性、范围与唯一性 → 构建 `[]SpanResult`（O(n) 时间/空间）。
- 整段：常量时间校验 `from/to` → 构建单元素切片（O(1)）。
- 内存：仅保留当前批的 `[]SpanResult`；不保留跨批状态；字符串做最小必要拷贝，结果 `Output` 为独立字符串。

#### 3.8.6 参考伪代码（解码 + 校验库）

```go
// 在 Decoder 实现内部：
cands := parseRawToCandidates(raw)
if mode == PerRecord {
  return ValidatePerRecord(tgt, cands)
}
return ValidateWhole(tgt, cands)
```

#### 3.8.7 配置（最小集）

- 校验库无配置项，仅依赖入参。
- 解码策略由编排层选择并注入具体 `Decoder` 实现；架构不提供默认模式，也不定义回退顺序。

#### 3.8.8 可观测性（可选）

- 计数：合法/非法响应次数、逐条/整段占比（由调用方统计）。
- 采样：保留少量失败样本（截断）用于诊断（由调用方实现，校验库不内置）。

#### 3.8.9 与上游/下游协作

- 上游（Prompt/LLMClient）：保证响应可被所选 `Decoder` 解码；架构不约束字段名/格式。
- 与 Assembler（3.9）：Assembler 按 `From` 升序线性拼接 `[]SpanResult`，仅处理同一 `FileID`；无需“最后写入获胜”。
- 与 Pipeline：`Decoder` 同步执行、无内部并发；仅处理当前批；错误快速上抛。

#### 3.8.10 不做的事（边界收紧）

- 不在校验库中做解析/回退/启发式修复。
- 不做自动回退到“部分覆盖/跳号补齐”。
- 不对输出做语言学修补或二次清洗。

#### 3.8.11 结构化格式注记（如 SRT）

- Decoder 可以读取 `Record.Meta` 中的结构化字段，将模型返回的文本与这些字段组装成最终块文本，写入 `SpanResult.Output`。
- 单条对齐：`[i,i]` → 产出包含 `Meta` 信息的完整字幕块；整段输出：`[L..R]` → 产出由多块拼接的完整文本。
- 禁止在 Decoder 外重推断/重编号；Assembler 不读取 Meta，只做线性拼接。

### 3.9 内容组装与顺序恢复

> 基于索引的线性装配；跨批顺序由编排器门闩保证；实现不做业务推断。

#### 3.9.1 目标与范围（最小必需）

- 目标：将 Decoder 产出的同一批、同一文件的 `[]SpanResult` 按 `From` 升序线性拼接，产出可被 `Writer` 直接消费的 `io.Reader`；跨批顺序由 Pipeline 以 `BatchIndex` 门闩保证。
- 范围：单文件、单批内装配；不引入跨文件/跨批的内部状态；不读取 `Meta`、不根据内容语义做任何“修补/选择/去重”。
- 非目标：
  - 不做“最后写入获胜”或复杂合并策略（上游已保证区间互斥与覆盖一致性）。
  - 不对非法区间做启发式修复（遇到违约立即报错）。
  - 不做格式化/语言学处理；不插入任何分隔符或行尾装饰（表现策略归 Writer 或由上游产出）。

#### 3.9.2 输入/输出与不变式

- 输入：`fileID FileID`、`spans []SpanResult`
  - 约束：`spans` 均属于同一 `fileID`；同批内 `From/To` 落在 `Target`；互不重叠；按 `From` 严格升序（由 Decoder/校验库保证）。
- 输出：`io.Reader`（可流式读取的拼接结果）。
- 违规处理：若发现不同 `FileID`、逆序或重叠，视为领域不变量违例；立即返回错误并不尝试修复。

#### 3.9.3 数据结构（扁平、连续、纯数据）

- 载体沿用 1.3 中已定义的 IR：`SpanResult{FileID, From, To, Output string}`。
- 装配输出接口：

```go
type Assembler interface {
    // 单批、单文件装配；不维护跨批状态。
    Assemble(ctx context.Context, fileID FileID, spans []SpanResult) (io.Reader, error)
}
```

实现可使用 `io.MultiReader`/小型 `bytes.Buffer` 封装多个只读 `string` 切片，避免二次拷贝；Assembler 不提供外部可见配置项。

#### 3.9.4 顺序恢复与提交门闩（与并发层配合）

- 批内顺序：装配严格按 `spans[i].From` 升序线性拼接，时间 O(n)、空间 O(1)（除去输出）。
- 跨批顺序：由 Pipeline 按 `BatchIndex` 单调提交同一 `FileID` 的批，调用 `Writer` 依序写出。Assembler 不持有 `prev/next` 任何跨批状态，不做重排缓冲。
- 幂等性：对同一批次、相同输入 `[]SpanResult` 多次 Assemble，输出字节序完全一致。

#### 3.9.5 伪代码（线性拼接）

```go
func (a *assembler) Assemble(ctx context.Context, fileID FileID, spans []SpanResult) (io.Reader, error) {
    if len(spans) == 0 { return strings.NewReader(""), nil }

    // 零拷贝倾向：构造 readers 切片，仅线性拼接输出内容
    rs := make([]io.Reader, 0, len(spans))
    for _, s := range spans {
        rs = append(rs, strings.NewReader(s.Output))
    }
    return io.MultiReader(rs...), nil
}
```

说明：

- 不插入任何分隔符或后缀；如需分隔/行尾规则，应由上游在 `Output` 中直接产出，或由 3.10 的 Writer 负责输出策略。
- 返回 `io.Reader` 以支持 `Writer` 流式落盘，避免构造大字符串。

#### 3.9.6 错误与边界（快速失败）

- 领域不变量违例：发现 `FileID` 混入、逆序或重叠；立即返回，交由上游诊断。
- 空输入：允许返回空 Reader（有些 Target 可能为空区间的上游约定，但架构不主动制造空 Span）。
- 不做：不尝试补齐缺口、不做内容合并/去重、不做“截断保护”。

#### 3.9.7 复杂度与局部性

- 时间：O(n)，单次线性遍历。
- 空间：O(1) 额外开销（除输出 readers 切片）；`spans` 为连续切片，缓存友好。
- 数据本地性：避免在热路径使用 `map`；仅顺序遍历与顺序写出。

#### 3.9.8 与其他阶段的契约对齐

- 与 3.3/3.7/3.8：`[]SpanResult` 的合法性与顺序由 Decoder+校验保证；Assembler 只做线性拼接。
- 与 4.2：跨批顺序恢复由编排层以 `BatchIndex` 门闩保证；Assembler 无跨批状态。
- 与 3.10：返回 `io.Reader` 即可交给 Writer 流式落盘；Writer 负责落盘与输出表现策略（如换行/分隔符/原子替换），Assembler 不介入。

#### 3.9.9 不做的事（边界收紧）

- 不读取 `Meta` 或任何业务字段；不根据内容文本做条件分支。
- 不实现缓存、去重、重试、回退、合并策略；不引入 goroutine。
- 不参与路径/文件命名与落盘细节（交由 3.10 Writer）。

本节仅定义装配阶段的结果契约与最小化行为；不规定任何业务实现细节。

### 3.10 输出与持久化

> 通用 Writer 契约（介质无关）；并提供文件系统 Writer 的最小实现参考。架构只求结果，不插手业务实现。

#### 3.10.1 目标与范围（最小必需）

- 目标：将 3.9 装配产出的 `io.Reader` 以流式方式持久化为目标介质上的最终工件。
- 范围：定义 Writer 的通用契约与结果（写入策略选择、取消响应、错误上抛）；不读取/不修改业务内容。
- 非目标：
  - 不做内容格式化/模板渲染/编码转换（按字节透传）。
  - 不做重试/回退/断点续写/校验和/去重/压缩/打包。
  - 不引入跨文件缓存或全局状态；跨文件并行由编排层统一控制。

#### 3.10.2 输入/输出与不变式

- 输入：`(id ArtifactID, r io.Reader)` 以及目标介质定位信息（由配置层提供）。
- 输出：将 `r` 的全部字节持久化为与 `id` 对应的工件；成功返回 `nil`，失败返回 `error`。
- 不变式：
  - 单写者：同一 `id` 任一时刻仅存在一个写入流（由编排层门闩保证）；Writer 不维护跨批状态。
  - 流式写出：使用缓冲顺序写入，禁止一次性将 `r` 读入内存。
  - 失败快速返回：任一 I/O 错误立即上抛；`ctx` 取消时立即返回 `ctx.Err()`。

#### 3.10.3 策略能力（无默认）

- 覆盖写：对目标工件进行覆盖式写入；中途失败可能产生部分内容，架构不做自动回滚。
- 原子替换：在与目标同一命名空间/介质内准备中间工件，完成写入并持久化后以原子操作替换目标；跨挂载/命名空间导致原子替换不可用时应直接报错。
- 选择由实现/配置决定；架构不规定字段名与默认值。

#### 3.10.4 并发与顺序（与 4.2 协调）

- 跨文件并行由编排层控制；Writer 不提供跨文件并行能力。
- 实现如采用内部并发以提升 I/O 吞吐，不得破坏单写者与流式写出不变式，且不得引入跨文件共享状态。
- 目录/命名等资源竞争由实现做最小化处理（如忽略“已存在”类非致命错误）。

#### 3.10.5 错误分类与处理（快速失败）

- 标识/路径类：映射或校验失败 → 路径/标识错误（最小分类）。
- I/O 类：打开/写入/同步/替换/关闭失败 → 直接上抛原始错误。
- 上下文：`ctx` 取消/超时 → 立即返回 `ctx.Err()`。
- 清理：原子模式下的中间工件可尽力清理；清理失败不二次包装为致命，记录后返回原始错误。

#### 3.10.6 复杂度与内存

- 时间：O(n)（n 为输出字节数），顺序写入。
- 空间：O(1) 额外开销（固定缓冲区），不拼接大字符串。
- 局部性：连续写入、单文件单写者，避免锁竞争与随机 I/O。

#### 3.10.7 与其他阶段的契约对齐

- 与 3.9：消费 `Assembler.Assemble` 返回的 `io.Reader`，不读取 `Meta`、不根据内容做任何决策。
- 与 4.2：跨批顺序与单写者保证由编排层提供；Writer 不维护跨批状态。
- 与 5.1：目标介质根/定位信息由 `Options.Writer` 提供；策略选项（覆盖/原子替换/权限策略等）由 Writer 实现自定义，架构不规定字段名与默认值。

#### 3.10.8 不做的事（边界收紧）

- 不做：多路径写入、镜像/备份、压缩/打包、校验和/签名、去重/增量写、断点续写、失败自动回滚策略。
- 不做：内容级尾随换行/分隔符注入（若需由上游产出或业务 Writer 实现）。
- 不做：对 `ArtifactID` 的重命名/扩展名推断（由业务层决定）。

---

#### 3.10.fs 文件系统 Writer（实现参考）

> 以下为落盘到本地/挂载文件系统时的最小实现约束与示例，属于“参考实现”，不构成通用契约。实现可等价替换，但必须满足 3.10 的通用不变式。

##### 3.10.fs.1 路径映射与目录结构（结果导向）

- 路径映射是纯函数：`dest = Join(outputDir, sanitize(id))`。
- 约束：
  - 对经 1.3 规范化后的 `id` 再做最小防御：`Clean` 路径、禁止逃逸 `outputDir`（拒绝绝对路径或以 `..` 起始的相对路径）。
  - 保留相对层级与文件名；扩展名由上游决定，Writer 不做推断。
  - 目录不存在时按需创建（等价于“创建父目录”）。
- 映射结果无效（空名、根目录冲突、越界）时立即返回路径错误，不做兜底。

示意（仅表意，非强制代码）：

```go
rel := filepath.Clean(string(id))
if filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") { return ErrPathInvalid }
dest := filepath.Join(outputDir, rel)
```

##### 3.10.fs.2 写入策略细节（覆盖/原子替换）

- 覆盖写：以可覆盖模式打开目标，使用缓冲顺序写入，完成后冲刷并关闭；中途失败可能留下部分内容，架构不做回滚。
- 原子替换：在目标同目录创建临时文件，完成写入并持久化后以重命名原子替换目标；要求同一挂载点；失败直接返回错误；临时文件清理由实现“尽力而为”。
- 权限与编码：
  - 权限由实现/平台默认或配置决定；架构不规定具体权限数值。
  - 编码按字节透传，不做换行规范化或 BOM 处理。

##### 3.10.fs.3 伪代码（示例，不构成契约）

```go
func (w *fsWriter) Write(ctx context.Context, id ArtifactID, r io.Reader) error {
    dest, err := mapPath(outputDir, id) // Clean + Join + 越界校验
    if err != nil { return err }
    if err := mkdirAll(filepath.Dir(dest)); err != nil { return err }

    // 覆盖写（示例，是否选择由实现/配置决定）
    f, err := openForOverwrite(dest) // 等价于创建+写入+截断，权限使用实现默认
    if err != nil { return err }
    defer f.Close()

    bw := bufio.NewWriter(f)
    if _, err := io.Copy(bw, readerWithCtx(ctx, r)); err != nil { return err }
    return bw.Flush()
}
```

注：`readerWithCtx` 表示在长拷贝过程中周期性检查 `ctx.Done()` 并尽快返回 `ctx.Err()`；该细节为示例，不构成契约要求。

##### 3.10.fs.4 平台相关注意事项（最小防御）

- 平台相关非法字符/命名限制由实现按最小防御处理，超出范围直接返回错误，不做智能纠正或降级。
- 原子替换依赖同目录同挂载点的 `rename` 语义；跨挂载点应直接报错而非回退到非原子替换（除非实现明确选择覆盖写策略）。

---

### 3.11 双语对照表（JSONL 边车）

> 在生成主结果工件的同时，以最小代价流式产出一份 JSONL 逐区间（通常逐条）对照表。不是新增阶段，而是复用 Decode→Assemble 之间已有数据。

#### 3.11.1 目标与范围（最小必需）

- 目标：对齐同一 `FileID` 下的源文本与目标文本，按 Span 区间一一输出到 JSONL 边车文件，便于下游检索与审校。
- 范围：
  - 不新增流水线阶段；在“提交门闩”按批就绪时顺手生成。
  - 仅使用现有 IR：`Batch.Records`（源）与 `[]SpanResult`（目标）。
  - 流式写出：单次 `Writer.Write(id+".jsonl", reader)`；不缓存整文件。
- 非目标：
  - 不做格式修补/智能回退；边车生成遇到错误按首错取消与主结果同命运。
  - 不引入内部并发或跨文件状态。

#### 3.11.2 记录结构（JSON Lines）

- 每行一个 JSON 对象（无外层数组），字段最小集合：

```json
{"file_id":"<string>","from":<int>,"to":<int>,"src":"<string>","dst":"<string>","meta":{...}}
```

- 字段说明：
  - `file_id`：与主工件一致（规范化后）。
  - `from`/`to`：闭区间，以全局 `Index` 表示（与 `SpanResult` 一致）。
  - `src`：源文本。由当前批 `Records[from..to]` 的 `Text` 顺序拼接得到（中间以 `\n` 连接，或按具体 Splitter 的原样文本）。
  - `dst`：目标文本。优先取 `SpanResult.Meta["dst_text"]`；缺省回退到 `SpanResult.Output`。
  - `meta`（可选）：透传 `SpanResult.Meta`（如 SRT 的 `seq`/`time`），用于下游定位；不存在可省略。

注意：JSONL 仅用于对照与审校；主装配/写出路径不读取 JSONL，保持解耦。

#### 3.11.3 生成位置与写出策略

- 位置：见 4.2 的“提交门闩”。当某批 `spans` 达到 `expect` 时：
  1) 先基于 `spans` 与同批 `Records` 线性生成若干 JSON 行并写入 `pwPairs`；
  2) 再调用 `Assembler.Assemble` 获取主结果 `io.Reader` 并通过 `pwMain` 管道写出。
- 写出：
  - 每文件建立两条独立 `io.Pipe`：
    - 主工件：`Writer.Write(file_id, prMain)`；
    - 边车：`Writer.Write(file_id+".jsonl", prPairs)`。
  - 两者并行进行，互不共享状态，均满足“单写者”不变式（各自针对不同目标）。
  - 文件结束时分别 `Close()` 相应管道并汇总错误。

#### 3.11.4 组装算法（O(n)，一次线性扫描）

- 预置：当前批 `spans` 已按 `From` 升序且不重叠；`Records` 按 `Index` 严格递增且为连续切片。
- 对每个 `span`：
  - 用两个移动指针在 `Records` 上定位 `[span.From..span.To]` 的起止位置；
  - `src = join(Records[i].Text)`（按顺序，最小实现以 `\n` 连接）；
  - `dst = span.Meta["dst_text"]` 或回退 `span.Output`；
  - `line = json.Marshal({file_id, from, to, src, dst, meta?})`；写入边车管道（追加 `\n`）。
- 跨批：按提交门闩自然递增，无需全局缓存；边车与主结果同时推进，边界由批窗口确定。

复杂度：

- 时间：O(n)（n 为本文件目标区间内 `Record`/`Span` 数量）。
- 空间：O(1) 额外开销（除 Pipe 缓冲）。
- 局部性：顺序访问 `Records` 与 `spans` 的连续切片，缓存友好。

#### 3.11.5 命名与路径

- 边车文件名：与主工件同名追加后缀 `.jsonl`，即 `ArtifactID + ".jsonl"`。
- 路径映射：沿用 3.10.fs 的 `mapPath` 规则；在 `flat=true` 时仅保留文件名；在层级模式下与主工件保持相对层级一致。

#### 3.11.6 错误与一致性

- 写出失败：与主写出同等对待；首错触发取消，两个写出均终止，返回首个错误。
- 生成失败：源/目标对齐不满足契约（通常由 Decoder/校验保证），到达此处应为“可装配”集合；否则快速失败并上抛。
- 一致性：同一文件的主工件与边车来自同一轮次数据；取消时两者均不保证完整，调用方应整体看待。

#### 3.11.7 不做的事（边界收紧）

- 不新增配置项；默认生成边车（是否忽略文件交由调用方）。
- 不做按内容的智能折行/清洗；保持最小必要的拼接与序列化。
- 不做多格式导出（仅 JSONL）。如需 CSV/TSV，属于业务 Writer 的范畴。

## 第四部分：并发与调度层

### 4.1 并发原语与同步机制

> 只用最小原语解决数据流并发：固定大小 worker 池 + 有界通道 + 单消费者聚合。避免在热路径上引入锁与复杂状态。

#### 4.1.1 目标与原则（最小必需）

- 单点并发：仅在流水线调度器统一创建/回收 goroutine，原子实现内部不启并发（见 1.4.4）。
- 有界背压：阶段之间用有界 `chan` 串联，默认容量 `2 × 并发度`，自然形成背压（见 1.4.4）。
- 简单直接：FIFO 优先，不做优先级/动态抢占；第一个错误触发全局取消，尽快收敛退出。
- 数据优先：以扁平结构承载任务与结果，避免指针追逐与多层间接；聚合单线程，省锁。

#### 4.1.2 采用原语

- goroutine：固定大小 worker 池，大小为 `concurrency`（构造参数注入，来源不在本层定义）。
- channel：
  - `inCh <- Task`：生产者→worker；容量默认 `2 × concurrency`。
  - `outCh <- Result`：worker→聚合；容量默认 `2 × concurrency`。
- context：自上而下传递取消与超时；收到取消后各方尽快返回 `ctx.Err()`。
- sync.WaitGroup：仅用于 worker 池收敛；聚合为单 goroutine，不需锁。

不采用：全局锁、复杂无锁队列、自旋等待、反射/泛型调度器、优先队列；它们都不符合“最小必需 + 数据局部性”的目标。

#### 4.1.3 任务与结果的最小数据形状

调度层不理解业务语义，只关心有序键与不透明载荷。

示意（Go 形状，仅表意，非强制定义）：

```go
type Key struct {
    FileID string
    Seq    int64 // 同一 File 内严格单调递增
}

type Task struct {
    Key     Key
    Payload any // 业务侧自定义的请求载荷（不透明）
    Budget  int // 可选预算提示（例如 token 估算）；0 表示未知
}

type Result struct {
    Key     Key
    Payload any // 业务侧自定义的结果载荷（不透明）
    Err     error
}

type Executor interface {
    Exec(ctx context.Context, t Task) (Result, error)
}

type Committer interface {
    Commit(ctx context.Context, r Result) error
}
```

约束：

- 并发层不读取、不修改 `Payload`；仅透传。
- 当 `Err != nil` 或 `Exec` 返回错误时，该结果不进入提交阶段，按首错取消处理。
- 预算 `Budget` 仅为提示，调度层不解释来源与含义。

#### 4.1.4 错误、取消与收敛

- 首错取消：任一 worker/生产者返回错误，聚合器记录首个错误并 `cancel()` 根上下文；关闭 `inCh`，等待 worker 退出；聚合排空 `outCh` 后返回。
- 无重试：架构不内建重试/退避；如需重试/回退，由业务侧 `Executor` 自行封装，调度层不插手。
- 无局部恢复：不做“跳过坏批继续”；首错即收敛，保证状态简单。

#### 4.1.5 内存与背压（再申明）

- 仅在通道与当前批上占用内存；队列容量受限于 `2 × 并发度`，与 1.4 的上界估算一致。
- Writer 变慢时，聚合无法及时消费 → `outCh` 填满 → worker 受阻 → `inCh` 背压到生产者 → 内存自然封顶。

#### 4.1.6 不做的事（边界收紧）

- 不做跨阶段共享缓存、跨批状态机。
- 不做优先级调度、公平队列、多级队列。
- 不做多错误聚合与按类恢复。
- 不做“局部乱序写回”策略；跨批顺序由 4.2 的提交门闩保证。

### 4.2 流水线编排与调度器（并发唯一入口）

> 规定阶段顺序、单点并发控制、背压与取消。聚焦“任务如何流动”，不插手任何业务实现。

#### 4.2.1 目标与范围

- 目标：以最小机制把“任务流”串起来，保证顺序与内存上界；并发层只管流动，不介入任务内容。
- 范围：
  - 生产者：串行产生 `Task{Key, Payload, Budget}` 并送入 `inCh`。
  - 并发执行：固定大小 worker 池仅调用业务注入的 `Executor.Exec`。
  - 聚合与提交：按 `Key{FileID, Seq}` 有序提交，调用 `Committer.Commit`。
- 非范围：不设计业务重试、内容修补、格式回退；不引入多入口并发；不在并发层估算/推断任何业务策略。

#### 4.2.2 调度器数据结构（最小集）

```go
type Scheduler struct {
    // 有界队列
    inCh  chan Task
    outCh chan Result

    // 提交门闩（聚合端，单 goroutine 访问）
    next map[string]int64 // 每个文件下一期望序号（Key.FileID -> next Seq）
    buf  map[Key]Result   // 暂存越序结果（容量受并发度上界）

    // 依赖（由上层注入，调度层仅调用）
    exec    Executor
    commit  Committer

    // 运行参数（只读）
    concurrency int // 并发度：构造参数注入
}
```

说明：

- `buf` 只缓存“已完成但未到提交序”的结果；最大挂起近似受限于并发度。
- `next` 初始为 0；新 `FileID` 首次出现时初始化。
- 生产者约束：对同一 `FileID` 必须按 `Seq` 顺序投递到 `inCh`；跨文件可交错。这一约束保证 `buf` 上界可由并发度与通道容量推导。
- 若追求更强数据局部性，可将 `buf` 改为每文件定长环形缓冲（容量≈并发度+1）。

#### 4.2.3 阶段与数据流

阶段顺序（单一数据流，调度层只管流转）：

1) Producer（串行）：从上游获取任务，按 `Key{FileID, Seq}` 递增将 `Task` 送入 `inCh`。
2) Workers（并发）：对每个 `Task` 调用 `Executor.Exec`，得到 `Result` 并送入 `outCh`；调度层不感知内部细节（如 Prompt/限流/调用/解码等）。
3) Aggregator（单线程）：按 4.2.4 的门闩顺序将成功 `Result` 交由 `Committer.Commit` 提交；错误触发首错取消。

背压链路：Writer 变慢 → Aggregator 堵塞 → `outCh` 满 → Workers 受阻 → `inCh` 满 → Producer 受阻。

#### 4.2.4 提交门闩（顺序与不变量）

- 目标：同一 `FileID` 内按 `Seq` 严格递增提交；跨文件相互独立、可并行。
- 规则：
  - 若 `r.Key.Seq == next[file]`，则提交并 `next[file]++`，随后检查 `buf` 的后继是否连续，尽可能地冲刷。
  - 若 `r.Key.Seq > next[file]`，则放入 `buf[r.Key]` 暂存，等待前驱到达。
  - 若 `< next[file]`，视为不变量违例（重复/越序回放），立即作为错误处理并触发首错取消。
- 内存上界：同一 `FileID` 下挂起数量 ≤ 并发度（最坏同文件并发完成但乱序）。

示意（仅表意）：

```go
var firstErr error
for r := range outCh {
    if r.Err != nil {
        if firstErr == nil { firstErr = r.Err; cancel() }
        continue
    }
    f := r.Key.FileID
    seq := r.Key.Seq
    if seq == next[f] {
        if err := commit.Commit(ctx, r); err != nil {
            if firstErr == nil { firstErr = err; cancel() }
            continue
        }
        for {
            next[f]++
            k := Key{FileID: f, Seq: next[f]}
            br, ok := buf[k]
            if !ok { break }
            if err := commit.Commit(ctx, br); err != nil {
                if firstErr == nil { firstErr = err; cancel() }
                break
            }
            delete(buf, k)
        }
    } else if seq > next[f] {
        buf[r.Key] = r
    } else { // seq < next[f]
        if firstErr == nil { firstErr = fmt.Errorf("duplicate or replay: %v", r.Key) }
        cancel()
    }
}
```

以上为数据流示意；`Committer` 为同步调用；错误按 4.1.4 首错取消规则处理。

#### 4.2.5 生产者与 worker 池（最小实现）

Producer（单 goroutine，严格串行）：

```go
for t := range tasks { // tasks 由上游业务按 Key 顺序提供
    select {
    case inCh <- t:
    case <-ctx.Done():
        return ctx.Err()
    }
}
close(inCh)
```

Workers（固定大小）：

```go
for t := range inCh {
    r, err := exec.Exec(ctx, t)
    if err != nil {
        out(Result{Key: t.Key, Err: err});
        continue
    }
    out(r)
}
```

以上仅为数据流示意；预算 `Budget` 如被使用，由业务侧在 `Executor` 内部与其策略/闸门交互；调度器与 worker 不做任何业务推断。

#### 4.2.6 错误处理与终止条件

- 首错优先：记录首个错误并 `cancel()`；继续排空 `outCh` 直至 worker 池收敛，随后返回该错误。
- 外部取消优先：若外部 `ctx.Done()` 先于首错发生，返回 `ctx.Err()`。
- 取消传递：生产/消费/worker 统一尊重 `ctx.Done()`；不做局部超时堆叠。
- 终止：`inCh` 关闭且所有 worker 退出，`outCh` 排空且聚合完成，返回 `nil` 或首错/取消错误。

#### 4.2.7 与业务执行器的协作

- 并发度：`concurrency` 为构造入参注入；来源（用户配置、闸门限额、TPS/RPM 估算等）不在本层定义。
- 限流/配额：如需限流/配额记账，由 `Executor` 内部完成；调度器不感知闸门存在，不做重试。
- 预算：`Task.Budget` 仅作为提示字段传入 `Executor`；调度层不读取、不校验其含义。

#### 4.2.8 可观测性（最小集）

- 计数：已发批数、成功提交批数、首个错误类型；采用聚合端计数。
- 时间：可选阶段耗时埋点（生产/调用/写出），仅用于诊断；不影响调度逻辑。

#### 4.2.9 不做的事（再次收紧）

- 不做跨阶段并发调优（如按阶段拆分独立池、动态伸缩）。
- 不做乱序写回与“最后写入胜出”。
- 不做任务抢占、饥饿避免与复杂公平性。
- 不做跨批/跨文件的粘连合并优化（如批合并/跨批复用）。
- 不做 tokens/费用估算与限流策略计算；这些逻辑属于业务层。

---

## 第五部分：支撑系统层

### 5.1 配置与依赖注入

> 以“最小必需”为原则，提供稳定的数据载体与显式装配机制：一次解析与校验，运行期只读；不做反射、不做动态加载、不做热更新；只传必要数据，不插手业务实现细节。

#### 5.1.1 目标与范围

- 目标：为 Reader/Splitter/Batcher（已实现 3.1/3.2/3.3）及后续组件提供统一、最小的配置与依赖注入骨架。
- 范围：定义配置源与优先级、Config 数据结构、组件注册表与工厂签名、一次性校验与装配流程。
- 非目标：不介入任何业务细节；不约束具体实现的内部参数含义；不引入运行期可变状态。

#### 5.1.2 配置源与优先级（一次性合并）

- 源：CLI 参数 > 环境变量 > 配置文件（JSON）。CLI 启动时会先尝试加载工作目录下的 `.env` 文件（不覆盖已存在的系统环境变量），因此“环境变量”包括 `.env` 中的内容。
- 合并：自上而下优先级覆盖；未知键一律报错；默认值仅在全部来源缺失时生效。
- 默认组件名（中性、安全）：`Reader=fs`、`Splitter=line`、`Batcher=sliding`、`Writer=stdout`（其余组件可留空，后续按需添加）。
- 默认并发度：`Concurrency=1`。
- ENV 键：统一前缀 `LLM_SPT_`，按 snake_case 映射字段名；越界或未知键报错（与 JSON/CLI 一致）。
- 时机：进程启动时解析一次并校验，通过后即不可变（只读）。

补充规则（ENV 合并的忽略策略）：

- 字符串键：空字符串视为未设置，不参与覆盖（例如 `LLM_SPT_LLM=""` 不会清空 JSON 中的值）。
- 数值键：仅在值为有效整数时才覆盖（空串或非法数字忽略）。特殊地，`LLM_SPT_MAX_RETRIES=0` 表示显式禁用重试。
- Provider 覆盖键（形如 `LLM_SPT_PROVIDER__<name>__*`）：只有发生有效变更时才落入覆盖；`OPTIONS_JSON` 为空时忽略，避免清空已存在的 `provider[<name>].options`。

多密钥与多 Provider 档位：

- 可在 `.env` 中定义任意数量的密钥变量名（例如 `OPENAI_API_KEY_PROD/BETA/...`），并在 `provider[<name>].options.api_key_env` 指向对应变量名；通过切换 `llm=<name>` 选择不同档位。
- 也可完全通过 ENV 配置 provider：使用 `LLM_SPT_PROVIDER__<name>__CLIENT` 与 `LLM_SPT_PROVIDER__<name>__OPTIONS_JSON`（包含 `api_key_env`）一次性注入。

#### 5.1.3 数据结构（扁平、可序列化、最小集）

示意（Go 形状，仅表意）：

```go
type Config struct {
    Inputs      []string
    Concurrency int

    // 批处理预算（配置层，唯一入口）
    MaxTokens   int

    // 组件选择（名称）—— 显式装配用（缺省采用内置默认名）
    Components struct {
        Reader        string
        Splitter      string
        Batcher       string
        Writer        string
        PromptBuilder string
        // 其余组件按需补充（Assembler/Writer）
    }

    // LLM 选择与命名 provider（统一入口）
    LLM      string // 当前使用的 provider 名称
    Provider map[string]struct {
        Client  string          // 实现名：openai|gemini
        Options json.RawMessage // 原样 JSON 透传给对应客户端
        Limits  struct {        // 仅承载，执行位于 3.7 闸门
            RPM             int
            TPM             int
            MaxTokensPerReq int
        }
    }

    // 各组件 Options 原样 JSON 子树；由具体工厂在内部自行反序列化
    Options struct {
        Reader        json.RawMessage
        Splitter      json.RawMessage
        Batcher       json.RawMessage
        Writer        json.RawMessage
        PromptBuilder json.RawMessage
        // 无 LLM 直连入口（统一通过 Provider.Options）
    }
}
// 运行时预算（调用层）：由编排层预扣固定开销后传入 Batcher.Make；
// 与配置层解耦，仅作为数据载体。
type BatchLimit struct { MaxTokens int }
```

约束：

- `Config` 为只读载体；运行期不得修改其字段。
- 组件具体 Options 的字段与含义由实现自定义，架构不表态；配置层仅保留顶层 `MaxTokens`；
- 运行时以 `BatchLimit{MaxTokens}` 承载“本次调用的有效预算”（已预扣固定开销）。
- 不在配置层出现任何函数值或复杂对象；Options 为纯数据，函数注入不在配置中体现。
- 顶层不出现任何具体 Writer/存储形态的字段（如路径/权限/覆盖策略等），一律下放到 `Options.Writer` 由实现自定义并校验。

#### 5.1.4 注册表与工厂（显式、零反射）

- 注册表：以字符串名称映射到工厂函数；注册表在组装层以字面 `map` 声明，禁止使用 `init()` 隐式注册。
- 工厂签名（示意）：

```go
type NewReader        func(raw json.RawMessage) (Reader, error)
type NewSplitter      func(raw json.RawMessage) (Splitter, error)
type NewBatcher       func(raw json.RawMessage) (Batcher, error)
type NewWriter        func(raw json.RawMessage) (Writer, error)
// 其余组件按需补充，对应自身原样 JSON Options

var registry = struct {
    Reader   map[string]NewReader
    Splitter map[string]NewSplitter
    Batcher  map[string]NewBatcher
    Writer   map[string]NewWriter
}{ /* 由 cmd/internal 以字面量填充 */ }
```

原则：不做反射、不做自动发现；名称不存在即报错，避免隐式耦合与不可预测行为。Options 的解析由各实现内部完成，装配层不理解业务细节。

补充：LLM 客户端同样通过注册表显式映射实现名（`client`）→ 工厂函数，禁止自动发现与隐式注册；新增实现仅需在注册表中增加映射。

#### 5.1.5 解析、校验与装配流程（一次性）

步骤：

- 解析：依次读取 CLI/ENV/JSON，按优先级合并为 `Config`；拒绝未知键。
- 校验（只校验“结果所必需”的最小边界）：
  - `len(Inputs) > 0`，`Concurrency >= 1`
  - `MaxTokens > 0`
  - `LLM` 非空，且在 `provider` 中存在同名项；`provider[LLM].client` 必须可在注册表中解析
  - 组件名称在注册表中存在（若为空则采用默认名）
  - 若存在 `provider[LLM].limits.max_tokens_per_req > 0`，则应满足 `MaxTokens <= max_tokens_per_req`（装配期可静态判定的前置条件）
- 注入：显式创建实例并组装流水线；Options 以原样 JSON 传入工厂，由实现私下解析；装配层不做二次方法注入。

严格解码要求：各组件工厂在解析自身 Options 时应启用“未知字段拒绝”（如 `json.Decoder.DisallowUnknownFields`）；避免因静默容错导致配置漂移。

#### 5.1.6 不做的事（边界收紧）

- 不支持运行期热更新与动态重载；配置只在启动时生效。
- 不提供动态插件加载或目录扫描；仅支持编译期注册的工厂。
- 不在配置层内置重试/退避/缓存；这些属于具体实现或上层策略。
- 不在配置中暴露模型/分词器细节为必填项；后续接入 LLMClient 时再最小化扩展。
- 不在装配层进行组件私有行为方法的二次注入；一次构造完成依赖装配。
- 不注入 logger/metrics/cache 等横切服务；观测与度量统一见 5.3，通过上层装配或 context 传递。

#### 5.1.7 与 3.x 的映射

- 3.1/3.2：`Inputs` 支撑 Reader；Splitter 的细节参数位于 `Options.Splitter` 的原样 JSON 中，架构不规定字段；保持只读与流式边界。
- 3.3：配置仅保留 `MaxTokens`；运行时以 `BatchLimit{MaxTokens}` 传递“有效预算”。批处理实现所需的 `ContextRadius/估算系数` 位于 `Options.Batcher`；不包含任何开销参数。
- 3.10：目标介质根/定位信息由 `Options.Writer` 提供；策略选项（覆盖/原子替换/权限策略等）由 Writer 实现自定义，架构不规定字段名与默认值。

#### 5.1.8 命名规范（重要）

命名规范（统一约定）

- 配置（JSON）：一律使用 snake_case。
  - 示例：
    - `max_tokens`
    - `options.reader`: `buf_size`, `exclude_dir_names`
    - `options.splitter`: `max_fragment_bytes`, `allow_exts`
    - `options.batcher`: `context_radius`, `bytes_per_token`
    - `options.writer`: `root_path`, `atomic_write`, `perm_mask`
    - `options.prompt_builder`: `inline_system_template`, `system_template_path`, `inline_glossary`, `glossary_path`
- CLI 标志：一律使用 kebab-case；仅提供极少全局覆盖（如 `--config`、`--llm`、`--concurrency`、`--max-tokens`）；不提供组件/Provider 的 Options 透传与任何实现专用旗标。
- 并发度字段命名统一为 `concurrency`（JSON/ENV/CLI 一致）。
- Go 结构体字段：使用导出驼峰命名 + `json` tag 映射到 snake_case（配置仅在加载/解码处生效；运行期只读）。

兼容性与错误处理

- 并发：`Concurrency` 仅由 Pipeline 使用；组件实现内部不得再启并发（与 1.4/3.3.8 保持一致）。
- 传参收敛：Pipeline 仅接收自身必要参数（如 `Params{Concurrency, Limit}`），不横传整份 Config。

### 5.2 错误处理与失败策略

> 结果导向与最小必需：仅定义错误的最小分类、传播与终止；不做业务回退/降级/重试。

#### 5.2.1 目标与范围（最小必需）

- 明确定义“最小错误分类 + 传播/终止原则”，使流水线在首错时可预测收敛并返回明确退出码。
- 架构只关心统一的成功/失败结果；不介入任何业务补救（不降级、不回滚、不自动修补）。

#### 5.2.2 错误分类（与退出码对齐的最小集合）

- 启动期（退出码=3）
  - 配置/装配错误：如未知键、类型不符、必填缺失、组件不可解析等。
- 运行期（退出码=1）
  - I/O/文件系统错误：路径不存在、权限拒绝、读写失败等。
  - 上游/网络错误：如连接失败、限流/节流等。
  - 协议/解码错误：响应结构或契约不满足。
  - 领域不变量违例：违反各组件契约的不变式（不在架构层枚举字段级细节）。
  - 资源/预算不足：如装载不可行或预算不足。
  - 超时/取消：统一返回 `ctx.Err()`。
- 参数错误（退出码=2）
  - CLI/输入参数非法。

说明：分类用于架构层的收敛与可观测性；“是否可恢复”等业务判断不在本层定义。

#### 5.2.3 传播与终止（首错取消）

- 首错取消：记录首个错误并取消上下文；排空队列完成收敛后返回首错。
- 外部取消优先：若外部 `ctx.Done()` 先到，返回 `ctx.Err()`。
- 同步返回：契约方法同步失败即返回；实现需在阻塞点检查 `ctx`。
- 不回滚：已提交结果不回滚；提交原子性等策略由上层选择。

#### 5.2.4 重试、退避与降级（架构不做）

- 架构不内置重试/退避/降级；相关策略由调用方或具体 Provider 内部实现，架构不感知闸门/配额。

#### 5.2.5 提交与部分成功

- 顺序与一致性：需满足组件契约的顺序与一致性约束；乱序/重复视为不变量违例并触发首错取消。
- 部分成功：首错前已提交的结果视为最终结果的一部分；架构不提供回滚通道。

#### 5.2.6 日志与指标（最小集合）

- 指标：成功提交批数、首错类别、处理耗时。
- 日志：结构化记录首错、关联标识与阶段名；使用哨兵错误与错误链匹配以支持 `errors.Is/As`。
- 隐私/载荷：由上层或运维策略决定，架构不强制。

### 5.3 日志与可观测性

> 结构化日志（文件落盘、按大小轮转）、最小必需指标（占位）与等级开关（仅 level）。

#### 5.3.1 目标与边界（最小必需）

- 目标：在不干预业务实现的前提下，提供“可关联、可定位、可度量”的最小观测能力；业务组件的错误与关键信息通过统一日志输出。
- 边界：
  - 不在 Context 注入 logger/metrics/tracer 对象；仅允许携带 `corr_id`（相关性 ID）。
  - 不记录业务载荷与密钥；仅输出定位所需最小上下文（file_id、batch_id、count、dur_ms）。
  - 采集点位于 Pipeline 边界（Reader/Splitter/Batcher/PromptBuilder/Gate/LLM/Decoder/Assembler/Writer），业务实现无需变更。

#### 5.3.2 输出通道、位置与格式

- 通道与位置：
  - 结果数据输出到 `stdout`。
  - 结构化日志默认写入文件 `logs/llmspt-current.txt`，按大小 10 MiB 轮转；轮转文件命名为 `llmspt-YYYYMMDD-HHMMSS.txt`，位于同目录下。
  - CLI 的人类可读提示仍输出到 `stderr`（极简）。
- 日志格式（契约）：结构化 JSON，每行一条（Line-delimited JSON）。
  - 时间戳：`ts` 使用 UTC ISO-8601（例：`2025-09-10T09:00:00Z`）。
  - 扁平结构，避免深嵌套，便于索引与查询。
  - 文本/人类可读格式不在架构契约中规定。

#### 5.3.3 日志字段（最小集合）

- 必填：
  - `level`: `debug|info|warn|error`（默认 `info`）。
  - `ts`: 事件时间（UTC）。
  - `corr_id`: 相关性 ID（应用边界自动生成；全局唯一、无业务语义的不透明字符串；生成算法不限）。
  - `comp`: 组件/层名（如 `pipeline|scheduler|gate|llm_client|decoder|io`）。
  - `stage`: 阶段名（统一三态：`start|finish|error`）。
  - `msg`: 简短信息（不含业务数据）。
- 选填（仅当存在时输出）：
  - `code`: 错误或分类码（与 5.2 错误分类对齐，如 `network|protocol|invariant|budget|cancel`）。
  - `dur_ms`: 本阶段耗时，毫秒（仅 `finish|error`）。
  - `count`: 数量类信息（如本批处理条数）。
  - `file_id|batch_id`: 与数据单元相关的最小标识（低基数、谨慎使用）。
  - `kv`: 默认禁用；启用时仅允许白名单键（由适配层配置），不参与索引，需截断与脱敏。

约束：

- 不记录密钥、令牌、请求/响应载荷；如必须诊断，仅输出长度、哈希或采样片段（参见 3.8.8）。
- 文本字段需有界截断（阈值由适配层配置）；默认实现为短消息，不记录大载荷。
- `corr_id` 由应用边界在每次运行生成（例如 UUIDv4），全局复用；不强制多级传播策略。

#### 5.3.4 最小指标（Metrics）

- 仅定义名字与标签；导出方式由适配器负责，架构不绑定实现。
- 计数器：
  - `op_total{comp,stage,result}`：操作次数；`result=success|error`。
  - `error_total{comp,code}`：错误次数（与 `code` 分类一致）。
- 直方图/摘要（二选一，建议直方图）：
  - `op_duration_ms{comp,stage}`：阶段耗时（毫秒）。

- 最小 SLI（不设具体阈值，阈值由运维定义）：
  - 错误率 = `error_total / op_total`
  - 延迟 P95 = `op_duration_ms` P95
  - 吞吐量 = `op_total` / 单位时间

标签约束：

- 禁止高基数标签（如 `file_id`、用户 ID）；`corr_id` 仅在日志中出现，不作为指标标签。
- 标签集合固定（`comp,stage,result,code`），不随业务扩展。

#### 5.3.5 采样与级别

- 级别：`debug|info|warn|error`（默认 `info`）。
- 不使用采样；仅基于等级过滤事件；`error` 级别事件永不丢弃。

#### 5.3.6 采集点（统一形态）

在 Pipeline 边界对所有业务组件采集：`reader|splitter|batcher|prompt_builder|gate|llm_client|decoder|assembler|writer` 的 `start|finish|error`，事件字段统一为 `comp + stage + code? + dur_ms? + count? + file_id? + batch_id?`，不记录业务载荷。

统一事件形态：所有采集点仅用 `comp + stage + code + dur_ms + count`，消除特殊分支。

#### 5.3.7 配置接口（最小集）

- 仅保留一个配置项：`logging.level = debug|info|warn|error`（默认 `info`）。
- 日志输出位置与轮转策略为固定默认：写入 `logs/llmspt-current.txt`，按 10 MiB 大小轮转为 `llmspt-YYYYMMDD-HHMMSS.txt`。

说明：后端、端口、导出协议、白名单键集、截断阈值等归适配/运维配置，不属于架构契约；业务/组件无需感知这些运维细节。

#### 5.3.8 适配与解耦

- 通过最小接口对接外部实现（示意）：
  - LoggerSink：`Emit(event)`；参数为扁平结构体/映射；由适配器完成编码与落地（是否支持文本、人类可读格式由适配器决定）。
  - MetricsSink：`Inc(name, labels)`、`Observe(name, labels, value)`；由适配器决定导出协议与端口（Prometheus/OpenTelemetry/本地日志/关闭等）。
- 组件内部仅组装“事件数据”，不持有任何外部 SDK 句柄；不在 Context 传递横切依赖。

#### 5.3.9 性能与失败策略

- 简化实现：默认使用同步文件写入（逐行 JSON），在事件量低/中场景下具备可接受的开销；生产可替换为异步落盘或外部收集。
- 优先级：`error` 级别事件永不丢弃；若实现具备背压/队列，应优先保证 `error` 事件成功落盘。
- 限额：对 `kv` 与消息长度做截断；避免格式化热路径上的反射与分配。
- 降级：观测后端不可用时，主流程不受影响；仅在 `stderr` 打印一次性告警。

#### 5.3.10 示例（JSON 行）

```text
{"level":"info","ts":"2025-09-10T09:00:00Z","corr_id":"8d7c...","comp":"pipeline","stage":"start","msg":"run"}
{"level":"error","ts":"2025-09-10T09:00:01Z","corr_id":"8d7c...","comp":"llm_client","stage":"error","code":"network","dur_ms":1234,"file_id":"sample.srt","batch_id":"3","msg":"invoke failed"}
{"level":"info","ts":"2025-09-10T09:00:02Z","corr_id":"8d7c...","comp":"pipeline","stage":"finish","dur_ms":2150,"count":42,"msg":"done"}
```

示例说明：上述仅用于字段契约示意，不限定实际实现的日志路由与后端。

以上设计保证：

- 数据优先：统一的扁平事件结构使查询与聚合显然；
- 简单高效：固定字段+少量可选，避免深层状态管理与分支；
- 结果导向：只度量“开始/结束/错误”和耗时计数，业务语义由上层解释。

---

### 5.4 终端信息提示（非日志）

> 面向人类的轻量状态输出，与结构化日志完全解耦；最小实现、低开销、零侵入业务数据路径。

#### 5.4.1 目标与边界

- 目标：在终端展示“当前任务相关信息”，便于实时观察处理进度与关键状态。
- 输出位置：`stderr`（不污染 `stdout` 的业务结果与管道）。
- 启用规则：检测为 TTY 时默认开启单行动态刷新；非 TTY 自动降级为关键节点分行打印。
- 非目标：不做复杂 TUI/进度条库、不做历史持久化、不预扫描文件总数。

#### 5.4.2 数据结构（最小、扁平）

- Run 级（一次运行唯一，仅最小状态）
  - `Concurrency int`
  - `LLM string`
  - `FilesDone int`（由 `FileFinish` 内部自增）
- File 级（文件生命周期内有效）
  - `FileID string`（显示短名，使用 `path.Base` 取末级，按可见宽度截断，尾部省略号）
  - `BatchesTotal int`
  - `BatchesDone int`
  - `Errors int`

说明：

- 不保留任何时间戳；不保留批内容/文本；不引入跨文件 map；更新为 O(1)。

#### 5.4.3 接口与输出规则（internal/diag/terminal）

- API（示意）：
  - `NewTerminal(w io.Writer, enabled bool) *Terminal`
  - `RunStart(concurrency int, llm string)`
  - `FileStart(fileID string, batchesTotal int)`
  - `FileProgress(done, total, errs int)`（内部≥100ms 节流刷新，合并多次更新）
  - `FileFinish(ok bool, dur time.Duration)`（立即刷新；内部 `FilesDone++`）
  - `RunFinish(ok bool, dur time.Duration)`（立即刷新，打印总览；ok/fail）
- TTY 模式：使用回车 `\r` 单行覆盖；需跟踪上一输出长度，短行用空格清尾；Finish 强制换行且不受节流限制；示例：
  `\r[file] docs/guide.md | 进度 7/12 | 错误 0 | 并发 4 | 用时 3.2s`
- 非 TTY：仅在关键节点打印一行（RunStart / FileStart / FileFinish / RunFinish），不打印周期性 Progress 行，不做覆盖。
- 兼容性：与日志完全分离；日志仍写入 `logs/llmspt-current.txt`。
- 环境变量：`CI=true` 时按非 TTY 策略处理；忽略 `NO_COLOR`（本设计无颜色）。
- 并发安全：采用“互斥 + 节流”的最小实现；写入失败或 `stderr` 不可用时，进入禁用态，后续调用为 no-op，不影响热路径。

#### 5.4.4 集成点（与 3.x 对齐）

- 组装层（`cmd/llmspt`）：
  - 按 CLI/ENV 启用：`--status`（默认 true）。
  - 运行前：`RunStart(concurrency, llm)`；结束：`RunFinish(ok, sinceStart)`。
- 流水线（`internal/pipeline`，不改变控制流，仅旁路通知）：
  - 切批成功后：`FileStart(fileID, len(batches))`。
  - 结果聚合门闩处：每就绪一个批（无论成功/失败）调用 `FileProgress(done, total, errs)`（内部节流合并）。
  - 单文件写出收尾：`FileFinish(ok, fileDuration)`。

边界情况：空文件/空批 `total=0`，仍执行 `FileStart → FileFinish`（进度 0/0）。

#### 5.4.5 性能与失败策略

- 时间：由节流控制，最多 10 次/秒；输出为常量时间字符串拼接。
- 空间：常量级；不缓存跨文件状态。
- 降级：若 `stderr` 非 TTY 或写失败，仅影响提示，不影响主流程；不重试、不阻塞热路径；写失败后终端提示进入禁用态。

#### 5.4.6 不做的事（约束）

- 不彩色/不依赖外部 TUI 库；不做复杂多行布局或对齐。
- 不展示 token/限流内部细节；不访问业务载荷；不读写日志文件。
- 不扫描目录统计总文件数；不进行权限/IO 探测以避免额外系统调用。

#### 5.4.7 示例

- TTY：
  - `[run] 并发=4 | llm=openai | 等待任务…`
  - `[file] docs/guide.md | 计划批次=12`
  - `\r[file] docs/guide.md | 进度 6/12 | 错误 0 | 并发 4 | 用时 3.1s`
  - `\n[done] docs/guide.md | 批次 12 | 总用时 5.1s`
  - `[ok] 全部完成 | 文件 8 | 总用时 41.3s`
- 非 TTY（无 Progress 行）：
  - `[run] 并发=4 | llm=openai`
  - `[file] docs/guide.md | 计划批次=12`
  - `[done] docs/guide.md | 批次 12 | 总用时 5.1s`
  - `[ok] 全部完成 | 文件 8 | 总用时 41.3s`

测试建议：

- 纯函数：文件名截断（`path.Base` + 可见宽度截断）、TTY/非 TTY 分支、覆盖清尾逻辑、节流行为（时间断言可放宽）。
- 集成：使用 Fake 组件与内存 writer 验证 `FileStart/Progress/Finish` 的调用次数与顺序；写失败后调用为 no-op。

---

## 第六部分：质量保障层

### 6.1 测试策略与契约测试

> 只规定输入/输出与错误分类的稳定契约，最小化用例与依赖；验证“结果是否满足契约”，不验证业务语义与实现细节。

#### 6.1.1 目标与边界（结果优先）

- 目标：以最小必需测试确保关键接口与跨层交互的“形状、时序与错误分类”稳定；以一条端到端基线验证装配可用。
- 边界：
  - 不测试业务正确率/质量评估（如文本好坏、召回/精准度等）；
  - 不限定具体实现策略（并发、缓存、重试等），仅对外部可观察行为与契约负责；
  - 不访问外部网络/真实第三方服务；一切外部依赖以可控的 Fake/Stub/Fixture 替代。

#### 6.1.2 分层策略（最小金字塔）

- 单元测试（多、快、纯）：
  - 目标：纯数据转换与判定逻辑（如分片、合批、对齐、解码），不做 I/O；
  - 约束：固定输入 → 确定输出；禁止并发与时钟依赖；避免深层嵌套与大量分支（数据结构优先）。
- 契约测试（少而关键）：
  - 目标：验证跨层/跨包的接口承诺，统一在 `pkg/contract` 提供测试套件与 Fake；
  - 核心：输入不变性、输出形状、顺序/幂等边界、取消/超时传递、错误最小分类、资源释放。
- 集成基线（更少，端到端）：
  - 目标：用极小 `testdata` 路径跑通一次流水线装配；
  - 约束：仅覆盖“能够连通并产出正确形状结果”；不拉长执行时间，不覆盖业务复杂场景。

#### 6.1.3 契约对象与必测行为

以下“对外契约对象”由 `pkg/contract/testkit` 提供统一断言与 Fake；internal 组件仅做单元/基线验证，不纳入契约测试：

- Reader：
  - 形状：按输入根产生有序 `Record`；`FileID`/`Index` 单调；
  - 行为：不可重排/去重，不缓存跨文件状态；出错即返回，不吞错。
- Splitter/Batcher：
  - 形状：`Batch.Records` 连续且与来源顺序一致；保留 `FileID`；
  - 行为：不修改输入内容；预算不足时快速失败（由上层处理）。
- PromptBuilder（纯计算）：
  - 形状：给定 `Batch` → 产出 `Prompt`（不透明载荷），失败即“输入无效”；
  - 行为：运行期无 I/O；若实现提供 token 预算估算，其结果不依赖具体 Prompt 实例（可选，不构成契约）。
- LLMClient.Invoke（传输适配）：
  - 形状：返回 `Raw.Text` 原样（无清洗/截断/修饰）；
  - 行为：一次请求一次返回；尊重 `ctx` 取消/超时；禁止隐式重试/并发；
  - 错误分类：取消/超时、限流/节流、协议/解码、输入无效、其他传输（见 3.6.4）。
- Decoder：
  - 形状：`Raw` → 结构化结果（如 `SpanResult[]`）；索引/范围自洽；
  - 行为：不依赖外部状态；无副作用。
- Writer：
  - 行为：幂等边界（同一输入多次写入的可见效果约定）；错误冒泡，不吞错；
  - 形状：可断言写入被调用的次数与顺序；具体路径等为 FakeWriter 内部约定，不纳入契约。

注：上述仅验证“接口行为与数据形状”，不验证内容语义与策略优劣。

补充：internal 组件（如 Gate/RateLimiter、Observability）不在对外契约之列，仅做单元/基线验证；若未来其接口在 `pkg/contract` 暴露，方可纳入契约测试范围。

#### 6.1.4 Testkit 与 Fake（可复用，零网络）

- 目录与组成：
  - `pkg/contract/testkit`：断言辅助、通用夹具（context、比较器、时序探针）；
  - `pkg/contract/fakes`：`FakeLLMClient`、`FakeGate`、`FakeWriter` 等；
  - `testdata/`：极小示例文件（KB 级），用于 Reader/流水线基线。
- `FakeLLMClient` 能力：
  - 配置为固定 `Raw`、按次序分发错误类别、支持 `ctx` 取消；
  - 可选注入延迟（毫秒级）便于覆盖取消时序（非契约要求，默认关闭）。
- `FakeGate` 能力：可编程放行/阻塞；支持计数与快照，便于断言限流路径。
- 约束：任何 Fake 不得使用网络与磁盘（除读取 `testdata`）；所有可观察点通过内存导出。

#### 6.1.5 用例组织与命名

- 命名：`Test<Component>_Contract/*`（契约）、`Test<Component>_Unit/*`（单元）、`Test_Pipeline_Baseline`（基线）。
- 布局建议：
  - 契约：`pkg/contract/<iface>_contract_test.go`
  - 单元：组件所在包的 `_test.go`
  - 基线：`internal/pipeline/pipeline_baseline_test.go` 使用 `testdata` 与 `fakes`
- 断言风格：只断言“形状、数量、顺序、分类、时序”，避免脆弱的全文 `golden`；若需 `golden`，仅用于稳定格式（如 JSON 行事件），并提供 `-update` 标志在本地更新。

#### 6.1.6 基线场景（端到端最小用例）

- 成功路径：
  - 输入：`testdata/mini/` 下 1 个小文件（两段文本）→ `Reader` → `Splitter/Batcher(C=2)`；
  - 基线采用固定 Prompt 样例与固定 Raw，仅用于连通性与结果形状验证；不构成接口契约或实现约束（示例可由 `Decoder` 产出 2 条 `SpanResult`）。
  - 断言：记录数/批数/顺序/`Raw` 原样/结果条数与覆盖范围；无日志阻塞；无残留 goroutine。
- 失败路径（各一条）：
  - 上游限流：`FakeGate` 拒绝 → 分类为“限流/节流”；
  - 取消：在 `Invoke` 前后注入取消 → 客户端及时返回 `context.Canceled`；
  - 输入无效：错误的 `Prompt` 形状 → `LLMClient` 分类为“输入无效”。

#### 6.1.7 CI 集成（快速、可重复）

- 命令：`go test ./... -short`；默认不跑任何需要真实网络/大数据的测试。
- 失败即止：契约与基线用例失败应直接阻断流水线；单元测试可并行。
- 产物（可选）：保存基线运行的结构化事件日志（若开启），便于回溯。
- 覆盖率：不设硬阈值；强调关键路径（契约与基线）全绿即可。

#### 6.1.8 反模式（禁止项）

- 用例耦合业务语义与大样本评测；
- 依赖真实上游服务或不稳定外部时钟/睡眠；
- 通过白盒断言内部私有函数/字段或并发实现细节；
- 过度 `golden` 导致频繁碎片化更新；
- 为了覆盖率添加无意义断言与无关场景。

#### 6.1.9 扩展与演进

- 新增能力时先更新 `pkg/contract` 接口与 Testkit，再放入实现；
- 第三方/自研实现必须通过契约套件后，方可被注册表选择；
- 若契约升级导致破坏性变化，需：
  1) 在 `CHANGELOG` 标注；
  2) 提供迁移说明与旧契约的兼容期测试入口；
  3) 基线用例同步更新。

---

## 第七部分：用户接口层

### 7.1 CLI 命令与参数约定

> 最小必需：一个可执行入口、单一主要子命令、位置参数承载输入根、极少全局旗标；不在 CLI 暴露任何组件/Provider 的 Options 透传。CLI 仅负责提供装配数据与触发执行，不介入业务实现细节；细粒度参数统一通过 JSON/ENV 配置承载。

#### 7.1.1 目标与范围（最小必需）

- 目标：以最小命令与少量旗标，组合 CLI/ENV/JSON 三源合并后的“有效配置”，构造并运行流水线。
- 范围：子命令布局、位置参数语义、旗标与 ENV 映射规则、优先级与合并、错误码与帮助输出。
- 非目标：不暴露任何实现内部参数的专用旗标；不提供 Options 透传旗标；不做动态插件管理；不提供交互式向导。

#### 7.1.2 子命令布局（Minimal）

- 可执行名：`llmspt`
- 子命令：
  - `run`（默认）：执行流水线。形如：`llmspt run [roots...] [flags]`
  - `version`：输出版本号与构建信息（纯信息性）。

说明：`llmspt [roots...] [flags]` 等价于 `llmspt run [roots...] [flags]`。

#### 7.1.3 位置参数与输入根（与 3.1 对齐）

- 位置参数 `roots...`：文件或目录路径，或单一 `-` 表示 STDIN。
- 规则：
  - 当 `roots` 为空或仅包含单一 `-` 时，从 STDIN 读入一次，产出 `(FileID="stdin", os.Stdin)`。
  - 禁止将 `-` 与其他根混用；一旦出现混用，立即报错退出（见错误码）。
  - 多个目录/文件根按给定顺序遍历（见 3.1）。

#### 7.1.4 全局旗标（仅承载结果所需）

- 通用运行参数（最小集）：
  - `--config <path>`：JSON 配置文件路径（可选）。
  - `--llm <name>`：LLM 提供方选择（可选，覆盖配置/ENV）。
  - `--concurrency <int>`：并发度，默认 `1`（可选）。
  - `--max-tokens <int>`：批处理/预算覆盖（可选，覆盖配置/ENV）。
  - `--status[=true|false]`：终端状态提示开关（默认 `true`；TTY 动态刷新，非 TTY 自动降级为分行）。
  - `--init-config [<dir>]`：在指定目录生成 `config.json` 与 `.env` 模板（若已存在则跳过，不覆盖）；不带值时默认当前目录。生成后直接退出，不进入运行路径。

约束：

- CLI 不提供任何组件或 Provider 的 Options 透传旗标；细粒度参数一律通过 JSON/ENV 配置。
- 旗标命名一律 kebab-case。
- 结果数据输出到 stdout；日志与帮助输出到 stderr。

#### 7.1.5 优先级与合并（与 5.1.2 一致）

- 优先级：`CLI flags > ENV(LLM_SPT_*) > JSON(--config)`。
- 合并：
  - 标量/字符串：后者覆盖前者。
  - 若配置出现 `Components.*` 名称：空则使用默认名；非空必须可解析。
  - `Options.*` 与 `provider[LLM].options/limits`：为完整替换；不做深度合并。
  - 未知键：在任一来源出现即报错（包括 ENV 未知键）。

#### 7.1.6 环境变量映射（前缀 LLM_SPT_）

- 规则：以 `LLM_SPT_` 为前缀；键名使用大写蛇形；与 JSON 键一一对应。CLI 会在启动早期自动加载工作目录 `.env`（不覆盖已存在 ENV）。
- 建议通道：
- `LLM_SPT_CONFIG_FILE=<path>` 或 `LLM_SPT_CONFIG_JSON=<json>`（二选一）
- `LLM_SPT_LLM=<name>`、`LLM_SPT_CONCURRENCY=<int>`、`LLM_SPT_MAX_TOKENS=<int>`

空值不覆盖：`.env` 或 ENV 中的空值不会覆盖配置（字符串空，或数值非法）；Provider 的 `OPTIONS_JSON` 空值也不会清空既有配置。

说明：不在文档中枚举 Provider/组件的私有键；遵循“一一映射、未知键失败、JSON 值必须合法”的统一规则。

#### 7.1.7 校验与错误码（快速失败）

- 最小校验：
  - 位置参数：`-` 不得与其他根混用；路径不可为空字符串。
  - `Concurrency >= 1`，有效配置必须导出 `MaxTokens > 0`（来源可为 JSON/ENV/CLI 覆盖）。
  - 有效配置必须能解析出 `LLM` 实现；对应客户端可解析。
  - 若存在 `provider[LLM].limits.max_tokens_per_req > 0`，需满足 `MaxTokens <= max_tokens_per_req`。
  - 若配置包含组件名称：名称必须可解析；组件配置的未知字段需在构造期被拒绝（未知键失败）。

- 退出码：
  - `0`：成功。
  - `1`：运行期失败（I/O/网络/上游错误）。
  - `2`：参数错误（命令语法/旗标冲突/STDIN 混用）。
  - `3`：配置/装配错误（未知键/校验不通过/组件不可解析）。

#### 7.1.8 使用示例（最小路径）

- 从 STDIN 读取，最小覆盖：
  - `cat input.txt | llmspt run --config ./llmspt.json`

- 指定目录输入，覆盖少量字段：
  - `llmspt run ./docs --config ./llmspt.json --llm fast_openai --concurrency 2`

- 无配置文件（直接给出必要覆盖）：
  - `llmspt run ./docs --llm fast_openai --max-tokens 800`

#### 7.1.10 初始化配置（模板生成）

- 生成模板到当前目录：
  - `llmspt --init-config`
  - 生成 `./config.json` 与 `./.env`（若文件已存在则跳过，不覆盖）。
- 生成模板到指定目录：
  - `llmspt --init-config out`
  - 自动创建目录并生成 `out/config.json` 与 `out/.env`（若已存在则跳过，不覆盖）。
- `.env` 规则：仅作为环境变量来源之一，启动时自动加载，不覆盖已有系统环境变量；空值不会覆盖配置。

#### 7.1.9 不做的事（边界收紧）

- 不提供实现内部参数或 Options 的任何 CLI 旗标（不含组件/Provider）。细粒度参数仅通过 JSON/ENV 提供。
- 不支持动态插件加载/目录扫描；仅限编译期注册的实现。
- 不提供交互式对话/向导；帮助仅为静态 usage 文本与 `--help` 输出。
- 不支持 YAML/TOML；仅支持 JSON 配置文件。
