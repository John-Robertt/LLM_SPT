# LLM-SPT：大语言模型字幕翻译工具

> 一键批量翻译SRT字幕文件，支持ChatGPT/Gemini主流AI模型

## 💡 核心功能

**一句话说明**：把SRT字幕文件扔给LLM-SPT，它会调用AI模型输出翻译好的字幕文件。

**解决的问题**：

- ✅ **批量处理**：一次翻译几十个字幕文件
- ✅ **智能分片**：自动处理长字幕，保持上下文连贯  
- ✅ **多模型支持**：OpenAI、Gemini
- ✅ **并发控制**：自动限流，避免API限制

## ⚡ 2分钟快速开始

### 步骤1：安装

```bash
git clone https://github.com/John-Robertt/LLM_SPT.git
cd LLM_SPT
go build -o llmspt cmd/llmspt/main.go
```

### 步骤2：配置

```bash
# 生成配置模板（同时生成 .env 模板，已存在则跳过）
# 不带值默认在当前目录生成 config.json 与 .env
./llmspt --init-config

# 编辑config.json，或在 .env 中填写 API Key/覆盖项
```

**最简配置示例**：

```json
{
  "llm": "gemini",
  "provider": {
    "gemini": {
      "options": {
        "api_key": "你的-API-密钥"
      }
    }
  }
}
```

#### 可选：使用 .env 提供环境变量

CLI 启动时会自动读取工作目录下的 `.env` 文件（不覆盖已存在的系统环境变量）。你可以把 API Key 和覆盖项放在 `.env` 中：

```dotenv
# 供应商 API Key（示例二选一）
GOOGLE_API_KEY="your-gemini-key"
# OPENAI_API_KEY="sk-your-openai-key"

# 可选：覆盖运行参数（与环境变量一致，前缀 LLM_SPT_）
LLM_SPT_LLM=gemini
LLM_SPT_CONCURRENCY=3
LLM_SPT_MAX_TOKENS=4096
# 也可指定配置文件路径
LLM_SPT_CONFIG_FILE=./config.json
```

优先级：`CLI 参数 > 环境变量（含 .env） > JSON 配置`。
规则补充：`.env` 空值不会覆盖配置（字符串空串和无效数字都被忽略；仅有效值才生效）。

提示：使用 `--init-config <dir>` 可将配置写入指定目录；会生成 `<dir>/config.json` 与 `<dir>/.env`（若已存在则跳过）。仅写 `--init-config` 时默认使用当前目录。

#### 多密钥/多 Provider 档位（OpenAI 示例）

你可以在 `.env` 中定义任意多个 API Key 变量名，然后在 `config.json` 的不同 provider 条目里通过 `options.api_key_env` 指向不同的变量，从而按需切换：

```dotenv
# .env：自定义多个变量名
OPENAI_API_KEY_PROD=sk-xxxx
OPENAI_API_KEY_BETA=sk-yyyy
OPENAI_API_KEY_BACKUP=sk-zzzz
```

```json
// config.json：为 OpenAI 定义多档位 profile
{
  "llm": "openai_prod",
  "provider": {
    "openai_prod":  {"client": "openai", "options": {"model": "gpt-4o-mini", "api_key_env": "OPENAI_API_KEY_PROD"}},
    "openai_beta":  {"client": "openai", "options": {"model": "gpt-4o-mini", "api_key_env": "OPENAI_API_KEY_BETA"}},
    "openai_backup":{"client": "openai", "options": {"model": "gpt-4o-mini", "api_key_env": "OPENAI_API_KEY_BACKUP"}}
  }
}
```

切换使用：

- 保持配置不变，通过 CLI 覆盖：`./llmspt --llm openai_beta ...`
- 或通过 ENV 覆盖：`LLM_SPT_LLM=openai_backup ./llmspt ...`

说明：

- `.env` 中不限于 `OPENAI_API_KEY/GOOGLE_API_KEY` 两个变量名；可以新增任意命名，用 `api_key_env` 指向它即可。
- 如果想完全用 ENV 驱动 provider，也可以用 `LLM_SPT_PROVIDER__openai__OPTIONS_JSON` 一次性注入完整 options JSON，例如：

```dotenv
LLM_SPT_LLM=openai_prod
LLM_SPT_PROVIDER__openai__CLIENT=openai
LLM_SPT_PROVIDER__openai__OPTIONS_JSON='{"model":"gpt-4o-mini","api_key_env":"OPENAI_API_KEY_PROD"}'
```

### 步骤3：翻译

```bash
# 翻译单个文件  
./llmspt input.srt

# 批量翻译
./llmspt *.srt
```

**效果展示**：

```srt
输入 input.srt:
1
00:00:01,000 --> 00:00:03,000
Hello, world!

输出 out/input.srt:
1  
00:00:01,000 --> 00:00:03,000
你好，世界！
```

