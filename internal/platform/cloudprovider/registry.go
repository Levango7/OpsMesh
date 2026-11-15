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
// 6 家内建厂商（aliyun/aws/azure/tencent/huawei/volcengine）均为真实实现：
// ValidateCredentials、ListRegions 与 Discover* 走各自厂商的 HTTP API。
func RegisterBuiltIns(f *AdapterFactory) {
	f.Register(aliyun.NewAliyunProvider())
	f.Register(aws.NewAWSProvider())
	f.Register(azure.NewAzureProvider())
	f.Register(tencent.NewTencentProvider())
	f.Register(huawei.NewHuaweiProvider())
	f.Register(volcengine.NewVolcengineProvider())
}
