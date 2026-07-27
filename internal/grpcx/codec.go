// Package grpcx 提供「自定义 JSON codec + 手写 grpc.ServiceDesc」的真实 gRPC 传输层。
// U-05: 在 9090 上承载 agent↔控制面 的注册/心跳/拉任务/上报结果四条通道，
// 不依赖 protobuf 代码生成（无 protoc），仅依赖 google.golang.org/grpc 稳定 API。
package grpcx

import (
	"encoding/json"

	"google.golang.org/grpc/encoding"
)

// CodecName 是我们在 encoding 注册表里登记的 codec 名字。
// 客户端用 ForceCodec 强制 content-subtype=json，服务端据此查找本 codec 解码。
const CodecName = "json"

// JSONCodec 导出的 jsonCodec 实例，供 agent 侧 grpc.ForceCodec(grpcx.JSONCodec) 使用。
var JSONCodec encoding.Codec = jsonCodec{}

// init 在包加载时把 json codec 注册到 grpc 的全局编码表，名字为 "json"。
func init() {
	encoding.RegisterCodec(jsonCodec{})
}

// jsonCodec 用标准库 encoding/json 序列化任意结构体，作为 gRPC 的 payload 编解码器。
type jsonCodec struct{}

func (jsonCodec) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func (jsonCodec) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

func (jsonCodec) Name() string {
	return CodecName
}
