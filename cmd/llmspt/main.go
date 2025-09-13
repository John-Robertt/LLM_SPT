package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	cfgpkg "llmspt/internal/config"
	"llmspt/internal/diag"
	"llmspt/internal/pipeline"
)

var pipelineRun = pipeline.Run

// 简化的 CLI：默认子命令 run。
// 位置参数为 roots（文件/目录 或 "-" 表示 STDIN，不能与其他根混用）。
// 全局旗标（最小集）：--config, --llm, --concurrency, --max-tokens
func main() {
	os.Exit(run())
}

func run() int {
	start := time.Now()
	corrID := genCorrID()
	// 在任何 ENV 读取前，尝试加载工作目录下的 .env（不覆盖已有 ENV）。
	_ = loadDotEnv(".env")
	// 从配置读取日志级别，仅保留 level 选项；默认 info
	logLevel := "info"
	// 先占位默认，稍后在解析/合并配置后重建 logger 以使用最终 level
	logger := diag.NewLogger(corrID, logLevel)
	// flags
	var (
		flagConfig      string
		flagLLM         string
		flagConcurrency int
		flagMaxTokens   int
		flagMaxRetries  int
		flagInitDir     string
		flagStatus      bool
	)
	flag.StringVar(&flagConfig, "config", "", "配置文件路径（JSON）；缺省读取 ./config.json（若存在）")
	flag.StringVar(&flagLLM, "llm", "", "provider 名称（覆盖配置）")
	flag.IntVar(&flagConcurrency, "concurrency", 0, "并发度（覆盖配置）")
	flag.IntVar(&flagMaxTokens, "max-tokens", 0, "最大 token 预算（覆盖配置）")
	// max-retries 允许显式设置为 0；默认 -1 表示“未覆盖”。
	flag.IntVar(&flagMaxRetries, "max-retries", -1, "LLM 阶段最大重试次数（覆盖配置；0 表示不重试）")
	flag.StringVar(&flagInitDir, "init-config", "", "在指定目录生成默认配置 config.json 和 .env 模板（若已存在则跳过，不覆盖）；不带值时默认当前目录")
	flag.BoolVar(&flagStatus, "status", true, "终端状态提示（stderr）。TTY 动态刷新；非 TTY 打点输出")
	normalizeInitArg()
	flag.Parse()

	// roots（位置参数）
	roots := flag.Args()

	// --init-config: 生成模板并退出
	var initDir string
	if strings.TrimSpace(flagInitDir) != "" {
		initDir = strings.TrimSpace(flagInitDir)
	}
	if initDir != "" {
		// 创建目录（若不存在）
		if err := os.MkdirAll(initDir, 0o755); err != nil {
			fprintf(os.Stderr, "生成默认配置失败: %v\n", err)
			logger.Error("pipeline", string(diag.Classify(err)), "first error", &start)
			return 3
		}
		cfg := cfgpkg.DefaultTemplateConfig()
		cfgPath := filepath.Join(initDir, "config.json")
		if err := writeConfig(cfgPath, cfg); err != nil {
			fprintf(os.Stderr, "生成默认配置失败: %v\n", err)
			logger.Error("pipeline", string(diag.Classify(err)), "first error", &start)
			return 3
		}
		// 生成 .env 模板（不覆盖已存在文件）。
		envPath := filepath.Join(initDir, ".env")
		if err := writeDotEnv(envPath); err != nil {
			fprintf(os.Stderr, "提示：.env 生成失败（已跳过）：%v\n", err)
		}
		return 0
	}

	// JSON 配置（文件或 ENV: LLM_SPT_CONFIG_JSON）
	var cfgJSON []byte
	if s := os.Getenv("LLM_SPT_CONFIG_JSON"); s != "" {
		cfgJSON = []byte(s)
	}

	if flagConfig == "" {
		if s := os.Getenv("LLM_SPT_CONFIG_FILE"); s != "" {
			flagConfig = s
		}
	}
	// 默认读取工作目录下 config.json（若存在）
	if flagConfig == "" {
		if _, err := os.Stat("config.json"); err == nil {
			flagConfig = "config.json"
		}
	}

	cfg := cfgpkg.Defaults()
	if flagConfig != "" || len(cfgJSON) > 0 {
		base, err := cfgpkg.LoadJSON(flagConfig, cfgJSON)
		if err != nil {
			fprintf(os.Stderr, "配置解析失败: %v\n", err)
			logger.Error("pipeline", string(diag.Classify(err)), "first error", &start)
			return 3
		}
		cfg = cfgpkg.Merge(cfg, base)
	}

	// ENV 覆盖（最小集合）
	overEnv, err := cfgpkg.EnvOverlay(os.Environ())
	if err != nil {
		fprintf(os.Stderr, "环境变量解析失败: %v\n", err)
		logger.Error("pipeline", string(diag.Classify(err)), "first error", &start)
		return 3
	}
	cfg = cfgpkg.Merge(cfg, overEnv)

	// CLI 覆盖
	var overCLI cfgpkg.Config
	// 标记 MaxRetries 未设置（避免默认 0 被误判为要覆盖）
	overCLI.MaxRetries = -1
	if flagLLM != "" {
		overCLI.LLM = flagLLM
	}
	if flagConcurrency > 0 {
		overCLI.Concurrency = flagConcurrency
	}
	if flagMaxTokens > 0 {
		overCLI.MaxTokens = flagMaxTokens
	}
	if flagMaxRetries >= 0 {
		overCLI.MaxRetries = flagMaxRetries
	}
	if len(roots) > 0 {
		overCLI.Inputs = roots
	}
	cfg = cfgpkg.Merge(cfg, overCLI)

	// 基本校验 & 装配
	if err := cfgpkg.Validate(cfg); err != nil {
		fprintf(os.Stderr, "配置校验失败: %v\n", err)
		// 提示打印有效配置，便于诊断
		_ = dumpConfig(cfg)
		logger.Error("pipeline", string(diag.Classify(err)), "first error", &start)
		return 3
	}

	// 使用最终配置中的日志级别重建 logger
	if strings.TrimSpace(cfg.Logging.Level) != "" {
		logLevel = strings.TrimSpace(cfg.Logging.Level)
	}
	logger = diag.NewLogger(corrID, logLevel)

	// 预检：若使用文件系统 Writer，检查输出目录的可写性
	if err := preflightCheckOutputDir(cfg); err != nil {
		fprintf(os.Stderr, "输出目录不可写或无法创建: %v\n", err)
		logger.Error("pipeline", string(diag.Classify(err)), "first error", &start)
		return 3
	}

	comp, set, _, _, err := cfgpkg.Assemble(cfg)
	if err != nil {
		fprintf(os.Stderr, "装配失败: %v\n", err)
		logger.Error("pipeline", string(diag.Classify(err)), "first error", &start)
		return 3
	}

	// 终端信息提示（非日志）：按 CLI 启用，默认开启
	term := diag.NewTerminal(os.Stderr, flagStatus)
	diag.SetTerminal(term)
	defer diag.SetTerminal(nil)
	if term != nil {
		term.RunStart(cfg.Concurrency, cfg.LLM)
	}

	// debug: 输出运行时配置信息（已脱敏）
	if logger != nil {
		kv := map[string]string{
			"inputs_count":   fmt.Sprintf("%d", len(cfg.Inputs)),
			"concurrency":    fmt.Sprintf("%d", cfg.Concurrency),
			"max_tokens":     fmt.Sprintf("%d", cfg.MaxTokens),
			"llm":            cfg.LLM,
			"reader":         cfg.Components.Reader,
			"splitter":       cfg.Components.Splitter,
			"batcher":        cfg.Components.Batcher,
			"prompt_builder": cfg.Components.PromptBuilder,
			"decoder":        cfg.Components.Decoder,
			"assembler":      cfg.Components.Assembler,
			"writer":         cfg.Components.Writer,
		}
		// 提取 Provider 关键信息（不含密钥）
		if p, ok := cfg.Provider[cfg.LLM]; ok {
			kv["provider_client"] = p.Client
			// 解析常见无敏感项
			type small struct {
				BaseURL  string `json:"base_url"`
				Model    string `json:"model"`
				Endpoint string `json:"endpoint_path"`
			}
			var s small
			_ = json.Unmarshal(p.Options, &s)
			if s.BaseURL != "" {
				kv["base_url"] = s.BaseURL
			}
			if s.Model != "" {
				kv["model"] = s.Model
			}
			if s.Endpoint != "" {
				kv["endpoint_path"] = s.Endpoint
			}
		}
		logger.DebugStart("config", "effective", "", "", kv)
	}

	// STDIN 混用规则已在 Validate 中统一校验，此处不再重复。

	// 运行流水线
	t := logger.Start("pipeline", "run")
	if err := pipelineRun(context.Background(), comp, set, logger); err != nil {
		// 分类到最接近的退出码（运行期错误）
		code := string(diag.Classify(err))
		logger.Error("pipeline", code, "first error", &start)
		diag.IncOp("pipeline", "error", "error")
		if code != "" && code != string(diag.CodeUnknown) {
			diag.IncError("pipeline", code)
		}
		if !errors.Is(err, context.Canceled) {
			fprintf(os.Stderr, "运行失败: %v\n", err)
		}
		if term != nil {
			term.RunFinish(false, time.Since(start))
		}
		return 1
	}
	if t != nil {
		t.Finish("run", 0)
	}
	diag.IncOp("pipeline", "finish", "success")
	diag.ObserveDuration("pipeline", "finish", time.Since(start).Milliseconds())
	if term != nil {
		term.RunFinish(true, time.Since(start))
	}
	return 0
}

