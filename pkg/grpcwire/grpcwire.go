// Package grpcwire 提供"RPC 真的能过 gRPC 线路"的通用契约检查。
//
// 为什么存在（#59 的防复发面）：本仓 6 个微服务 api/proto 下的类型一度是**手写 Go
// struct**——没有 protoimpl、没有 ProtoReflect()，不满足 grpc 默认 proto codec 对
// proto.Message 的要求。后果不是"某个方法少了"，而是**每一条 RPC 都在编解码阶段失败**：
//
//	proto: failed to marshal, message is *alertv1.CreateRuleRequest, want proto.Message
//
// 而当时仓库里没有任何测试 dial 过这些服务（全是进程内直调 Service 方法），于是
// "CI 全绿 + 健康检查全绿 + gRPC API 100% 不可用"长期共存。
//
// 逐方法手写往返测试会随 RPC 数量线性膨胀（这 6 个服务共 109 条 unary 方法），
// 所以这里用 protobuf 反射把"每条方法的请求与响应都真过一次 codec"做成通用检查：
// 服务描述与消息类型都取自全局注册表，服务端用泛化 handler 回一个零值响应，
// 客户端用 conn.Invoke 发真请求。任何一端类型不再是 proto.Message，marshal/unmarshal
// 就会红，并且红在具体方法名上。
package grpcwire

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// bufName 必须与 dialer 里 passthrough 的目标一致。
const bufName = "grpcwire-bufnet"

// methodPlan 是一条方法的可执行检查计划（类型在测试主线程里就解析好，
// 避免 handler 里 panic 变成"服务端崩了、测试挂着不动"这种难读的红）。
type methodPlan struct {
	fullMethod string
	newRequest func() proto.Message
	newReply   func() proto.Message
}

// serveAsync 在后台跑 srv.Serve(lis)，把退出错误送进 channel。
//
// 为什么不用 `_ = srv.Serve(lis)`：本仓 errcheck 开了 check-blank，`_ =` 也要显式豁免；
// 更重要的是**服务端起不来时必须让测试红**——静默吞掉这个错误的话，客户端只会看到
// 一堆 "connection refused / Unavailable"，排查会误指向线路或生成物。
func serveAsync(srv *grpc.Server, lis net.Listener) chan error {
	done := make(chan error, 1)
	go func() { done <- srv.Serve(lis) }()
	return done
}

// reportServe 在关闭服务端之后回收退出错误：ErrServerStopped / nil 属正常收尾。
func reportServe(t *testing.T, done chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("bufconn gRPC 服务端异常退出：%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("bufconn gRPC 服务端在 2s 内没有退出，Serve 协程可能泄漏")
	}
}

// ServeBufconn 起一个进程内 bufconn 的 gRPC server 并返回与之相连的 client。
//
// register 里传生成的 RegisterXxxServer（真实现或替身都行），返回的 conn 交给生成的
// NewXxxClient 使用——这样"typed 客户端与服务端注册仍然对得上"也被走了一遍，
// 而不只是泛化 Invoke。cleanup 必须调用（关 conn 与 server）。
//
// 目标必须写 passthrough:///：默认 dns resolver 会去解析这个名字并返回零地址
// （实测 "name resolver error: produced zero addresses"，与线路无关，容易误判成故障）。
func ServeBufconn(t *testing.T, register func(srv *grpc.Server)) (*grpc.ClientConn, func()) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	register(srv)
	done := serveAsync(srv, lis)

	conn, err := grpc.NewClient("passthrough:///"+bufName,
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		srv.Stop()
		reportServe(t, done)
		t.Fatalf("NewClient: %v", err)
	}
	return conn, func() {
		_ = conn.Close()
		srv.Stop()
		reportServe(t, done)
	}
}

