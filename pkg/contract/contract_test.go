package contract

import (
    "errors"
    "path/filepath"
    "testing"
)

// TestNormalizeFileID 验证路径规范化逻辑。
func TestNormalizeFileID(t *testing.T) {
    // 原有测试用例
    wpath := filepath.Join("a", "b", "c")
    basicCases := map[string]string{
        wpath: "a/b/c",
        "./x/../y": "y",
        "": ".",
    }
    for in, want := range basicCases {
        got := NormalizeFileID(in)
        if string(got) != want {
            t.Fatalf("基础测试 %s -> %s, 预期 %s", in, got, want)
        }
    }

    // 扩展测试用例 - 系统化覆盖
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        // 反斜杠转换
        {"Windows路径", "C:\\Users\\test\\file.txt", "C:/Users/test/file.txt"},
        {"相对路径反斜杠", "src\\main\\java\\App.java", "src/main/java/App.java"},
        
        // path.Clean 功能
        {"清理多余斜杠", "path//to///file.txt", "path/to/file.txt"},
        {"清理当前目录", "path/./to/./file.txt", "path/to/file.txt"},
        {"处理父目录", "path/to/../from/file.txt", "path/from/file.txt"},
        
        // 边界情况
        {"单个点", ".", "."},
        {"双点", "..", ".."},
        {"根路径", "/", "/"},
        {"Windows根", "C:\\", "C:"},
        
        // 跨平台混合分隔符
        {"混合分隔符", "C:\\Users/test\\Documents/file.txt", "C:/Users/test/Documents/file.txt"},
        {"复杂混合路径", "src\\..\\test/./data\\\\file.txt", "test/data/file.txt"},
        
        // 特殊字符
        {"中文路径", "项目\\文档/测试.txt", "项目/文档/测试.txt"},
        {"空格路径", "My Documents\\My File.txt", "My Documents/My File.txt"},
        
        // 绝对路径
        {"Unix绝对路径", "/home/user/../admin/file.txt", "/home/admin/file.txt"},
        {"Windows绝对路径", "C:\\Program Files\\..\\Windows\\System32", "C:/Windows/System32"},
        
        // 极端情况
        {"仅分隔符", "\\\\\\///", "/"},
        {"复杂父目录", "a\\b\\c\\..\\..\\..\\..\\d", "../d"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := NormalizeFileID(tt.input)
            if string(result) != tt.expected {
                t.Errorf("NormalizeFileID(%q) = %q, expected %q", tt.input, result, tt.expected)
            }
        })
    }
}

// BenchmarkNormalizeFileID 性能基准测试
func BenchmarkNormalizeFileID(b *testing.B) {
    testPaths := []string{
        "C:\\Users\\test\\Documents\\file.txt",
        "src/main/java/../../../test/data/file.txt",
        "path//to///many////slashes/file.txt",
        "very/long/path/with/many/segments/and/mixed\\separators/file.txt",
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        for _, path := range testPaths {
            NormalizeFileID(path)
        }
    }
}

// TestValidatePerRecordSuccess 验证按记录校验的成功路径及深拷贝。
func TestValidatePerRecordSuccess(t *testing.T) {
    tgt := Target{FileID: FileID("f"), From: 0, To: 2}
    cands := []SpanCandidate{
        {From: 0, To: 0, Output: "a"},
        {From: 1, To: 1, Output: "b", Meta: Meta{"k": "v"}},
        {From: 2, To: 2, Output: "c"},
    }
    spans, err := ValidatePerRecord(tgt, cands)
    if err != nil {
        t.Fatalf("unexpected err: %v", err)
    }
    cands[1].Output = "x"
    cands[1].Meta["k"] = "x"
    if spans[1].Output != "b" || spans[1].Meta["k"] != "v" {
        t.Fatalf("span 未拷贝")
    }
}

// TestValidatePerRecordErrors 覆盖各类错误分支。
func TestValidatePerRecordErrors(t *testing.T) {
    cases := []struct {
        name  string
        tgt   Target
        cands []SpanCandidate
        want  error
    }{
        {"bad target", Target{From: 2, To: 1}, nil, ErrInvalidInput},
        {"len mismatch", Target{From: 0, To: 1}, []SpanCandidate{{From: 0, To: 0}}, ErrResponseInvalid},
        {"from>to", Target{From: 0, To: 0}, []SpanCandidate{{From: 1, To: 0}}, ErrResponseInvalid},
        {"range candidate", Target{From: 0, To: 1}, []SpanCandidate{{From: 0, To: 1}, {From: 1, To: 1}}, ErrResponseInvalid},
        {"non contiguous", Target{From: 0, To: 1}, []SpanCandidate{{From: 0, To: 0}, {From: 0, To: 0}}, ErrResponseInvalid},
    }
    for _, tt := range cases {
        t.Run(tt.name, func(t *testing.T) {
            _, err := ValidatePerRecord(tt.tgt, tt.cands)
            if !errors.Is(err, tt.want) {
                t.Fatalf("want %v got %v", tt.want, err)
            }
        })
    }
}

// TestValidateWhole 验证整段校验。
func TestValidateWhole(t *testing.T) {
    tgt := Target{FileID: FileID("f"), From: 0, To: 2}
    cand := []SpanCandidate{{From: 0, To: 2, Output: "abc", Meta: Meta{"a": "b"}}}
    spans, err := ValidateWhole(tgt, cand)
    if err != nil {
        t.Fatalf("unexpected err: %v", err)
    }
    cand[0].Output = "x"
    cand[0].Meta["a"] = "x"
    if spans[0].Output != "abc" || spans[0].Meta["a"] != "b" {
        t.Fatalf("span 未拷贝")
    }
    tests := []struct {
        name  string
        tgt   Target
        cands []SpanCandidate
        want  error
    }{
        {"bad target", Target{From: 2, To: 1}, cand, ErrInvalidInput},
        {"len", tgt, []SpanCandidate{}, ErrResponseInvalid},
        {"range", tgt, []SpanCandidate{{From: 1, To: 2}}, ErrResponseInvalid},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := ValidateWhole(tt.tgt, tt.cands)
            if !errors.Is(err, tt.want) {
                t.Fatalf("want %v got %v", tt.want, err)
            }
        })
    }
}

// TestCloneString 验证字符串拷贝函数。
func TestCloneString(t *testing.T) {
    if cloneString("") != "" {
        t.Fatalf("空串应返回空串")
    }
    if cloneString("abc") != "abc" {
        t.Fatalf("普通字符串未保持内容")
    }
}

// TestCloneMeta 验证元信息拷贝函数。
func TestCloneMeta(t *testing.T) {
    if cloneMeta(nil) != nil {
        t.Fatalf("nil 应返回 nil")
    }
    m := Meta{"k": "v"}
    c := cloneMeta(m)
    m["k"] = "x"
    if c["k"] != "v" {
        t.Fatalf("clone 未独立")
    }
}

