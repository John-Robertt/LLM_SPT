package pipeline

import (
    "context"
    "errors"
    "fmt"
    "encoding/json"
    "io"
    "strings"
    "sync"
    "time"

	"llmspt/internal/diag"
	"llmspt/internal/prompt"
	"llmspt/internal/rate"
	"llmspt/pkg/contract"
)

// - 单点并发：仅此层管理并发与背压；原子组件均为同步、无内部并发。
// - 顺序门闩：同一 FileID 的批按 BatchIndex 严格递增提交；乱序结果暂存，连续冲刷。
// - 首错取消：任一阶段出现错误，记录首错并 cancel 整体；排空后返回该错误。
// - 预算：在进入 LLM 前使用 PromptBuilder 固定开销预扣，结合配置与 Provider 限额计算有效 MaxTokens。

// Components 聚合运行所需的原子组件。
type Components struct {
	Reader        contract.Reader
	Splitter      contract.Splitter
	Batcher       contract.Batcher
	PromptBuilder contract.PromptBuilder
	LLM           contract.LLMClient
	Decoder       contract.Decoder
	Assembler     contract.Assembler
	Writer        contract.Writer
}

// Settings 运行期配置（最小必要）。
type Settings struct {
	// 输入根与输出根（输出由 Writer 的 options 决定，这里只保留输入）
	Inputs      []string
	Concurrency int
	// 预算：最大 token、估算参数（bytesPerToken）；若 <=0 则关闭预算
	MaxTokens     int
	BytesPerToken int
	// MaxRetries: LLM/Decoder 阶段最大重试次数（>=0）。0 表示不重试。
	MaxRetries int
	// 限流闸门（可选）：若非空，则在调用 LLM 前调用 Gate.Wait
	Gate rate.Gate
	// 限流分组键（外部根据 Provider 生成）
	GateKey rate.LimitKey
}

