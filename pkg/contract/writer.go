package contract

import (
	"context"
	"io"
)

// ArtifactID: 与 FileID 等价的持久化工件标识（语义别名）。
// 说明：架构层使用 ArtifactID 以强调“结果工件”，实现上与 FileID 复用同一表示，避免不必要的类型分裂。
type ArtifactID = FileID

// Writer: 将装配结果以流式方式持久化到目标介质（文件系统/对象存储等）。
// 约束：
//  1. 同一 ArtifactID 单写者；
//  2. 流式写入（O(1) 额外内存），按字节透传，不读取/修改业务内容；
//  3. ctx 取消/超时需尽快返回；
//  4. 错误直接上抛（不做重试/回退）。
type Writer interface {
	Write(ctx context.Context, id ArtifactID, r io.Reader) error
}
