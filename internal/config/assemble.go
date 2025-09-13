package config

import (
	"errors"
	"fmt"
	"strings"

	"llmspt/internal/pipeline"
	"llmspt/internal/rate"
	"llmspt/pkg/registry"
)

// Validate 对最小必要边界做静态校验。
func Validate(cfg Config) error {
	if len(cfg.Inputs) == 0 {
		return errors.New("config: inputs empty")
	}
	// 输入路径不得为空字符串；"-" 不能与其他根混用
	dash := false
	for _, r := range cfg.Inputs {
		if strings.TrimSpace(r) == "" {
			return errors.New("config: input path cannot be empty")
		}
		if strings.TrimSpace(r) == "-" {
			dash = true
		}
	}
	if dash && len(cfg.Inputs) > 1 {
		return errors.New("config: '-' cannot be mixed with other roots")
	}
	if cfg.Concurrency < 1 {
		return errors.New("config: concurrency must be >= 1")
	}
	if cfg.MaxTokens <= 0 {
		return errors.New("config: max_tokens must be > 0")
	}
	if cfg.MaxRetries < 0 {
		return errors.New("config: max_retries must be >= 0")
	}
	if cfg.LLM == "" {
		return errors.New("config: llm not set")
	}
	prov, ok := cfg.Provider[cfg.LLM]
	if !ok {
		return fmt.Errorf("config: provider %q not found", cfg.LLM)
	}
	if prov.Client == "" {
		return fmt.Errorf("config: provider %q missing client", cfg.LLM)
	}
	if prov.Limits.MaxTokensPerReq > 0 && cfg.MaxTokens > prov.Limits.MaxTokensPerReq {
		return fmt.Errorf("config: max_tokens(%d) exceeds provider.max_tokens_per_req(%d)", cfg.MaxTokens, prov.Limits.MaxTokensPerReq)
	}
	// 组件名若为空，使用默认名（由 Defaults() 提供）。此处只要最终有值即可。
	if name := effName(cfg.Components.Reader, Defaults().Components.Reader); registry.Reader[name] == nil {
		return fmt.Errorf("config: reader %q not registered", name)
	}
	if name := effName(cfg.Components.Splitter, Defaults().Components.Splitter); registry.Splitter[name] == nil {
		return fmt.Errorf("config: splitter %q not registered", name)
	}
	if name := effName(cfg.Components.Batcher, Defaults().Components.Batcher); registry.Batcher[name] == nil {
		return fmt.Errorf("config: batcher %q not registered", name)
	}
	if name := effName(cfg.Components.PromptBuilder, Defaults().Components.PromptBuilder); registry.PromptBuilder[name] == nil {
		return fmt.Errorf("config: prompt_builder %q not registered", name)
	}
	if name := effName(cfg.Components.Decoder, Defaults().Components.Decoder); registry.Decoder[name] == nil {
		return fmt.Errorf("config: decoder %q not registered", name)
	}
	if name := effName(cfg.Components.Assembler, Defaults().Components.Assembler); registry.Assembler[name] == nil {
		return fmt.Errorf("config: assembler %q not registered", name)
	}
	if name := effName(cfg.Components.Writer, Defaults().Components.Writer); registry.Writer[name] == nil {
		return fmt.Errorf("config: writer %q not registered", name)
	}
	if registry.LLMClient[prov.Client] == nil {
		return fmt.Errorf("config: llm client %q not registered", prov.Client)
	}
	return nil
}

// Assemble 构造 Components、Settings 与限流 Gate+Key。
// 严格 Options 解析在 registry （工厂）层进行；此处只传 raw JSON。
func Assemble(cfg Config) (pipeline.Components, pipeline.Settings, rate.Gate, rate.LimitKey, error) {
	if err := Validate(cfg); err != nil {
		return pipeline.Components{}, pipeline.Settings{}, nil, "", err
	}

	// 有效名称
	d := Defaults()
	rn := effName(cfg.Components.Reader, d.Components.Reader)
	sn := effName(cfg.Components.Splitter, d.Components.Splitter)
	bn := effName(cfg.Components.Batcher, d.Components.Batcher)
	pn := effName(cfg.Components.PromptBuilder, d.Components.PromptBuilder)
	dn := effName(cfg.Components.Decoder, d.Components.Decoder)
	an := effName(cfg.Components.Assembler, d.Components.Assembler)
	wn := effName(cfg.Components.Writer, d.Components.Writer)

	// 构造实例
	r, err := registry.Reader[rn](cfg.Options.Reader)
	if err != nil {
		return pipeline.Components{}, pipeline.Settings{}, nil, "", err
	}
	s, err := registry.Splitter[sn](cfg.Options.Splitter)
	if err != nil {
		return pipeline.Components{}, pipeline.Settings{}, nil, "", err
	}
	b, err := registry.Batcher[bn](cfg.Options.Batcher)
	if err != nil {
		return pipeline.Components{}, pipeline.Settings{}, nil, "", err
	}
	pb, err := registry.PromptBuilder[pn](cfg.Options.PromptBuilder)
	if err != nil {
		return pipeline.Components{}, pipeline.Settings{}, nil, "", err
	}
	dec, err := registry.Decoder[dn](cfg.Options.Decoder)
	if err != nil {
		return pipeline.Components{}, pipeline.Settings{}, nil, "", err
	}
	asm, err := registry.Assembler[an](cfg.Options.Assembler)
	if err != nil {
		return pipeline.Components{}, pipeline.Settings{}, nil, "", err
	}
	w, err := registry.Writer[wn](cfg.Options.Writer)
	if err != nil {
		return pipeline.Components{}, pipeline.Settings{}, nil, "", err
	}

	// LLM 客户端
	prov := cfg.Provider[cfg.LLM]
	newLLM := registry.LLMClient[prov.Client]
	llm, err := newLLM(prov.Options)
	if err != nil {
		return pipeline.Components{}, pipeline.Settings{}, nil, "", err
	}

	comp := pipeline.Components{
		Reader:        r,
		Splitter:      s,
		Batcher:       b,
		PromptBuilder: pb,
		LLM:           llm,
		Decoder:       dec,
		Assembler:     asm,
		Writer:        w,
	}

	// 限流 Gate（按 provider 限额构造；分组键从 options 中派生 API Key）
	gmap := map[rate.LimitKey]rate.Limits{}
	// 默认使用 API Key 派生分组键（更稳定）；若失败则退化为 provider 名称。
	key, derr := rate.DeriveKeyFromProviderOptions(prov.Client, prov.Options)
	if derr != nil {
		key = rate.LimitKey(cfg.LLM)
	}
	gmap[key] = rate.Limits{RPM: prov.Limits.RPM, TPM: prov.Limits.TPM, MaxTokensPerReq: prov.Limits.MaxTokensPerReq}
	gate := rate.NewGate(gmap, nil)

	set := pipeline.Settings{
		Inputs:      cloneStrings(cfg.Inputs),
		Concurrency: cfg.Concurrency,
		MaxTokens:   cfg.MaxTokens,
		// BytesPerToken: 由 Prompt 估算器默认 4；此处保持 0 使用默认。
		BytesPerToken: 0,
		MaxRetries:    cfg.MaxRetries,
		Gate:          gate,
		GateKey:       key,
	}

	return comp, set, gate, key, nil
}

func effName(got, def string) string {
	if got == "" {
		return def
	}
	return got
}