// AssertUnaryMethodsCrossCodec 对给定 protobuf 包（如 "opsmesh.device.v1"）里的
// **每一条 unary 方法**做一次真实的"gRPC 请求→编解码→服务端→编解码→响应"往返。
//
// 失败即红的情形（正是 #59 的形态）：
//   - 请求或响应类型不实现 proto.Message（手写 struct 回归、生成物被手写版覆盖）；
//   - 消息类型没进全局 protobuf 注册表；
//   - 服务或方法名与生成物不一致（路径两侧都由描述符拼出，因此这条同时钉住同源）。
//
// 返回值是**实际检查过的方法条数**：调用方必须拿它与 .proto 里的 rpc 条数做断言。
// 没有这条返回值，"包名写错 / 注册表为空"这类覆盖塌方只会留下一个安静的绿。
func AssertUnaryMethodsCrossCodec(t *testing.T, pkgs ...string) int {
	t.Helper()

	services := collectServices(t, pkgs)
	if len(services) == 0 {
		t.Fatalf("包 %v 下没找到任何 service（生成物未注册到全局？或包名写错）", pkgs)
	}

	var plans []methodPlan
	byService := map[string][]methodPlan{}
	for _, sd := range services {
		methods := sd.Methods()
		for i := 0; i < methods.Len(); i++ {
			md := methods.Get(i)
			if md.IsStreamingClient() || md.IsStreamingServer() {
				// 本仓 6 个服务目前全为 unary。出现流式时必须扩这里——
				// 静默跳过会让"覆盖全部方法"这句话变成谎。
				t.Fatalf("%s/%s 是流式方法，本检查未覆盖，请扩实现而不是跳过", sd.FullName(), md.Name())
			}
			input, output := md.Input(), md.Output()
			p := methodPlan{
				fullMethod: "/" + string(sd.FullName()) + "/" + string(md.Name()),
				newRequest: func() proto.Message { return newMessageOf(t, input) },
				newReply:   func() proto.Message { return newMessageOf(t, output) },
			}
			plans = append(plans, p)
			byService[string(sd.FullName())] = append(byService[string(sd.FullName())], p)
		}
	}

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	for _, sd := range services {
		srv.RegisterService(buildServiceDesc(t, sd), struct{}{})
	}
	done := serveAsync(srv, lis)
	defer func() {
		srv.Stop()
		reportServe(t, done)
	}()

	// 必须 passthrough：默认 dns resolver 会去解析这个名字并返回零地址
	// （实测 "name resolver error: produced zero addresses"，与线路无关，容易误判）。
	conn, err := grpc.NewClient("passthrough:///"+bufName,
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var ok int
	for _, p := range plans {
		req, reply := p.newRequest(), p.newReply()
		if err := conn.Invoke(ctx, p.fullMethod, req, reply); err != nil {
			t.Errorf("Invoke %s 失败（这就是 #59 的成因形态）：%v", p.fullMethod, err)
			continue
		}
		ok++
	}
	t.Logf("gRPC codec 往返通过 %d/%d 条方法，覆盖 %d 个 service", ok, len(plans), len(services))
	if ok != len(plans) {
		t.Fatalf("只通过 %d/%d——上面的逐条错误已给出红在哪个方法", ok, len(plans))
	}
	return ok
}

// collectServices 从全局 protobuf 文件注册表挑出指定包的所有 service。
func collectServices(t *testing.T, pkgs []string) []protoreflect.ServiceDescriptor {
	t.Helper()
	want := make(map[string]bool, len(pkgs))
	for _, p := range pkgs {
		want[p] = true
	}
	var out []protoreflect.ServiceDescriptor
	// RangeFiles 不返回 error（它靠回调返回值决定是否继续遍历）。
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !want[string(fd.Package())] {
			return true
		}
		ss := fd.Services()
		for i := 0; i < ss.Len(); i++ {
			out = append(out, ss.Get(i))
		}
		return true
	})
	return out
}

// mtOf 取消息描述符对应的 protobuf 类型（不依赖 *testing.T 的版本，供 handler 复用）。
func mtOf(t *testing.T, md protoreflect.MessageDescriptor) protoreflect.MessageType {
	t.Helper()
	mt, err := protoregistry.GlobalTypes.FindMessageByName(md.FullName())
	if err != nil {
		t.Fatalf("消息类型 %s 不在 protobuf 注册表中：%v", md.FullName(), err)
	}
	return mt
}

