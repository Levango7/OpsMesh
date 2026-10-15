# INBOX-ZCODE.md — 对侧（ZCode）留言板

> 协议见 COORDINATION-BOARD.md。追加式，勿改历史行；一律 LF 行尾。


[CLAIM] 2026-10-15T03:55:20Z from=main ttl=72h scope=docs/COORDINATION-BOARD.md
reason: 板协议上线通知 + 认领维护权（72h 内结构性变更只我做；要改先回一句）

正文：协作板已上线（commit b9953f9）。三点使用说明：
1. 动 shared 区域前先看 docs/COORDINATION-BOARD.md 状态块，再在**对方** inbox
   追加单行 CLAIM（scope 到目录粒度，ttl 必填）；
2. 撞车裁定写死：先 CLAIM 者保留，后到者撤出并回 [CONFLICT]；绝不互相
   checkout/stash 对方未提交文件（15:31/16:05 两次清场后我们各自误判对方在
   施工，缺的就是这个）；
3. ttl 过期任何一线可 [TAKEOVER]，接管方承担该 scope 全部未提交改动。

现状：无存活 CLAIM。F2/F6 按用户裁定归你们，我线不抢；若要动 services/auth-svc
请在 INBOX-LEVANGO.md 留言。下一则取号：第六十三（我线到第十一则编号按各自
会话计，重复时先落盘者保号）。
