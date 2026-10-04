package cmdb

import (
	"math"
	"sort"
	"strings"

	"github.com/Levango7/OpsMesh/internal/fulltext"
)

// ===========================================================================
// 全文检索
//
// 设计要点（改动本文件前请先读）：
//
//  1. 打分与判定只有一份实现——matchCI。memory 与 SQL 两个后端各自用不同方式
//     *召回候选*，但最终的"命中与否"与"排序"都调用 matchCI。只有这样才能保证
//     同一查询在两个后端得到相同的结果；否则后端切换时排序会悄悄变化，
//     而且这种差异没有任何编译期或测试期信号。
//
//  2. 召回只需是命中集合的**超集**。倒排索引的前缀展开、SQL 的 LIKE 子串匹配
//     都天然是超集，因此精确判定放心交给 matchCI。这个前提让 SQL 侧可以省掉
//     LIKE 转义（'%'、'_' 未转义只会放宽匹配），也让索引侧不必追求精确。
//
//  3. 分词复用 internal/fulltext，与日志检索共用一套规则。
// ===========================================================================

// CiSearchMode 检索模式。
type CiSearchMode string

const (
	// SearchModeAll（默认）：所有检索词都命中才返回（AND）。
	SearchModeAll CiSearchMode = "all"
	// SearchModeAny：命中任一检索词即返回（OR），按命中词数加权排序。
	SearchModeAny CiSearchMode = "any"
	// SearchModePhrase：检索词需在同一个字段内连续出现。
	SearchModePhrase CiSearchMode = "phrase"
)

// 检索参数边界。
const (
	ciSearchDefaultLimit = 50
	ciSearchMaxLimit     = 500
	// ciSearchSQLRecallCap 是 SQL 后端单次检索取回的候选上限。
	//
	// SQL 侧先按 updated_at 取回至多这么多个候选，再由 matchCI 精排；
	// 因此候选数超过此值时，真正最相关的少数 CI 可能落在窗口外。
	// 这是"零 schema 变更"约束下 LIKE 召回的固有限制——要消除它需要在
	// ci_items 上建 FULLTEXT 索引（属 migrations 变更，另行规划）。
	ciSearchSQLRecallCap = 1000
)

// CiSearchQuery CI 全文检索请求。
type CiSearchQuery struct {
	Query  string       // 检索词；空串返回空结果（不报错）
	CiType string       // 可选：按 CI 类型过滤
	Status string       // 可选：按状态过滤，空串默认 active（与列表接口口径一致）
	Limit  int          // 可选：返回条数上限，<=0 用默认值
	Mode   CiSearchMode // 可选：all / any / phrase，空串按 all 处理
}

// CiSearchHit 一条检索结果：CI 字段内联展开，额外带得分与命中的检索词。
type CiSearchHit struct {
	CiItem
	Score   float64  `json:"score"`
	Matched []string `json:"matched,omitempty"`
}

// normalizeCiSearch 归一化检索参数：分词去重、定模式、夹取 limit、补默认状态。
func normalizeCiSearch(q CiSearchQuery) (tokens []string, mode CiSearchMode, limit int, status string) {
	tokens = dedupeStrings(fulltext.Tokenize(q.Query))
	switch q.Mode {
	case SearchModeAll, SearchModeAny, SearchModePhrase:
		mode = q.Mode
	default:
		// 未知模式按 all 处理而不是报错：检索模式只是相关性偏好，
		// 拼错模式名不该让整个检索失败。
		mode = SearchModeAll
	}
	limit = q.Limit
	if limit <= 0 {
		limit = ciSearchDefaultLimit
	}
	if limit > ciSearchMaxLimit {
		limit = ciSearchMaxLimit
	}
	status = q.Status
	if status == "" {
		status = "active"
	}
	return tokens, mode, limit, status
}

// 检索字段权重：越能标识一个 CI 的字段权重越高。
const (
	ciWeightName  = 3.0 // 主机名/展示名是主要检索入口
	ciWeightAttrs = 2.0 // 属性键与值
	ciWeightType  = 1.5 // CI 类型
	ciWeightIdent = 1.0 // id / agentID / deviceID / source
)

// ciField 一个参与检索的字段：权重 + 分词结果。
type ciField struct {
	weight float64
	tokens []string
}