func fprintf(w *os.File, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }

func dumpConfig(c cfgpkg.Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	_, _ = os.Stderr.Write(append([]byte("有效配置:\n"), b...))
	_, _ = os.Stderr.Write([]byte("\n"))
	return nil
}

func hasDash(ss []string) bool {
	for _, s := range ss {
		if strings.TrimSpace(s) == "-" {
			return true
		}
	}
	return false
}

func writeConfig(path string, c cfgpkg.Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if path == "-" {
		_, err = os.Stdout.Write(append(b, '\n'))
		return err
	}
	// 不覆盖已存在文件
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		return err
	}
	_, _ = f.Write([]byte("\n"))
	return nil
}

func genCorrID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// loadDotEnv 读取简单的 .env 文件格式并注入进程环境。
// 规则：
// - 忽略不存在的文件；无法读取时返回错误（但调用处可忽略）。
// - 跳过空行与以 # 开头的行；支持可选的前缀 "export ".
// - 仅按首个 '=' 分割；key 为左侧去空白；value 去首尾空白；
// - 若 value 被成对的单/双引号包裹，则去除外层引号；双引号内常见转义 \n/\t/\\/\" 作最小处理。
// - 不覆盖已存在的环境变量（保持系统/调用者优先）。
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if key == "" {
			continue
		}
		// 去除成对引号
		if len(val) >= 2 {
			if (val[0] == '\'' && val[len(val)-1] == '\'') || (val[0] == '"' && val[len(val)-1] == '"') {
				quoted := val[0]
				val = val[1 : len(val)-1]
				if quoted == '"' {
					// 最小转义处理
					val = strings.ReplaceAll(val, "\\n", "\n")
					val = strings.ReplaceAll(val, "\\t", "\t")
					val = strings.ReplaceAll(val, "\\r", "\r")
					val = strings.ReplaceAll(val, "\\\"", "\"")
					val = strings.ReplaceAll(val, "\\\\", "\\")
				}
			}
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, val)
	}
	return s.Err()
}