## 🎯 模型选择指南

| 提供商 | 推荐模型 | 特点 | 适用场景 |
|--------|----------|------|----------|
| **Gemini** | `gemini-2.5-flash` | 免费额度大，速度快 | 测试和小批量 |
| **OpenAI** | `gpt-4o` | 质量高，理解能力强 | 生产和高质量要求 |

**模型配置示例**：

```json
// Gemini (推荐新手)
{
  "llm": "gemini",
  "provider": {
    "gemini": {
      "options": {
        "model": "gemini-2.5-flash",
        "api_key": "your-key"
      }
    }
  }
}

// OpenAI (高质量)
{
  "llm": "openai", 
  "provider": {
    "openai": {
      "options": {
        "model": "gpt-4-turbo",
        "api_key": "sk-your-key"
      }
    }
  }
}
```

## ⚙️ 性能调优

### 并发度设置

根据文件大小选择合适的并发度：

```bash
# 小文件 (<100行)：稳定优先
./llmspt --concurrency 1 small.srt

# 中等文件 (100-1000行)：平衡模式  
./llmspt --concurrency 3 medium.srt

# 大文件 (>1000行)：速度优先
./llmspt --concurrency 5 large.srt
```

### 性能对比

```text
测试：2283行字幕文件
并发度 1：  ~3分钟   （稳定，适合小文件）
并发度 3：  ~1分钟   （推荐，平衡选择）
并发度 5：  ~40秒    （激进，适合批量）
```

### 完整配置示例

**高性能配置**（适合批量处理）：

```json
{
  "concurrency": 5,
  "max_tokens": 4096,
  "llm": "openai",
  "provider": {
    "openai": {
      "options": {
        "model": "gpt-3.5-turbo",
        "api_key": "your-key"
      },
      "limits": {
        "rpm": 60,
        "tpm": 40000
      }
    }
  }
}
```

**高质量配置**（适合重要内容）：

```json
{
  "concurrency": 1,
  "max_tokens": 4096, 
  "llm": "openai",
  "provider": {
    "openai": {
      "options": {
        "model": "gpt-4o",
        "api_key": "your-key"
      }
    }
  },
  "options": {
    "batcher": {
      "context_radius": 3
    }
  }
}
```

## 🛠️ 故障排查

### 常见问题速查

#### API Key错误

```
错误：authentication failed
解决：检查config.json中api_key是否正确
```

#### 超出限制

```
错误：rate limit exceeded
解决：降低concurrency或调整limits.rpm/tpm
```

#### 翻译不完整

```
原因：Token预算不足
解决：增加max_tokens或减小批次大小
```

#### 速度太慢

```
原因：并发度过低
解决：增加concurrency到3-5
```

### 日志诊断

启用详细日志：

```json
{
  "logging": {"level": "debug"}
}
```

关键指标：

- `batcher.make`: 分批耗时
- `llm_client.invoke`: API调用延迟  
- `decoder.decode`: 解析耗时

## 🔧 工作原理

LLM-SPT使用流水线架构处理字幕翻译：

```text
SRT文件 → 解析记录 → 智能分批 → AI翻译 → 结果解析 → 重组输出
  ↓         ↓         ↓        ↓        ↓         ↓
Reader → Splitter → Batcher → LLM → Decoder → Writer
```

**核心特性**：

- **智能分片**：保留时间轴连续性，控制token预算
- **并发处理**：多批次同时翻译，智能排队
- **顺序保证**：最终输出按原始顺序组装

## 🚀 高级用法

### 命令行选项

```bash
# 基本选项
./llmspt --concurrency 3 --max-tokens 4096 *.srt

# 使用配置文件
./llmspt --config my-config.json *.srt

# 从标准输入
cat input.srt | ./llmspt -
```

### 批量处理目录

```bash
# 处理多个目录
./llmspt folder1/*.srt folder2/*.srt

# 递归处理（配置文件中设置）
{
  "inputs": ["movies/**/*.srt", "shows/**/*.srt"]
}
```

## 📝 环境要求

- Go 1.22+
- 支持的操作系统：Linux, macOS, Windows
- API密钥：OpenAI/Gemini

### 构建

```bash
# 本地构建
go build -o llmspt cmd/llmspt/main.go

# 多平台构建  
GOOS=linux GOARCH=amd64 go build -o llmspt-linux cmd/llmspt/main.go
GOOS=windows GOARCH=amd64 go build -o llmspt.exe cmd/llmspt/main.go
```

### 测试

```bash
# 运行测试
go test ./...

# 性能测试
go test -bench=. ./...
```

## ⚠️ 重要提醒

- **费用控制**：合理设置`limits.tpm`避免意外高额费用
- **备份文件**：处理前备份重要字幕文件
- **API配额**：注意各平台的免费/付费限制

---

**LLM-SPT** - 让字幕翻译变得简单高效 ⚡