// ciSearchFields 把 CI 拆成带权重的检索字段。
//
// 每个字段**独立分词**，"短语必须落在同一字段内"的语义才有意义；
// 若先拼成一整篇再分词，跨字段的相邻词会被误判为短语。
func ciSearchFields(it *CiItem) []ciField {
	fields := make([]ciField, 0, 4)
	fields = append(fields, ciField{ciWeightName, fulltext.Tokenize(it.Name)})
	fields = append(fields, ciField{ciWeightType, fulltext.Tokenize(it.CiType)})
	// 属性的键与值一并参与检索（键如 ip/os，值如 10.0.0.1/CentOS）。
	var attrText strings.Builder
	for _, k := range sortedAttrKeys(it.Attrs) {
		attrText.WriteString(k)
		attrText.WriteByte(' ')
		attrText.WriteString(it.Attrs[k])
		attrText.WriteByte(' ')
	}
	fields = append(fields, ciField{ciWeightAttrs, fulltext.Tokenize(attrText.String())})
	ident := strings.Join([]string{it.ID, it.AgentID, it.DeviceID, it.Source}, " ")
	fields = append(fields, ciField{ciWeightIdent, fulltext.Tokenize(ident)})
	return fields
}

// ciSearchText 生成送入倒排索引的整篇文本。
//
// 各字段原文用空格拼接：空格在分词器中是分隔符，所以
// Tokenize(ciSearchText(it)) == 各字段分词结果的顺序拼接，
// 索引召回看到的 token 集合与 matchCI 逐字段判定看到的一致。
// 一旦这条等式被打破（例如换成逗号以外的连接符），索引就会漏召回。
func ciSearchText(it *CiItem) string {
	parts := make([]string, 0, 6+2*len(it.Attrs))
	parts = append(parts, it.Name, it.CiType)
	for _, k := range sortedAttrKeys(it.Attrs) {
		parts = append(parts, k, it.Attrs[k])
	}
	parts = append(parts, it.ID, it.AgentID, it.DeviceID, it.Source)
	return strings.Join(parts, " ")
}

// matchCI 判定 CI 是否命中检索词并返回得分（0 表示未命中）。
//
// 两个后端共用的唯一打分器，见本文件顶部设计要点 1。
//
// 匹配语义：检索词是文档词的前缀（"web" 命中 "webserver"），
// 中文按字切分后前缀匹配等价于精确匹配。
// 得分：各检索词在权重最高的命中字段上的贡献之和，词频取对数饱和
// （tf=1 → 1.0，tf=10 → 2.0），避免同一词堆砌刷分。
func matchCI(it *CiItem, tokens []string, mode CiSearchMode) (float64, []string) {
	if it == nil || len(tokens) == 0 {
		return 0, nil
	}
	fields := ciSearchFields(it)
	matched := make([]string, 0, len(tokens))
	var score float64
	for _, tok := range tokens {
		best := 0.0
		for _, f := range fields {
			tf := 0
			for _, dt := range f.tokens {
				if strings.HasPrefix(dt, tok) {
					tf++
				}
			}
			if tf == 0 {
				continue
			}
			w := f.weight * (1 + math.Log10(float64(tf)))
			if w > best {
				best = w
			}
		}
		if best > 0 {
			matched = append(matched, tok)
			score += best
		}
	}
	switch mode {
	case SearchModeAny:
		if len(matched) == 0 {
			return 0, nil
		}
		// 命中词越多越靠前（覆盖率加权）。
		score *= float64(len(matched)) / float64(len(tokens))
	case SearchModePhrase:
		if len(matched) != len(tokens) || !ciHasPhrase(fields, tokens) {
			return 0, nil
		}
	default: // SearchModeAll
		if len(matched) != len(tokens) {
			return 0, nil
		}
	}
	return score, matched
}

// ciHasPhrase 判定 tokens 是否在某个字段内连续出现（每个 token 按前缀匹配）。
func ciHasPhrase(fields []ciField, tokens []string) bool {
	for _, f := range fields {
		for i := 0; i+len(tokens) <= len(f.tokens); i++ {
			ok := true
			for j, tok := range tokens {
				if !strings.HasPrefix(f.tokens[i+j], tok) {
					ok = false
					break
				}
			}
			if ok {
				return true
			}
		}
	}
	return false
}

// ciMatchesFilter 租户/类型/状态三重过滤（空串表示不过滤该维度）。
func ciMatchesFilter(it *CiItem, tenantID, ciType, status string) bool {
	if tenantID != "" && it.TenantID != tenantID {
		return false
	}
	if ciType != "" && it.CiType != ciType {
		return false
	}
	if status != "" && it.Status != status {
		return false
	}
	return true
}

// sortCiSearchHits 按得分降序；同分按 CI ID 升序，
// 保证同一查询多次调用返回相同顺序（否则前端列表会无故跳动）。
func sortCiSearchHits(hits []CiSearchHit) {
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].ID < hits[j].ID
	})
}

// sortedAttrKeys 返回属性键的字典序切片。
// map 遍历顺序是随机的；短语检索要求"连续"，不排序会让同一条 CI 两次检索结果不同。
func sortedAttrKeys(attrs map[string]string) []string {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// dedupeStrings 去重并保持首次出现顺序。
func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
