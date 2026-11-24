package governance

import (
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Row 一行数据，键为列名。
type Row map[string]interface{}

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]{0,127}$`)

// ValidateIdentifier 校验 SQL 标识符（表/列/库表限定名）。
func ValidateIdentifier(name string) error {
	if !identifierPattern.MatchString(name) {
		return fmt.Errorf("%w: invalid identifier %q", ErrInvalid, name)
	}
	return nil
}

// EvaluateRule 使用当前时间评估规则。
func EvaluateRule(rule DataRule, rows []Row) (RuleResult, error) {
	return EvaluateRuleAt(rule, rows, time.Now().UTC())
}

// EvaluateRuleAt 在给定时间点评估规则，纯函数（不读取系统时钟）。
//
// 各类型语义：
//   - null: 统计空值（缺失/nil/空串）数量，默认允许 0；Expr 可写 maxNulls=N。
//   - unique: 统计非空重复值数量，必须为 0。
//   - range: 数值越界（含非数值）数量必须为 0；Expr "min=..,max=.." 或 "min,max"。
//   - regex: 非空值不匹配数量必须为 0；Expr 为正则。
//   - freshness: 时间列最大年龄不超过 maxAgeHours；空/非法时间计为违规。
//   - rowcount: 行数在 [min,max] 内；Expr "min=..,max=.." 或 "min,max"。
func EvaluateRuleAt(rule DataRule, rows []Row, now time.Time) (RuleResult, error) {
	result := RuleResult{
		RuleID:   rule.ID,
		TenantID: rule.TenantID,
		RunAt:    now.UTC(),
	}
	kind := strings.ToLower(strings.TrimSpace(rule.Kind))
	switch kind {
	case RuleKindNull:
		column, err := requireColumn(rule.Column)
		if err != nil {
			return RuleResult{}, err
		}
		maxNulls, err := parseIntExpr(rule.Expr, []string{"maxNulls", "max"}, 0)
		if err != nil {
			return RuleResult{}, err
		}
		if maxNulls < 0 {
			return RuleResult{}, fmt.Errorf("%w: maxNulls must be >= 0", ErrInvalid)
		}
		count := 0
		for _, row := range rows {
			if isBlank(row[column]) {
				count++
			}
		}
		result.Observed = fmt.Sprintf("nulls=%d", count)
		result.Passed = count <= maxNulls
		if !result.Passed {
			result.Detail = fmt.Sprintf("null values %d exceed allowed %d", count, maxNulls)
		}
	case RuleKindUnique:
		column, err := requireColumn(rule.Column)
		if err != nil {
			return RuleResult{}, err
		}
		seen := make(map[string]struct{}, len(rows))
		duplicates := 0
		var sample string
		for _, row := range rows {
			if isBlank(row[column]) {
				continue
			}
			key := fmt.Sprint(normalizeValue(row[column]))
			if _, ok := seen[key]; ok {
				duplicates++
				if sample == "" {
					sample = key
				}
				continue
			}
			seen[key] = struct{}{}
		}
		result.Observed = fmt.Sprintf("duplicates=%d", duplicates)
		result.Passed = duplicates == 0
		if !result.Passed {
			result.Detail = fmt.Sprintf("duplicate values=%d sample=%s", duplicates, sample)
		}
	case RuleKindRange:
		column, err := requireColumn(rule.Column)
		if err != nil {
			return RuleResult{}, err
		}
		min, max, hasMin, hasMax, err := parseRangeExpr(rule.Expr)
		if err != nil {
			return RuleResult{}, err
		}
		if !hasMin && !hasMax {
			return RuleResult{}, fmt.Errorf("%w: range requires min or max", ErrInvalid)
		}
		violations := 0
		var sample string
		for _, row := range rows {
			if isBlank(row[column]) {
				continue
			}
			v, ok := toFloat(row[column])
			if !ok || (hasMin && v < min) || (hasMax && v > max) {
				violations++
				if sample == "" {
					sample = fmt.Sprint(row[column])
				}
			}
		}
		result.Observed = fmt.Sprintf("out_of_range=%d", violations)
		result.Passed = violations == 0
		if !result.Passed {
			result.Detail = fmt.Sprintf("out-of-range values=%d sample=%s", violations, sample)
		}
	case RuleKindRegex:
		column, err := requireColumn(rule.Column)
		if err != nil {
			return RuleResult{}, err
		}
		pattern := strings.TrimSpace(rule.Expr)
		if pattern == "" {
			return RuleResult{}, fmt.Errorf("%w: regex requires expr", ErrInvalid)
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return RuleResult{}, fmt.Errorf("%w: invalid regex: %v", ErrInvalid, err)
		}
		violations := 0
		var sample string
		for _, row := range rows {
			if isBlank(row[column]) {
				continue
			}
			value := fmt.Sprint(normalizeValue(row[column]))
			if !re.MatchString(value) {
				violations++
				if sample == "" {
					sample = value
				}
			}
		}
		result.Observed = fmt.Sprintf("non_matching=%d", violations)
		result.Passed = violations == 0
		if !result.Passed {
			result.Detail = fmt.Sprintf("non-matching values=%d sample=%s", violations, sample)
		}
	case RuleKindFreshness:
		column, err := requireColumn(rule.Column)
		if err != nil {
			return RuleResult{}, err
		}
		maxAgeHours, err := parseFloatExpr(rule.Expr, []string{"maxAgeHours", "max"}, -1)
		if err != nil {
			return RuleResult{}, err
		}
		if maxAgeHours <= 0 {
			return RuleResult{}, fmt.Errorf("%w: freshness requires maxAgeHours > 0", ErrInvalid)
		}
		violations := 0
		maxAge := 0.0
		var sample string
		for _, row := range rows {
			ts, ok := toTime(row[column])
			if !ok {
				violations++
				if sample == "" {
					sample = fmt.Sprint(row[column])
				}
				continue
			}
			age := now.Sub(ts).Hours()
			if age < 0 {
				age = 0
			}
			if age > maxAge {
				maxAge = age
			}
			if age > maxAgeHours {
				violations++
				if sample == "" {
					sample = ts.UTC().Format(time.RFC3339)
				}
			}
		}
		result.Observed = fmt.Sprintf("max_age_hours=%.2f,violations=%d", maxAge, violations)
		result.Passed = violations == 0
		if !result.Passed {
			result.Detail = fmt.Sprintf("stale values=%d allowed_hours=%.2f sample=%s", violations, maxAgeHours, sample)
		}
	case RuleKindRowCount:
		min, max, hasMin, hasMax, err := parseRangeExpr(rule.Expr)
		if err != nil {
			return RuleResult{}, err
		}
		if !hasMin && !hasMax {
			return RuleResult{}, fmt.Errorf("%w: rowcount requires min or max", ErrInvalid)
		}
		count := len(rows)
		result.Observed = fmt.Sprintf("rows=%d", count)
		result.Passed = (!hasMin || float64(count) >= min) && (!hasMax || float64(count) <= max)
		if !result.Passed {
			result.Detail = fmt.Sprintf("row count %d outside [%s,%s]", count, boundText(min, hasMin), boundText(max, hasMax))
		}
	default:
		return RuleResult{}, fmt.Errorf("%w: unsupported rule kind %q", ErrInvalid, rule.Kind)
	}
	return result, nil
}

// SuggestMask 按 PII 类型生成脱敏建议值。
func SuggestMask(kind, value string) string {
	if value == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case PIIKindEmail:
		if at := strings.LastIndex(value, "@"); at > 0 && at < len(value)-1 {
			return value[:1] + "***" + value[at:]
		}
		return "***"
	case PIIKindPhone:
		digits := make([]rune, 0, len(value))
		for _, r := range value {
			if r >= '0' && r <= '9' {
				digits = append(digits, r)
			}
		}
		if len(digits) >= 7 {
			return string(digits[:3]) + "****" + string(digits[len(digits)-4:])
		}
		return maskAll(value)
	case PIIKindIDCard:
		runes := []rune(value)
		if len(runes) > 10 {
			return string(runes[:6]) + strings.Repeat("*", len(runes)-10) + string(runes[len(runes)-4:])
		}
		return maskAll(value)
	case PIIKindName:
		runes := []rune(value)
		if len(runes) <= 1 {
			return "*"
		}
		return string(runes[:1]) + "**"
	case PIIKindCustom:
		runes := []rune(value)
		if len(runes) == 0 {
			return ""
		}
		return string(runes[:1]) + "***"
	default:
		return maskAll(value)
	}
}

// BuildRetentionSQL 生成保留策略 SQL（MySQL 语法）。
//
// dataset 为普通表或 schema.table；anonymize 时 dataset 必须是 table.column，
// 对指定列写入 '***'。所有标识符经 ValidateIdentifier 校验。
func BuildRetentionSQL(policy RetentionPolicy) (string, error) {
	dataset := strings.TrimSpace(policy.Dataset)
	if err := ValidateIdentifier(dataset); err != nil {
		return "", err
	}
	if policy.RetentionDays <= 0 {
		return "", fmt.Errorf("%w: retentionDays must be > 0", ErrInvalid)
	}
	cutoff := fmt.Sprintf("NOW() - INTERVAL %d DAY", policy.RetentionDays)
	switch strings.ToLower(strings.TrimSpace(policy.Action)) {
	case RetentionActionDelete:
		return fmt.Sprintf("DELETE FROM %s WHERE created_at < %s;", dataset, cutoff), nil
	case RetentionActionArchive:
		archive := dataset + "_archive"
		if err := ValidateIdentifier(archive); err != nil {
			return "", err
		}
		return fmt.Sprintf("INSERT INTO %s SELECT * FROM %s WHERE created_at < %s;", archive, dataset, cutoff), nil
	case RetentionActionAnonymize:
		dot := strings.LastIndex(dataset, ".")
		if dot <= 0 || dot >= len(dataset)-1 {
			return "", fmt.Errorf("%w: anonymize dataset must be table.column", ErrInvalid)
		}
		table, column := dataset[:dot], dataset[dot+1:]
		if err := ValidateIdentifier(table); err != nil {
			return "", err
		}
		if err := ValidateIdentifier(column); err != nil {
			return "", err
		}
		return fmt.Sprintf("UPDATE %s SET %s = '***' WHERE created_at < %s;", table, column, cutoff), nil
	default:
		return "", fmt.Errorf("%w: unsupported retention action %q", ErrInvalid, policy.Action)
	}
}

func requireColumn(column string) (string, error) {
	column = strings.TrimSpace(column)
	if column == "" {
		return "", fmt.Errorf("%w: column is required", ErrInvalid)
	}
	return column, nil
}

func isBlank(v interface{}) bool {
	if v == nil {
		return true
	}
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value) == ""
	case []byte:
		return len(value) == 0
	}
	return false
}

func normalizeValue(v interface{}) interface{} {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	}
	return v
}

func toFloat(v interface{}) (float64, bool) {
	switch value := v.(type) {
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, false
		}
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case int32:
		return float64(value), true
	case uint64:
		return float64(value), true
	case uint:
		return float64(value), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	case []byte:
		f, err := strconv.ParseFloat(strings.TrimSpace(string(value)), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

func toTime(v interface{}) (time.Time, bool) {
	switch value := v.(type) {
	case time.Time:
		return value.UTC(), true
	case string:
		return parseTimeString(value)
	case []byte:
		return parseTimeString(string(value))
	case int64:
		return unixAuto(value), true
	case int:
		return unixAuto(int64(value)), true
	case float64:
		return unixAuto(int64(value)), true
	}
	return time.Time{}, false
}

func parseTimeString(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts.UTC(), true
	}
	if ts, err := time.Parse("2006-01-02 15:04:05", value); err == nil {
		return ts.UTC(), true
	}
	if ts, err := time.Parse("2006-01-02", value); err == nil {
		return ts.UTC(), true
	}
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return unixAuto(n), true
	}
	return time.Time{}, false
}

func unixAuto(n int64) time.Time {
	if n > 1_000_000_000_000 {
		return time.UnixMilli(n).UTC()
	}
	return time.Unix(n, 0).UTC()
}

func parseRangeExpr(expr string) (min, max float64, hasMin, hasMax bool, err error) {
	raw := strings.TrimSpace(expr)
	if raw == "" {
		return 0, 0, false, false, nil
	}
	if strings.Contains(raw, "=") {
		for _, part := range strings.Split(raw, ",") {
			pair := strings.SplitN(part, "=", 2)
			if len(pair) != 2 {
				return 0, 0, false, false, fmt.Errorf("%w: invalid range expr %q", ErrInvalid, expr)
			}
			key := strings.ToLower(strings.TrimSpace(pair[0]))
			value := strings.TrimSpace(pair[1])
			if value == "" {
				continue
			}
			f, convErr := strconv.ParseFloat(value, 64)
			if convErr != nil {
				return 0, 0, false, false, fmt.Errorf("%w: invalid range value %q", ErrInvalid, value)
			}
			switch key {
			case "min":
				min, hasMin = f, true
			case "max":
				max, hasMax = f, true
			default:
				return 0, 0, false, false, fmt.Errorf("%w: unknown range key %q", ErrInvalid, key)
			}
		}
	} else {
		parts := strings.Split(raw, ",")
		if len(parts) != 2 {
			return 0, 0, false, false, fmt.Errorf("%w: invalid range expr %q", ErrInvalid, expr)
		}
		if value := strings.TrimSpace(parts[0]); value != "" {
			f, convErr := strconv.ParseFloat(value, 64)
			if convErr != nil {
				return 0, 0, false, false, fmt.Errorf("%w: invalid min %q", ErrInvalid, value)
			}
			min, hasMin = f, true
		}
		if value := strings.TrimSpace(parts[1]); value != "" {
			f, convErr := strconv.ParseFloat(value, 64)
			if convErr != nil {
				return 0, 0, false, false, fmt.Errorf("%w: invalid max %q", ErrInvalid, value)
			}
			max, hasMax = f, true
		}
	}
	if hasMin && hasMax && min > max {
		return 0, 0, false, false, fmt.Errorf("%w: min > max", ErrInvalid)
	}
	return min, max, hasMin, hasMax, nil
}

func parseIntExpr(expr string, keys []string, fallback int) (int, error) {
	raw := strings.TrimSpace(expr)
	if raw == "" {
		return fallback, nil
	}
	if strings.Contains(raw, "=") {
		pair := strings.SplitN(raw, "=", 2)
		key := strings.ToLower(strings.TrimSpace(pair[0]))
		if !containsString(keys, key) {
			return 0, fmt.Errorf("%w: unknown key %q", ErrInvalid, key)
		}
		raw = strings.TrimSpace(pair[1])
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid integer expr %q", ErrInvalid, expr)
	}
	return n, nil
}

func parseFloatExpr(expr string, keys []string, fallback float64) (float64, error) {
	raw := strings.TrimSpace(expr)
	if raw == "" {
		return fallback, nil
	}
	if strings.Contains(raw, "=") {
		pair := strings.SplitN(raw, "=", 2)
		key := strings.ToLower(strings.TrimSpace(pair[0]))
		if !containsString(keys, key) {
			return 0, fmt.Errorf("%w: unknown key %q", ErrInvalid, key)
		}
		raw = strings.TrimSpace(pair[1])
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid number expr %q", ErrInvalid, expr)
	}
	return f, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func boundText(v float64, present bool) string {
	if !present {
		return "*"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func maskAll(value string) string {
	count := utf8.RuneCountInString(value)
	if count <= 0 {
		return ""
	}
	return strings.Repeat("*", count)
}

var _ = http.StatusOK
