// Package gates 存放跨模块的结构性门禁测试（源码形态扫描）。
//
// 本包没有运行期代码：只有测试。之所以单独成包，是因为它检查的对象在
// **别的模块**里（services/*/cmd/*/main.go，go.work 下的独立模块），
// 根模块的 build/vet/lint 与那些模块的单测都碰不到这类缺陷形态。
package gates