// normalizeInitArg: 允许 --init-config 在未提供路径值时采用默认值当前目录 "."。
// 兼容以下形式：
//
//	--init-config                => 等价于 --init-config .
//	--init-config=out
//	--init-config out
//
// 仅在检测到“裸开关或后继为下一个开关”的情况下插入默认值。
func normalizeInitArg() {
	args := os.Args
	if len(args) <= 1 {
		return
	}
	out := make([]string, 0, len(args)+1)
	out = append(out, args[0])
	for i := 1; i < len(args); i++ {
		a := args[i]
		out = append(out, a)
		if a == "--init-config" || a == "-init-config" {
			// 若已到末尾，补一个默认值
			if i == len(args)-1 {
				out = append(out, ".")
				continue
			}
			// 若下一个是开关（以 - 开头），则补默认值
			if strings.HasPrefix(args[i+1], "-") {
				out = append(out, ".")
				continue
			}
		}
	}
	os.Args = out
}

// deriveDotEnvPath 根据配置目标路径，推导 .env 生成位置。
// 规则：
// - 若 dest 为 "-"（stdout），则返回当前目录下的 .env
// - 否则返回与 dest 同目录的 .env
// deriveDotEnvPath: 不再使用（init-config 语义已改为目录）。

// writeDotEnv 生成 .env 模板（若文件已存在则跳过）。
// 仅创建文件；不覆盖，不合并。
func writeDotEnv(path string) error {
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		// 已存在直接跳过
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	// 构造内容：包含支持的覆盖项与常见 Provider 密钥。
	var b strings.Builder
	b.WriteString("# LLM-SPT .env 模板（由 --init-config 生成）\n")
	b.WriteString("# 优先级：CLI > ENV(.env) > JSON\n")
	b.WriteString("# 空值表示未设置；按需填写后移除本行注释。\n\n")

	// 通用：配置源
	b.WriteString("# 配置来源（可二选一）\n")
	b.WriteString("LLM_SPT_CONFIG_FILE=\n")
	b.WriteString("LLM_SPT_CONFIG_JSON=\n\n")

	// 顶层覆盖
	b.WriteString("# 运行参数覆盖\n")
	b.WriteString("LLM_SPT_INPUTS=\n")
	b.WriteString("LLM_SPT_CONCURRENCY=\n")
	b.WriteString("LLM_SPT_MAX_TOKENS=\n")
	b.WriteString("LLM_SPT_MAX_RETRIES=\n")
	b.WriteString("LLM_SPT_LLM=\n\n")

	// 组件选择
	b.WriteString("# 组件选择\n")
	b.WriteString("LLM_SPT_COMPONENTS_READER=\n")
	b.WriteString("LLM_SPT_COMPONENTS_SPLITTER=\n")
	b.WriteString("LLM_SPT_COMPONENTS_BATCHER=\n")
	b.WriteString("LLM_SPT_COMPONENTS_WRITER=\n")
	b.WriteString("LLM_SPT_COMPONENTS_PROMPT_BUILDER=\n")
	b.WriteString("LLM_SPT_COMPONENTS_DECODER=\n")
	b.WriteString("LLM_SPT_COMPONENTS_ASSEMBLER=\n\n")

	// Provider: openai
	b.WriteString("# Provider 覆盖（openai）\n")
	b.WriteString("LLM_SPT_PROVIDER__openai__CLIENT=\n")
	b.WriteString("LLM_SPT_PROVIDER__openai__LIMITS_RPM=\n")
	b.WriteString("LLM_SPT_PROVIDER__openai__LIMITS_TPM=\n")
	b.WriteString("LLM_SPT_PROVIDER__openai__LIMITS_MAX_TOKENS_PER_REQ=\n")
	b.WriteString("LLM_SPT_PROVIDER__openai__OPTIONS_JSON=\n\n")

	// Provider: gemini
	b.WriteString("# Provider 覆盖（gemini）\n")
	b.WriteString("LLM_SPT_PROVIDER__gemini__CLIENT=\n")
	b.WriteString("LLM_SPT_PROVIDER__gemini__LIMITS_RPM=\n")
	b.WriteString("LLM_SPT_PROVIDER__gemini__LIMITS_TPM=\n")
	b.WriteString("LLM_SPT_PROVIDER__gemini__LIMITS_MAX_TOKENS_PER_REQ=\n")
	b.WriteString("LLM_SPT_PROVIDER__gemini__OPTIONS_JSON=\n\n")

	// 常见供应商 API Key（由 Provider 客户端读取，不经 LLM_SPT_ 前缀）
	b.WriteString("# 常见供应商 API Key\n")
	b.WriteString("OPENAI_API_KEY=\n")
	b.WriteString("GOOGLE_API_KEY=\n")
	b.WriteString("\n")

	// 写入（不覆盖）
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(b.String()); err != nil {
		return err
	}
	return nil
}

