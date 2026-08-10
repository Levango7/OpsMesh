// Package grpcx 提供「自定义 JSON codec + 手写 grpc.ServiceDesc」的真实 gRPC 传输层。
// U-05: 在 9090 上承载 agent↔控制面 的注册/心跳/拉任务/上报结果四条通道，
// 不依赖 protobuf 代码生成（无 protoc），仅依赖 google.golang.org/grpc 稳定 API。
package grpcx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"google.golang.org/grpc/encoding"
)

// CodecName 是我们在 encoding 注册表里登记的 codec 名字。
// 客户端用 ForceCodec 强制 content-subtype=json，服务端据此查找本 codec 解码。
const CodecName = "json"

// CodecVersion 是 JSON codec 的协议版本号，注入到每条报文的 __v 元字段。
//
// JSON codec 为正式契约，版本协商字段 __v=1：
//   - Marshal 在每条 JSON object 报文中注入 "__v":CodecVersion 元字段；
//   - Unmarshal 校验 __v 字段存在且等于 CodecVersion，缺失或不匹配返回 error；
//   - 从而在 codec 层强制版本协商，避免静默兼容旧报文，使契约变更可被对端立即感知。
const CodecVersion = 1

// versionField 是注入到 JSON 报文中的版本协商字段名（双下划线前缀避免与业务字段冲突）。
const versionField = "__v"

// ErrCodecVersionMissing 表示 Unmarshal 时未找到 __v 字段。
var ErrCodecVersionMissing = errors.New("grpcx: codec version field __v missing")

// ErrCodecVersionMismatch 表示 __v 字段值与 CodecVersion 不匹配。
var ErrCodecVersionMismatch = errors.New("grpcx: codec version mismatch")

// JSONCodec 导出的 jsonCodec 实例，供 agent 侧 grpc.ForceCodec(grpcx.JSONCodec) 使用。
var JSONCodec encoding.Codec = jsonCodec{}

// init 在包加载时把 json codec 注册到 grpc 的全局编码表，名字为 "json"。
func init() {
	encoding.RegisterCodec(jsonCodec{})
}

// jsonCodec 用标准库 encoding/json 序列化任意结构体，作为 gRPC 的 payload 编解码器。
//
// JSON codec 为正式契约，版本协商字段 __v=1：
//   - Marshal：先 json.Marshal(v)，再在结果 JSON object 中注入 "__v":1 元字段；
//   - Unmarshal：先校验 __v 字段存在且等于 CodecVersion，再 json.Unmarshal 到目标。
type jsonCodec struct{}

func (jsonCodec) Marshal(v interface{}) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return injectVersion(raw)
}

func (jsonCodec) Unmarshal(data []byte, v interface{}) error {
	if err := checkVersion(data); err != nil {
		return err
	}
	// 业务结构体不应声明 __v 字段；encoding/json 默认忽略未知字段，故直接 Unmarshal 即可。
	return json.Unmarshal(data, v)
}

func (jsonCodec) Name() string {
	return CodecName
}

// injectVersion 在已序列化的 JSON 字节流中注入 "__v":CodecVersion 元字段。
//
// 处理规则：
//   - "null"（nil 指针 / 零值指针 struct）→ {"__v":1}
//   - "{}"（空 struct）→ {"__v":1}
//   - "{...}"（非空 object）→ {"__v":1,...}
//   - 非 object（数组/字符串/数字/布尔）：返回原值，由 Unmarshal 侧 checkVersion 拒绝。
//
// 注入位置固定在 '{' 之后第一个字段，保证字段顺序确定，便于 golden 报文比对。
func injectVersion(raw []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("grpcx: marshal produced empty bytes")
	}
	// json.Marshal(零值指针) 产出 "null"；视为空对象。
	if string(trimmed) == "null" {
		return []byte(fmt.Sprintf(`{"%s":%d}`, versionField, CodecVersion)), nil
	}
	if trimmed[0] != '{' {
		// 非 object：codec 协议仅支持 object 形态报文；返回原值，Unmarshal 侧 checkVersion 会拒绝。
		return raw, nil
	}
	braceIdx := bytes.IndexByte(raw, '{')
	if braceIdx < 0 {
		return nil, errors.New("grpcx: malformed object: missing '{'")
	}
	rest := raw[braceIdx+1:]
	restTrimmed := bytes.TrimSpace(rest)
	sep := []byte(",")
	// 空对象 {}：rest 以 '}' 开头，不插入逗号。
	if len(restTrimmed) == 0 || restTrimmed[0] == '}' {
		sep = nil
	}
	out := make([]byte, 0, len(raw)+16)
	out = append(out, raw[:braceIdx+1]...) // include '{'
	out = append(out, '"')
	out = append(out, versionField...)
	out = append(out, '"', ':')
	out = append(out, []byte(fmt.Sprintf("%d", CodecVersion))...)
	out = append(out, sep...)
	out = append(out, rest...)
	return out, nil
}

// checkVersion 校验 data 中的 __v 字段存在且等于 CodecVersion。
//
// 仅对 object 形态的 JSON 报文做校验；非 object 报文直接拒绝（codec 协议违规）。
func checkVersion(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return ErrCodecVersionMissing
	}
	if trimmed[0] != '{' {
		return fmt.Errorf("grpcx: codec version check failed: expected json object, got %q", trimmed[0])
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("grpcx: codec version check failed: %w", err)
	}
	raw, ok := probe[versionField]
	if !ok {
		return ErrCodecVersionMissing
	}
	var got int
	if err := json.Unmarshal(raw, &got); err != nil {
		return fmt.Errorf("grpcx: codec version field __v not int: %w", err)
	}
	if got != CodecVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrCodecVersionMismatch, got, CodecVersion)
	}
	return nil
}
