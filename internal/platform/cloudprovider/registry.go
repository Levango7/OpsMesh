package cloudprovider

import (
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/aliyun"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/aws"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/azure"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/huawei"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/tencent"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/volcengine"
)

// RegisterBuiltIns 将内建所有云厂商适配器注册到给定工厂。
// aliyun 已接入真实 HTTP API 调用（ValidateCredentials/ListRegions/Discover*），
// 其余厂商提供 ValidateCredentials + ListRegions（硬编码区域列表）+ Discover*（优雅降级为空）。
func RegisterBuiltIns(f *AdapterFactory) {
	f.Register(aliyun.NewAliyunProvider())
	f.Register(aws.NewAWSProvider())
	f.Register(azure.NewAzureProvider())
	f.Register(tencent.NewTencentProvider())
	f.Register(huawei.NewHuaweiProvider())
	f.Register(volcengine.NewVolcengineProvider())
}
