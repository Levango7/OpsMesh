# ADR：RAG / 提示词 / 知识库三存储的持久化与跨副本一致性（issue #151）

- 日期：2026-11-25
- 状态：提议（待评审）
- 关联：#151、#149（两存储并发安全已修）、#150（预算硬限 ADR）、release-notes 的内存披露

## 背景与现状（钉在代码上）

三处 AI 存储均为 `map + sync.RWMutex + 租户前缀键` 的**进程内实现**：

- `internal/ai/gateway/rag.go`：`InMemoryVectorStore`（RAG 向量文档 + embedding + metadata，TopK 相似度检索）。
- `internal/knowledge/retrieval.go`：`InMemoryKnowledgeStore`（知识文档 + embedding + metadata + citation，ListBySource/检索）。
- `internal/ai/gateway/prompt.go`：`PromptTemplateStore`（提示词模板 CRUD）。

`docs/release-notes.md:63-66` 已如实披露：三者**不持久化、不跨副本一致、重启即丢**。这是已声明的产品能力边界，不是文档失实——本 ADR 是把该边界升级为可交付能力。

## 问题

- 重启/升级后 RAG 语料、知识库、提示词模板全部丢失，须重建。
- 多副本各自一份 ⇒ 同一租户在不同副本看到不同语料/模板，检索与回答不一致。
- 无法作为生产知识底座承载真实业务语料。

## 目标

三存储**持久化 + 跨副本一致**：写路径落 SQL、SQL 为唯一权威源、重启不丢、多副本一致，保留租户隔离与 TopK/删除/计数语义。不改变对外 API 形状与既有并发安全保证（#149 的锁 + 可变引用复制保留）。

## 关键决策：向量相似度怎么走（本 ADR 的 crux）

控制面主存储是 **MySQL**（无通用向量类型；HeatWave 向量不在当前部署口径）。RAG/知识库的 TopK 相似度检索有三条路：

- **A. 关系表存 embedding（JSON/BLOB），应用侧算余弦相似度**。语料按租户读出后在进程内算分排序。
  - 优点：零新依赖、与 MySQL 天然契合、跨副本一致由 SQL 保证、迁移简单。
  - 缺点：单租户语料须全量读入内存算分，规模受限于单租户文档数；大语料需分页/候选裁剪。
  - 适用：v1 的中小语料（单租户数千~数万 chunk 级），配合按租户/source 预过滤把候选集控制在可算范围。
- **B. pgvector（PostgreSQL）**：仓库已有 PG 方言地基（A3-1/A3-2 在途）。向量列 + `<=>` 距离索引下推。
  - 优点：相似度下推 DB、可扩展到大语料。
  - 缺点：仅 PG 部署可用，MySQL 部署退回 A；依赖 PG 方言收口进度。
- **C. 外部向量库（Qdrant/Milvus 等）**：新依赖与运维面，超出 v1 范围，不选。

**建议：v1 采用 A（关系表 + 应用侧余弦），B（pgvector）作为 PG 部署的可选加速层留作 v2。** 理由：先以最小依赖把「持久化 + 跨副本一致 + 不丢」这一核心缺口补上，且 A 对 MySQL/PG 部署通用；把相似度下推的性能优化与 PG 方言成熟度解耦，避免本 ADR 被 PG 进度阻塞。需在 ADR 中写明 A 的规模上限与候选裁剪策略，避免「全表扫」被误当成无限可扩展。

## 各存储的持久化模型

- **提示词（最简单，关系型）**：新表 `prompt_templates`（tenant_id+id 联合主键、category、content、variables JSON、metadata JSON、updated_at）。CRUD 全走 SQL；List 按 tenant+category 查询。
- **知识库**：新表 `knowledge_docs`（tenant_id+id 主键、source、content、embedding JSON、metadata JSON、citation JSON、created_at）。ListBySource/检索按 tenant(+source) 读出后应用侧算分（决策 A）。
- **RAG**：新表 `rag_documents`（tenant_id+id 主键、content、embedding JSON、metadata JSON、created_at）。TopK 按 tenant 读出候选后应用侧余弦排序（决策 A）。

均带 `tenant_id` 前缀列（延续现有租户隔离），`embedding` 以 JSON 数组存（float64 维度固定、写入校验维度一致），`metadata`/`citation` 以 JSON 存。

## 一致性模型

- **SQL 为唯一权威源**：写路径（Upsert/Delete）直接落 SQL；读路径默认读 SQL。
- **可选只读缓存**：进程内 read-through 缓存仅作性能优化，失效以写操作/短 TTL 失效；**不允许缓存成为第二权威**（避免 #150 的 RMW/双写问题）。v1 可不做缓存，直读 SQL，先保证正确。
- 多副本一致由「都读 SQL」天然获得；#149 的输入/输出可变引用复制在 SQL 边界同样保留（防 alias 回写）。

## 迁移与兼容

- 新增 3 张表（035/036/037 迁移序号顺延）；不改既有 API 形状与接口签名——`VectorStore`/`KnowledgeStore`/`PromptTemplateStore` 接口不变，新增 SQL 实现 + 按主存储类型装配（同 #150 的 server.go 选择点）。
- 进程内实现保留为 dev/单副本形态；SQL 部署切换到 SQL 实现。
- 不做旧内存数据「迁移」（内存本就重启即丢，无源可迁）；release-notes 说明升级为持久化能力。

## 失败语义

- 写路径（Upsert/Delete）：SQL 故障返回 err 并 `storefail.Record`，调用方按现有错误处理（不静默吞）。
- 检索/读路径：SQL 故障时 RAG/知识库检索返回错误（不假装「无结果」误导回答）；提示词读取失败按 fail-closed（模板不可用即报错）。
- 不引入「检索失败降级为空结果」的静默行为（会生成无依据回答）。

## 测试计划

1. 三存储 SQL 实现的 CRUD/TopK/删除/计数/ListBySource 与内存实现**同语义对照**（同一组用例双跑）。
2. 重启后数据不丢（重建 store 实例读回）。
3. 两个 store 实例（模拟两副本）写入互见、检索一致。
4. 租户隔离：跨租户读写不可见（含 SQL 层）。
5. TopK 正确性：构造已知向量，余弦排序与 TopK 截断正确；候选预过滤（tenant/source）生效。
6. 并发：`-race` 下 Upsert/Search/Delete 无数据竞争、无 alias 回写（延续 #149 判据）。
7. 失败语义：SQL 断开时读/写按上述语义返回 err 并 Record。
8. embedding 维度不一致写入被拒。

## 不做

- 不引入外部向量库；不做相似度 ANN 索引（v1 应用侧精确余弦，pgvector 加速留 v2）。
- 不做语料实时 ingestion 管道/embedding 生成（沿用现有 embed provider 接口）。
- 不做缓存一致性协议的复杂设计（v1 直读 SQL）。