// preflightCheckOutputDir: 当 Writer 使用文件系统实现(fs)时，启动前检查输出目录可写性。
// 规则：
// - 若目录已存在：尝试创建并删除临时文件；失败则判为不可写。
// - 若目录不存在：检查父目录是否可写（尝试在父目录创建并删除临时目录）。
// 仅针对 fs writer 生效；其他 writer 跳过。
func preflightCheckOutputDir(cfg cfgpkg.Config) error {
	// 计算生效的 writer 名称
	def := cfgpkg.Defaults()
	writerName := cfg.Components.Writer
	if strings.TrimSpace(writerName) == "" {
		writerName = def.Components.Writer
	}
	if strings.TrimSpace(writerName) != "fs" {
		return nil
	}
	// 解析 fs writer 的 output_dir
	var wopts struct {
		OutputDir string `json:"output_dir"`
	}
	if len(cfg.Options.Writer) > 0 {
		_ = json.Unmarshal(cfg.Options.Writer, &wopts)
	}
	dir := strings.TrimSpace(wopts.OutputDir)
	if dir == "" {
		// 未指定时无法可靠检查，让装配阶段按实现自行报错
		return nil
	}
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		// 目录存在：尝试写入临时文件
		f, err := os.CreateTemp(dir, ".wcheck-*")
		if err != nil {
			return err
		}
		name := f.Name()
		_ = f.Close()
		_ = os.Remove(name)
		return nil
	} else if err == nil && !st.IsDir() {
		return fmt.Errorf("路径存在但不是目录: %s", dir)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	// 目录不存在：检查父目录可写性
	parent := filepath.Dir(dir)
	if parent == "" || parent == dir {
		return fmt.Errorf("无法确定父目录: %s", dir)
	}
	pst, err := os.Stat(parent)
	if err != nil {
		return err
	}
	if !pst.IsDir() {
		return fmt.Errorf("父路径不是目录: %s", parent)
	}
	tmpd, err := os.MkdirTemp(parent, ".wcheck-*")
	if err != nil {
		return err
	}
	_ = os.RemoveAll(tmpd)
	return nil
}
