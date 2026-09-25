package grpcx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// gRPC agent 身份绑定的 metadata 键（小写，按 gRPC 约定）。
// 定义在 grpcx 包而非控制面：agent 侧（internal/agent）与控制面侧（internal/controlplane/grpc）
// 必须使用同一组键名与同一套签名算法，否则验签必然失败。集中于此避免两端漂移。
const (
	// AgentSignatureMetadataKey ：HMAC 签名（hex）所在 metadata 键。
	AgentSignatureMetadataKey = "agent-signature"
	// AgentTimestampMetadataKey ：签名生成时刻（Unix 秒，十进制字符串）所在 metadata 键。
	AgentTimestampMetadataKey = "agent-timestamp"
	// AgentSignatureAlgMetadataKey ：签名算法版本所在 metadata 键（v1|v2）。
	// 缺失时按 v1 处理——兼容未升级的存量 agent（控制面先升级不破网）。
	AgentSignatureAlgMetadataKey = "agent-signature-alg"
)

// 签名算法版本。身份绑定强度递增，新 agent 一律用 v2。
const (
	// AgentSignatureAlgV1 旧算法：HMAC-SHA256(secret, timestamp+identity)。
	// 不覆盖载荷：同一 agent 在同一秒内的任意请求可被中间人改写业务字段后原样重放
	// （签名仍有效）。仅为兼容存量 agent 保留，控制面按限次 WARN 告警。
	AgentSignatureAlgV1 = "v1"
	// AgentSignatureAlgV2 新算法：HMAC-SHA256(secret, "v2\n"+timestamp+"\n"+identity+"\n"+payloadDigest)。
	// 覆盖载荷摘要：任务结果 / 日志 / 心跳指标等任一字段被篡改都会导致验签失败。
	AgentSignatureAlgV2 = "v2"
)

// PayloadDigest 计算报文的载荷摘要：hex(sha256(<JSON codec 序列化后的报文>))。
//
// 与线上字节完全一致：gRPC 传输即 codec.Marshal 的输出（含 __v 版本字段），
// 故 agent 端对发送对象、控制面端对解码后的对象独立计算结果必然相等
// （这些报文结构体均为 JSON 可逆：无 omitempty 字段丢失、map 键有序）。
// Marshal 失败返回空串；调用方须把空串视为「不可验签」而非「匹配」。
func PayloadDigest(msg any) string {
	raw, err := jsonCodec{}.Marshal(msg)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ComputeAgentSignatureV1 计算旧算法签名（不覆盖载荷），与存量 agent 实现逐字节一致。
func ComputeAgentSignatureV1(secret, timestamp, identity string) string {
	return hmacHex(secret, timestamp+identity)
}

// ComputeAgentSignatureV2 计算新算法签名，覆盖 timestamp / 身份 / 载荷摘要。
// 三段以 "\n" 分隔：timestamp 为十进制、identity 为 agentID（不含换行）、
// digest 为 hex，均无 "\n"，故拼接无歧义（不可把边界挪到另一段）。
func ComputeAgentSignatureV2(secret, timestamp, identity, payloadDigest string) string {
	return hmacHex(secret, AgentSignatureAlgV2+"\n"+timestamp+"\n"+identity+"\n"+payloadDigest)
}

// hmacHex 返回 HMAC-SHA256(secret, msg) 的 hex 编码。
func hmacHex(secret, msg string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}
