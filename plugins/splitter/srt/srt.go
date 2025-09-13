package srt

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"llmspt/pkg/contract"
)

// Options 为 SRT Splitter 的可选配置（最小必要）。
type Options struct {
	// MaxFragmentBytes: 文本片段最大字节数。0 表示不限制。
	MaxFragmentBytes int `json:"max_fragment_bytes"`
	// AllowExts: 允许处理的文件扩展名（大小写不敏感，包含点，如 [".srt"]）。
	// 为空时采用默认 [".srt"]；显式设为空切片则表示不限制。
	AllowExts []string `json:"allow_exts"`
}

// Splitter 实现 SRT 拆分。
type Splitter struct {
	maxBytes int
	// 允许扩展名（小写），若为 nil 表示不限制。
	allow map[string]struct{}
}

// New 创建 SRT Splitter。
func New(opts *Options) *Splitter {
	mb := 0
	if opts != nil && opts.MaxFragmentBytes > 0 {
		mb = opts.MaxFragmentBytes
	}
	var allow map[string]struct{}
	if opts == nil || opts.AllowExts == nil {
		// 默认只处理 .srt
		allow = map[string]struct{}{".srt": {}}
	} else if len(opts.AllowExts) > 0 {
		allow = make(map[string]struct{}, len(opts.AllowExts))
		for _, e := range opts.AllowExts {
			if e == "" {
				continue
			}
			allow[strings.ToLower(e)] = struct{}{}
		}
	} else {
		// 显式空切片：不限制
		allow = nil
	}
	return &Splitter{maxBytes: mb, allow: allow}
}

var timeLineRe = regexp.MustCompile(`^\d{2}:\d{2}:\d{2},\d{3} --> \d{2}:\d{2}:\d{2},\d{3}`)

// Split 将单个 SRT 文件拆分为 []Record。
func (s *Splitter) Split(ctx context.Context, fileID contract.FileID, r io.Reader) ([]contract.Record, error) {
	// 根据扩展名提前判定是否处理
	if s.allow != nil {
		ext := strings.ToLower(path.Ext(string(fileID)))
		if _, ok := s.allow[ext]; !ok {
			return nil, nil
		}
	}
	br := bufio.NewReader(r)
	var recs []contract.Record
	var idx contract.Index

	for {
		if err := ctxErr(ctx); err != nil {
			return nil, err
		}

		// 读取一个块：序号行、时间轴行、文本若干行，空行结束
		seqLine, eof, err := readTrimmedLine(br)
		if err != nil {
			return nil, err
		}
		if eof {
			break
		}
		if seqLine == "" { // 跳过多余空行
			continue
		}
		// 验证序号
		if _, err := strconv.Atoi(seqLine); err != nil {
			return nil, fmt.Errorf("srt format error: invalid sequence line: %q", seqLine)
		}

		timeLine, _, err := readTrimmedLine(br)
		if err != nil {
			return nil, err
		}
		if !timeLineRe.MatchString(timeLine) {
			return nil, fmt.Errorf("srt format error: invalid time line: %q", timeLine)
		}

		// 收集文本行直到遇到空行或 EOF
		var texts []string
		// 维护累积字节数用于 MaxFragmentBytes 早返回。
		sumBytes := 0
		for {
			if err := ctxErr(ctx); err != nil {
				return nil, err
			}
			line, e, err := readTrimmedLine(br)
			if err != nil {
				return nil, err
			}
			if line == "" || e { // 空行或 EOF 结束当前块
				if e && line != "" {
					// 在 EOF 且最后一行非空时也累计并检查
					// 预测拼接后的总长度：已有文本字节 + 新行字节 + 现有行数作为分隔符 '\n'
					if s.maxBytes > 0 {
						predicted := sumBytes + len(line)
						if len(texts) > 0 {
							predicted += len(texts)
						} // 加上分隔符数量
						if predicted > s.maxBytes {
							return nil, fmt.Errorf("fragment too large: %d > %d", predicted, s.maxBytes)
						}
					}
					texts = append(texts, line)
				}
				break
			}
			// 早期尺寸检查：预测 join 后的大小（分隔符个数为当前行数）。
			if s.maxBytes > 0 {
				predicted := sumBytes + len(line)
				if len(texts) > 0 {
					predicted += len(texts)
				}
				if predicted > s.maxBytes {
					return nil, fmt.Errorf("fragment too large: %d > %d", predicted, s.maxBytes)
				}
			}
			texts = append(texts, line)
			sumBytes += len(line)
		}

		text := strings.Join(texts, "\n")
		// UTF-8 校验（最小必要：非法字节快速失败）
		if !utf8.ValidString(text) {
			return nil, errors.New("decode error: invalid UTF-8 in text block")
		}
		if s.maxBytes > 0 && len(text) > s.maxBytes {
			return nil, fmt.Errorf("fragment too large: %d > %d", len(text), s.maxBytes)
		}

		recs = append(recs, contract.Record{
			Index:  idx,
			FileID: fileID,
			Text:   text,
			Meta:   contract.Meta{"seq": seqLine, "time": timeLine},
		})
		idx++
	}
	return recs, nil
}

// readTrimmedLine 读取一行，归一 CRLF→LF，并去除结尾换行符；返回该行、是否 EOF。
func readTrimmedLine(br *bufio.Reader) (line string, eof bool, err error) {
	s, err := br.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			eof = true
		} else {
			return "", false, err
		}
	}
	// 去除尾部换行（\n 或 \r\n）
	s = strings.TrimSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\r")
	return s, eof && s == "", nil
}

func ctxErr(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