// Run 执行完整流水线：Reader → Splitter → Batcher → Prompt → (Gate) → LLM → Decoder → Assembler → Writer。
// 约束：
// - 所有组件均为同步实现；
// - LLM 调用是并发的唯一重负载点，受 Concurrency 和 Gate 控制；
// - 同一文件的批次按 BatchIndex 顺序提交给 Assembler/Writer，保证输出稳定。
func Run(ctx context.Context, comp Components, set Settings, logger *diag.Logger) error {
	if err := sanity(comp, set); err != nil {
		return fmt.Errorf("sanity: %w", err)
	}

	// 预估固定提示词开销（用于批量预算）
	effMax := set.MaxTokens
	if set.MaxTokens > 0 {
		_, overhead := prompt.EffectiveMaxTokens(comp.PromptBuilder, set.BytesPerToken, set.MaxTokens)
		effMax = set.MaxTokens - overhead
		if effMax <= 0 {
			return fmt.Errorf("%w: effective token budget <= 0 after overhead", contract.ErrBudgetExceeded)
		}
	}

	// 顺序门闩：每个文件独立装配/写出。
	// 由于 Reader/ Splitter 按文件遍历，我们逐文件处理，内部对批并发执行。
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

    perFile := func(fileID contract.FileID, recs []contract.Record) error {
		// 切批
		btimer := (*diag.Timer)(nil)
		if logger != nil {
			btimer = logger.StartWith("batcher", "make", string(fileID), "")
		}
        batches, err := comp.Batcher.Make(ctx, recs, contract.BatchLimit{MaxTokens: effMax})
        if err != nil {
			if logger != nil {
				code := diag.Classify(err)
				// 使用外层开始时间不重要，Error 会自行计算时长为空或传入 nil
				logger.ErrorWith("batcher", string(code), "make failed", nil, string(fileID), "")
				diag.IncOp("batcher", "error", "error")
				if code != diag.CodeUnknown {
					diag.IncError("batcher", string(code))
				}
			}
			return fmt.Errorf("batcher make: %w", err)
        }
        if btimer != nil {
            btimer.Finish("make", int64(len(batches)))
            diag.IncOp("batcher", "finish", "success")
        }
        // 终端提示：文件开始（即使 total=0 也要发）
        if t := diag.GetTerminal(); t != nil {
            t.FileStart(string(fileID), len(batches))
        }
        fileStart := time.Now()
        ok := false
        defer func() {
            if t := diag.GetTerminal(); t != nil {
                t.FileFinish(ok, time.Since(fileStart))
            }
        }()
        if len(batches) == 0 {
            // 没有目标，写空输出
            atimer := (*diag.Timer)(nil)
            if logger != nil {
                atimer = logger.StartWith("assembler", "assemble", string(fileID), "")
			}
			r, aerr := comp.Assembler.Assemble(ctx, fileID, nil)
			if aerr != nil {
				if logger != nil {
					code := diag.Classify(aerr)
					logger.ErrorWith("assembler", string(code), "assemble failed", nil, string(fileID), "")
					diag.IncOp("assembler", "error", "error")
					if code != diag.CodeUnknown {
						diag.IncError("assembler", string(code))
					}
				}
				return fmt.Errorf("assembler assemble: %w", aerr)
			}
			if atimer != nil {
				atimer.Finish("assemble", 0)
				diag.IncOp("assembler", "finish", "success")
			}

			wtimer := (*diag.Timer)(nil)
			if logger != nil {
				wtimer = logger.StartWith("writer", "write", string(fileID), "")
			}
			werr := comp.Writer.Write(ctx, contract.ArtifactID(fileID), r)
            if werr != nil {
                if logger != nil {
                    code := diag.Classify(werr)
                    logger.ErrorWith("writer", string(code), "write failed", nil, string(fileID), "")
                    diag.IncOp("writer", "error", "error")
                    if code != diag.CodeUnknown {
                        diag.IncError("writer", string(code))
                    }
                }
                return fmt.Errorf("writer write: %w", werr)
            }
            if wtimer != nil {
                wtimer.Finish("write", 0)
                diag.IncOp("writer", "finish", "success")
            }
            // 写出空 JSONL 边车
            if perr := comp.Writer.Write(ctx, contract.ArtifactID(string(fileID)+".jsonl"), strings.NewReader("")); perr != nil {
                if logger != nil {
                    code := diag.Classify(perr)
                    logger.ErrorWith("writer", string(code), "write failed", nil, string(fileID), "")
                    diag.IncOp("writer", "error", "error")
                    if code != diag.CodeUnknown { diag.IncError("writer", string(code)) }
                }
                return fmt.Errorf("writer write(jsonl): %w", perr)
            }
            ok = true
            return nil
        }

		// 并发 worker 处理 LLM/Decoder，结果通过门闩按序装配/写出
		type job struct{ b contract.Batch }
		type res struct {
			idx   int64
			spans []contract.SpanResult
			err   error
		}
		// 有界通道：默认 2×并发度，形成自然背压
		inCh := make(chan job, set.Concurrency*2)
		outCh := make(chan res, set.Concurrency*2)

		// workers
		var wg sync.WaitGroup
		worker := func() {
			defer wg.Done()
            for j := range inCh {
                // 先构建 Prompt（一次性），再基于实际 Prompt 内容估算 tokens 更接近真实请求规模
                var err error
                var p contract.Prompt
                pbtimer := (*diag.Timer)(nil)
                if logger != nil {
                    pbtimer = logger.StartWith("prompt_builder", "build", string(j.b.FileID), fmt.Sprintf("%d", j.b.BatchIndex))
					logger.DebugStart("prompt_builder", "build_req", string(j.b.FileID), fmt.Sprintf("%d", j.b.BatchIndex), map[string]string{
						"from":    fmt.Sprintf("%d", j.b.TargetFrom),
						"to":      fmt.Sprintf("%d", j.b.TargetTo),
						"records": fmt.Sprintf("%d", len(j.b.Records)),
					})
				}
				p, err = comp.PromptBuilder.Build(ctx, j.b)
				if err != nil {
					if logger != nil {
						code := diag.Classify(err)
						logger.ErrorWith("prompt_builder", string(code), "build failed", nil, string(j.b.FileID), fmt.Sprintf("%d", j.b.BatchIndex))
						diag.IncOp("prompt_builder", "error", "error")
						if code != diag.CodeUnknown {
							diag.IncError("prompt_builder", string(code))
						}
					}
					outCh <- res{idx: j.b.BatchIndex, err: err}
					continue
				}
                if pbtimer != nil {
                    pbtimer.Finish("build", int64(len(j.b.Records)))
                    diag.IncOp("prompt_builder", "finish", "success")
                }
                // 基于 Prompt 内容估算 tokens（包含 system/user/schema 文本）；更保守
                tokens := 0
                if set.MaxTokens > 0 {
                    bpt := set.BytesPerToken
                    if bpt <= 0 {
                        bpt = 4
                    }
                    tokens = approxPromptTokens(p, bpt)
                }
                // 调用 LLM + 解码（带重试）
                tgt := contract.Target{FileID: j.b.FileID, From: j.b.TargetFrom, To: j.b.TargetTo}
				attempts := set.MaxRetries + 1
				var lastErr error
				for attempt := 0; attempt < attempts; attempt++ {
					if set.Gate != nil {
						if logger != nil {
							logger.DebugStart("gate", "ask", string(j.b.FileID), fmt.Sprintf("%d", j.b.BatchIndex), map[string]string{
								"requests": "1",
								"tokens":   fmt.Sprintf("%d", tokens),
								"attempt":  fmt.Sprintf("%d", attempt+1),
							})
						}
						if err := set.Gate.Wait(ctx, rate.Ask{Key: set.GateKey, Requests: 1, Tokens: tokens}); err != nil {
							if logger != nil {
								code := diag.Classify(err)
								logger.ErrorWith("gate", string(code), "wait failed", nil, string(j.b.FileID), fmt.Sprintf("%d", j.b.BatchIndex))
								diag.IncOp("gate", "error", "error")
								if code != diag.CodeUnknown {
									diag.IncError("gate", string(code))
								}
							}
							lastErr = err
							break // Gate 错误不重试（通常为取消或输入非法）
						}
					}

					// LLM 调用
					lltimer := (*diag.Timer)(nil)
					if logger != nil {
						lltimer = logger.StartWithKV("llm_client", "invoke", string(j.b.FileID), fmt.Sprintf("%d", j.b.BatchIndex), map[string]string{
							"tokens":  fmt.Sprintf("%d", tokens),
							"attempt": fmt.Sprintf("%d", attempt+1),
						})
					}
					raw, err := comp.LLM.Invoke(ctx, j.b, p)
					if err != nil {
                    if logger != nil {
                        code := diag.Classify(err)
                        // 若为上游 HTTP 错误，附带状态码/消息
                        var kv map[string]string
                        var ue contract.UpstreamError
                        if errors.As(err, &ue) {
                            kv = map[string]string{
                                "http_status": fmt.Sprintf("%d", ue.UpstreamStatus()),
                            }
                            if m := strings.TrimSpace(ue.UpstreamMessage()); m != "" {
                                if len(m) > 200 { m = m[:200] }
                                kv["upstream_msg"] = m
                            }
                            logger.ErrorWithKV("llm_client", string(code), "invoke failed", nil, string(j.b.FileID), fmt.Sprintf("%d", j.b.BatchIndex), kv)
                        } else {
                            logger.ErrorWith("llm_client", string(code), "invoke failed", nil, string(j.b.FileID), fmt.Sprintf("%d", j.b.BatchIndex))
                        }
                        diag.IncOp("llm_client", "error", "error")
                        if code != diag.CodeUnknown {
                            diag.IncError("llm_client", string(code))
                        }
                    }
						lastErr = err
						if attempt+1 < attempts && shouldRetryInvoke(err) {
							_ = sleepWithCtx(ctx, 200*time.Millisecond)
							continue
						}
						break
					}
					if lltimer != nil {
						lltimer.Finish("invoke", int64(tokens))
						diag.IncOp("llm_client", "finish", "success")
					}

					// 解码
					var spans []contract.SpanResult
					dctimer := (*diag.Timer)(nil)
					if logger != nil {
						dctimer = logger.StartWith("decoder", "decode", string(j.b.FileID), fmt.Sprintf("%d", j.b.BatchIndex))
					}
                if dm, ok := comp.Decoder.(contract.DecoderWithMeta); ok {
                    // 构建 idx→meta 只读映射（批窗口内可见），并回填源文本用于协议校验（如“原文回显”检测）
                    idxMeta := make(contract.IndexMetaMap, len(j.b.Records))
                    for _, r := range j.b.Records {
                        // 拷贝一份 meta
                        mm := make(contract.Meta, len(r.Meta)+1)
                        for k, v := range r.Meta {
                            mm[k] = v
                        }
                        // 附带源文本供解码器用于协议层校验（键名以 _ 前缀避免与业务字段冲突）
                        mm["_src_text"] = r.Text
                        idxMeta[r.Index] = mm
                    }
                    spans, err = dm.DecodeWithMeta(ctx, tgt, raw, idxMeta)
                } else {
                    spans, err = comp.Decoder.Decode(ctx, tgt, raw)
                }
					if err != nil {
						if logger != nil {
							code := diag.Classify(err)
							logger.ErrorWith("decoder", string(code), "decode failed", nil, string(j.b.FileID), fmt.Sprintf("%d", j.b.BatchIndex))
							diag.IncOp("decoder", "error", "error")
							if code != diag.CodeUnknown {
								diag.IncError("decoder", string(code))
							}
						}
						lastErr = err
						if attempt+1 < attempts && shouldRetryDecode(err) {
							_ = sleepWithCtx(ctx, 200*time.Millisecond)
							continue
						}
						break
					}
					if dctimer != nil {
						dctimer.Finish("decode", int64(len(spans)))
					}
					diag.IncOp("decoder", "finish", "success")
					// 成功
					outCh <- res{idx: j.b.BatchIndex, spans: spans, err: nil}
					lastErr = nil
					goto jobdone
				}
				// 最终失败
				outCh <- res{idx: j.b.BatchIndex, err: lastErr}
			jobdone:
				_ = 0
			}
		}

		nWorkers := set.Concurrency
		if nWorkers < 1 {
			nWorkers = 1
		}
		wg.Add(nWorkers)
		for i := 0; i < nWorkers; i++ {
			go worker()
		}

		// 生产者
		go func() {
			defer close(inCh)
			for _, b := range batches {
				select {
				case <-ctx.Done():
					return
				case inCh <- job{b: b}:
				}
			}
		}()

		// 提交门闩：按 BatchIndex 连续冲刷；就绪即装配并通过管道流式写出
		expect := int64(0)
		buf := make(map[int64][]contract.SpanResult)
		var firstErr error

		// 建立管道，单次调用 Writer.Write，以流式方式落盘
		pr, pw := io.Pipe()
		wdone := make(chan error, 1)
		wtimer := (*diag.Timer)(nil)
		if logger != nil {
			wtimer = logger.StartWith("writer", "write", string(fileID), "")
		}
		go func() {
			err := comp.Writer.Write(ctx, contract.ArtifactID(fileID), pr)
			wdone <- err
		}()

		// JSONL 边车：并行写出至 <artifact>.jsonl
		prPairs, pwPairs := io.Pipe()
		wdonePairs := make(chan error, 1)
		go func() {
			jsonlID := contract.ArtifactID(string(fileID) + ".jsonl")
			err := comp.Writer.Write(ctx, jsonlID, prPairs)
			wdonePairs <- err
		}()
		enc := json.NewEncoder(pwPairs)
		enc.SetEscapeHTML(false)

        // 仅用于进度展示（不再用于退出条件）
        want := len(batches)
        doneCount := 0
        errCount := 0

        // 由 workers 生命周期决定 outCh 关闭，避免基于固定计数阻塞
        go func() {
            wg.Wait()
            close(outCh)
        }()

        for r := range outCh {
            // 进度统计（无论成功/失败）
            doneCount++
            if r.err != nil {
                errCount++
            }
            if t := diag.GetTerminal(); t != nil {
                t.FileProgress(doneCount, want, errCount)
            }
            if r.err != nil && firstErr == nil {
                firstErr = r.err
                cancel()
                // 不立刻 return，继续排空 outCh 以便 orderly 结束
            }
            if r.err == nil {
                buf[r.idx] = r.spans
                for {
                    spans, ok := buf[expect]
                    if !ok {
                        break
                    }
                    // 先生成 JSONL 边车（基于当前批 Records 与 spans）
                    {
                        recs := batches[expect].Records
                        // 移动指针，减少重复扫描
                        pos := 0
                        if len(spans) > 0 {
                            f0 := spans[0].From
                            for pos < len(recs) && recs[pos].Index < f0 {
                                pos++
                            }
                        }
                        for _, sp := range spans {
                            for pos < len(recs) && recs[pos].Index < sp.From {
                                pos++
                            }
                            var sb strings.Builder
                            j := pos
                            firstTok := true
                            for j < len(recs) && recs[j].Index <= sp.To {
                                if !firstTok { sb.WriteByte('\n') } else { firstTok = false }
                                sb.WriteString(recs[j].Text)
                                j++
                            }
                            dst := sp.Output
                            if sp.Meta != nil {
                                if v := sp.Meta["dst_text"]; strings.TrimSpace(v) != "" {
                                    dst = v
                                }
                            }
                            row := struct {
                                FileID string        `json:"file_id"`
                                From   int64         `json:"from"`
                                To     int64         `json:"to"`
                                Src    string        `json:"src"`
                                Dst    string        `json:"dst"`
                                Meta   contract.Meta `json:"meta,omitempty"`
                            }{
                                FileID: string(fileID),
                                From:   int64(sp.From),
                                To:     int64(sp.To),
                                Src:    sb.String(),
                                Dst:    dst,
                                Meta:   sp.Meta,
                            }
                            if err := enc.Encode(&row); err != nil && firstErr == nil {
                                firstErr = err
                                cancel()
                                break
                            }
                        }
                    }
                    atimer := (*diag.Timer)(nil)
                    if logger != nil {
                        atimer = logger.StartWith("assembler", "assemble", string(fileID), fmt.Sprintf("%d", expect))
                    }
                    rd, aerr := comp.Assembler.Assemble(ctx, fileID, spans)
                    if aerr != nil {
                        if logger != nil {
                            code := diag.Classify(aerr)
                            logger.ErrorWith("assembler", string(code), "assemble failed", nil, string(fileID), fmt.Sprintf("%d", expect))
                            diag.IncOp("assembler", "error", "error")
                            if code != diag.CodeUnknown {
                                diag.IncError("assembler", string(code))
                            }
                        }
                        firstErr = aerr
                        cancel()
                        break
                    }
                    if atimer != nil {
                        atimer.Finish("assemble", int64(len(spans)))
                        diag.IncOp("assembler", "finish", "success")
                    }
                    if _, cerr := io.Copy(pw, rd); cerr != nil && firstErr == nil {
                        firstErr = cerr
                        cancel()
                        break
                    }
                    delete(buf, expect)
                    expect++
                }
            }
        }

        if firstErr != nil { _ = pw.CloseWithError(firstErr) } else { _ = pw.Close() }
        if firstErr != nil { _ = pwPairs.CloseWithError(firstErr) } else { _ = pwPairs.Close() }
        werr := <-wdone
        werrPairs := <-wdonePairs
        if firstErr != nil {
			if logger != nil {
				code := diag.Classify(firstErr)
				logger.ErrorWith("writer", string(code), "first error", nil, string(fileID), "")
				diag.IncOp("writer", "error", "error")
				if code != diag.CodeUnknown {
					diag.IncError("writer", string(code))
				}
			}
			return fmt.Errorf("worker first error: %w", firstErr)
		}
		if werr != nil || werrPairs != nil {
			if logger != nil {
				code := diag.Classify(func() error { if werr != nil { return werr }; return werrPairs }())
				logger.ErrorWith("writer", string(code), "write failed", nil, string(fileID), "")
				diag.IncOp("writer", "error", "error")
				if code != diag.CodeUnknown {
					diag.IncError("writer", string(code))
				}
			}
			if werr != nil { return fmt.Errorf("writer write: %w", werr) }
			return fmt.Errorf("writer write(jsonl): %w", werrPairs)
		}
        if wtimer != nil {
            wtimer.Finish("write", 1)
            diag.IncOp("writer", "finish", "success")
        }
        ok = true
        return nil
    }

	// Reader 遍历文件；逐文件拆分
	rtimer := (*diag.Timer)(nil)
	if logger != nil {
		rtimer = logger.Start("reader", "iterate")
	}
    err := comp.Reader.Iterate(ctx, set.Inputs, func(fid contract.FileID, rc io.ReadCloser) error {
        defer rc.Close()
        stimer := (*diag.Timer)(nil)
        if logger != nil {
            stimer = logger.StartWith("splitter", "split", string(fid), "")
        }
		recs, err := comp.Splitter.Split(ctx, fid, rc)
		if err != nil {
			if logger != nil {
				code := diag.Classify(err)
				logger.ErrorWith("splitter", string(code), "split failed", nil, string(fid), "")
				diag.IncOp("splitter", "error", "error")
				if code != diag.CodeUnknown {
					diag.IncError("splitter", string(code))
				}
			}
			return fmt.Errorf("splitter split: %w", err)
		}
		if stimer != nil {
			stimer.Finish("split", int64(len(recs)))
			diag.IncOp("splitter", "finish", "success")
		}
        if len(recs) == 0 {
            // 没有可处理内容：按空输出
            if t := diag.GetTerminal(); t != nil {
                t.FileStart(string(fid), 0)
            }
            fileStart := time.Now()
            ok := false
            defer func() {
                if t := diag.GetTerminal(); t != nil {
                    t.FileFinish(ok, time.Since(fileStart))
                }
            }()
            atimer := (*diag.Timer)(nil)
            if logger != nil {
                atimer = logger.StartWith("assembler", "assemble", string(fid), "")
            }
            r, aerr := comp.Assembler.Assemble(ctx, fid, nil)
            if aerr != nil {
                if logger != nil {
                    code := diag.Classify(aerr)
                    logger.ErrorWith("assembler", string(code), "assemble failed", nil, string(fid), "")
                    diag.IncOp("assembler", "error", "error")
                    if code != diag.CodeUnknown {
                        diag.IncError("assembler", string(code))
                    }
                }
                return fmt.Errorf("assembler assemble: %w", aerr)
            }
            if atimer != nil {
                atimer.Finish("assemble", 0)
                diag.IncOp("assembler", "finish", "success")
            }
            wtimer := (*diag.Timer)(nil)
            if logger != nil {
                wtimer = logger.StartWith("writer", "write", string(fid), "")
            }
            werr := comp.Writer.Write(ctx, contract.ArtifactID(fid), r)
            if werr != nil {
                if logger != nil {
                    code := diag.Classify(werr)
                    logger.ErrorWith("writer", string(code), "write failed", nil, string(fid), "")
                    diag.IncOp("writer", "error", "error")
                    if code != diag.CodeUnknown {
                        diag.IncError("writer", string(code))
                    }
                }
                return fmt.Errorf("writer write: %w", werr)
            }
            if wtimer != nil {
                wtimer.Finish("write", 1)
                diag.IncOp("writer", "finish", "success")
            }
            // 写出空 JSONL 边车
            if perr := comp.Writer.Write(ctx, contract.ArtifactID(string(fid)+".jsonl"), strings.NewReader("")); perr != nil {
                if logger != nil {
                    code := diag.Classify(perr)
                    logger.ErrorWith("writer", string(code), "write failed", nil, string(fid), "")
                    diag.IncOp("writer", "error", "error")
                    if code != diag.CodeUnknown { diag.IncError("writer", string(code)) }
                }
                return fmt.Errorf("writer write(jsonl): %w", perr)
            }
            ok = true
            return nil
        }
		if err := perFile(fid, recs); err != nil {
			return fmt.Errorf("perFile: %w", err)
		}
		return nil
	})
	if err != nil {
		if logger != nil {
			code := diag.Classify(err)
			logger.Error("reader", string(code), "iterate failed", nil)
			diag.IncOp("reader", "error", "error")
			if code != diag.CodeUnknown {
				diag.IncError("reader", string(code))
			}
		}
		return fmt.Errorf("reader iterate: %w", err)
	}
	if rtimer != nil {
		rtimer.Finish("iterate", 0)
		diag.IncOp("reader", "finish", "success")
	}
	return nil
}