// newMessageOf 按消息描述符构造对应具体类型。
// 这一步本身就是断言：类型没登记进 protobuf 注册表（手写 struct 的典型症状）即 fatal。
func newMessageOf(t *testing.T, md protoreflect.MessageDescriptor) proto.Message {
	t.Helper()
	return mtOf(t, md).New().Interface()
}

// buildServiceDesc 把 protobuf 服务描述符翻译成 grpc.ServiceDesc。
//
// HandlerType 用 (*any)(nil)：grpc 只检查注册对象是否实现该接口，空接口人人满足
// （注册时传 struct{}{}）。handler 固定"解出请求 → 回一个零值响应"，于是请求与响应
// 两侧的 codec 都被真实走一遍——这正是我们要的证据，业务语义另有测试管。
func buildServiceDesc(t *testing.T, sd protoreflect.ServiceDescriptor) *grpc.ServiceDesc {
	t.Helper()
	desc := &grpc.ServiceDesc{
		ServiceName: string(sd.FullName()),
		HandlerType: (*any)(nil),
	}
	methods := sd.Methods()
	for i := 0; i < methods.Len(); i++ {
		md := methods.Get(i)
		input, output := md.Input(), md.Output()
		// 服务端侧类型也在此刻解析：红要红在测试主线程，别红成服务端 panic。
		inProbe, outProbe := mtOf(t, input), mtOf(t, output)
		full := "/" + string(sd.FullName()) + "/" + string(md.Name())
		desc.Methods = append(desc.Methods, grpc.MethodDesc{
			MethodName: string(md.Name()),
			Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
				// 请求必须能解进生成类型——解不动就报错在这里，而不是等到业务逻辑。
				req := inProbe.New().Interface()
				if err := dec(req); err != nil {
					return nil, fmt.Errorf("decode %s: %w", full, err)
				}
				return outProbe.New().Interface(), nil
			},
		})
	}
	return desc
}

// 工具链版本钉。改任何一项都必须：① 用新版本重新生成**全部 6 个服务**的 pb 文件，
// ② 跑完各服务的 wire_test，③ 同步这里的常量。
//
// 为什么要钉版本而不是只查"是不是生成物"：protoc-gen-go 的小版本会改生成代码的
// 结构（descriptor 布局、getter 形态），"手写文件"与"不同版本生成的文件"都能通过
// 只看首行的检查。本仓的 golangci-lint 也踩过同类坑（action 用 latest 导致本机绿、CI 红）。
const (
	WantProtocGenGo      = "protoc-gen-go v1.36.12"
	WantProtocGenGoGRPC  = "protoc-gen-go-grpc v1.6.2"
	WantProtocAnnotation = "protoc-gen-go" // 消息文件
)

// AssertGeneratedFilesLookGenerated 是第二道闸：确认这些 .pb.go 真是 protoc 产物，
// 且由**钉住的那套工具链**产出。
//
// 手写 struct 时代同名文件也存在，光看文件名判不出来；但 protoc-gen-go 必然在文件头
// 写下生成标记与版本号。codec 兼容性交给上面的往返测试，这里只查出处——一条断言只管一件事。
func AssertGeneratedFilesLookGenerated(t *testing.T, files ...string) {
	t.Helper()
	if len(files) == 0 {
		t.Fatal("没有传入任何文件路径——覆盖为空的绿等于没测")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Errorf("读 %s: %v", f, err)
			continue
		}
		head := string(b)
		if len(head) > 400 {
			head = head[:400]
		}
		if !strings.HasPrefix(head, "// Code generated by protoc") {
			t.Errorf("%s 不是 protoc 生成物（首行：%q）——手写类型会让每条 RPC 都在编解码阶段失败", f, firstLine(head))
			continue
		}
		want := WantProtocGenGo
		if strings.HasSuffix(f, "_grpc.pb.go") {
			want = WantProtocGenGoGRPC
		}
		if !strings.Contains(head, want) {
			t.Errorf("%s 不是钉住的工具链产出（应含 %q，文件头：%q）——"+
				"要换工具链版本请同时重新生成全部 6 个服务并更新 grpcwire 的常量", f, want, firstLine(head))
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
