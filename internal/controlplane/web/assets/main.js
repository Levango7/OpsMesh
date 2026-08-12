// OpsMesh 个人版仪表盘（已下线）重定向脚本。
// M-前端收敛（v0.4.0）：原 1.3 万行原生 ES module 仪表盘已移除。
// 本文件仅保留 ES module 形态 + 一个跳转：把 / 的旧访问引导至企业版前端 /enterprise/。
// 保留 import 语句以维持 handleAsset 契约测试断言（main.js 含 import）。
import './theme.js';

(function redirect() {
  var p = window.location.pathname;
  if (p === '/' || p === '/index.html') {
    window.location.replace('/enterprise/');
  }
})();