func sanity(c Components, s Settings) error {
	if c.Reader == nil || c.Splitter == nil || c.Batcher == nil || c.PromptBuilder == nil || c.LLM == nil || c.Decoder == nil || c.Assembler == nil || c.Writer == nil {
		return errors.New("pipeline: missing components")
	}
	if s.Concurrency < 1 {
		s.Concurrency = 1
	}
	if len(s.Inputs) == 0 {
		return errors.New("pipeline: empty inputs")
	}
	return nil
}

// shouldRetryInvoke: 根据错误类型判断是否重试 LLM 调用。
// - 取消/超时：不重试；
// - 预算/限流：重试（交由 Gate 控制速率）；
// - 网络类错误：重试；
// - 其他未知错误：不重试。
func shouldRetryInvoke(err error) bool {
	if err == nil {
		return false
	}
	code := diag.Classify(err)
	switch code {
	case diag.CodeCancel:
		return false
	case diag.CodeBudget, diag.CodeNetwork:
		return true
	default:
		return false
	}
}

// shouldRetryDecode: 针对“模型幻觉/响应无效”做有限次重试。
// - 协议/响应无效：重试；
// - 取消/超时/输入非法等：不重试。
func shouldRetryDecode(err error) bool {
	if err == nil {
		return false
	}
	code := diag.Classify(err)
	return code == diag.CodeProtocol
}

// sleepWithCtx: 可取消的 sleep（最小实现）。
func sleepWithCtx(ctx context.Context, d time.Duration) error {
    if d <= 0 {
        return nil
    }
    t := time.NewTimer(d)
    defer t.Stop()
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-t.C:
        return nil
    }
}

// approxPromptTokens: 基于 Prompt 实际文本内容的简易 token 估算（tokens ≈ ceil(bytes / bpt)）。
// 目的：比“仅按窗口文本估算”更接近真实请求体规模，便于 Gate 进行单请求上限判定。
func approxPromptTokens(p contract.Prompt, bpt int) int {
    if bpt <= 0 {
        bpt = 4
    }
    total := 0
    switch v := p.(type) {
    case contract.TextPrompt:
        total = len(v)
    case contract.ChatPrompt:
        for _, m := range v {
            if m.Content == "" {
                continue
            }
            total += len(m.Content)
        }
    default:
        return 0
    }
    if total <= 0 {
        return 0
    }
    return (total + bpt - 1) / bpt
}
