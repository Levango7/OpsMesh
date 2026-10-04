package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	alertv1 "github.com/Levango7/OpsMesh/services/alert-svc/api/proto/v1"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/engine"
	"github.com/Levango7/OpsMesh/services/alert-svc/internal/store"
)

func newTestService() *Service {
	eng := engine.NewEngine(nil)
	st := store.NewMemoryStore()
	return NewService(eng, st)
}

func TestCreateRule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	req := &alertv1.CreateRuleRequest{
		Rule: &alertv1.AlertRule{
			Name:      "High CPU",
			TenantId:  "tenant-1",
			Metric:    "cpu_usage",
			Op:        ">",
			Threshold: 80.0,
			Duration:  300,
			Severity:  "critical",
			Enabled:   true,
		},
	}

	rule, err := svc.CreateRule(ctx, req)
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	if rule.Id == "" {
		t.Error("expected rule ID to be set")
	}
	if rule.CreatedAt == nil {
		t.Error("expected CreatedAt to be set")
	}
	if rule.UpdatedAt == nil {
		t.Error("expected UpdatedAt to be set")
	}
}

func TestCreateRuleNil(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.CreateRule(ctx, &alertv1.CreateRuleRequest{})
	if err != ErrRuleInvalid {
		t.Fatalf("expected ErrRuleInvalid, got: %v", err)
	}
}

func TestGetRule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateRule(ctx, &alertv1.CreateRuleRequest{
		Rule: &alertv1.AlertRule{
			Name:      "High Memory",
			TenantId:  "tenant-1",
			Metric:    "mem_usage",
			Op:        ">",
			Threshold: 90.0,
			Severity:  "warning",
			Enabled:   true,
		},
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	got, err := svc.GetRule(ctx, &alertv1.GetRuleRequest{Id: created.Id})
	if err != nil {
		t.Fatalf("GetRule failed: %v", err)
	}

	if got.Id != created.Id {
		t.Errorf("expected ID %s, got %s", created.Id, got.Id)
	}
	if got.Name != "High Memory" {
		t.Errorf("expected name High Memory, got %s", got.Name)
	}
	if got.Metric != "mem_usage" {
		t.Errorf("expected metric mem_usage, got %s", got.Metric)
	}
}

func TestGetRuleNotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.GetRule(ctx, &alertv1.GetRuleRequest{Id: "nonexistent"})
	if err != ErrRuleNotFound {
		t.Fatalf("expected ErrRuleNotFound, got: %v", err)
	}
}

func TestListRules(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := svc.CreateRule(ctx, &alertv1.CreateRuleRequest{
			Rule: &alertv1.AlertRule{
				Name:      "Rule " + uuid.New().String(),
				TenantId:  "tenant-1",
				Metric:    "cpu_usage",
				Op:        ">",
				Threshold: float64(70 + i),
				Severity:  "warning",
				Enabled:   true,
			},
		})
		if err != nil {
			t.Fatalf("CreateRule failed: %v", err)
		}
	}

	resp, err := svc.ListRules(ctx)
	if err != nil {
		t.Fatalf("ListRules failed: %v", err)
	}

	if len(resp.Rules) != 3 {
		t.Errorf("expected 3 rules, got %d", len(resp.Rules))
	}
}

func TestUpdateRule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateRule(ctx, &alertv1.CreateRuleRequest{
		Rule: &alertv1.AlertRule{
			Name:      "Original",
			TenantId:  "tenant-1",
			Metric:    "cpu_usage",
			Op:        ">",
			Threshold: 80.0,
			Severity:  "warning",
			Enabled:   true,
		},
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	created.Name = "Updated"
	created.Threshold = 90.0

	updated, err := svc.UpdateRule(ctx, &alertv1.UpdateRuleRequest{Rule: created})
	if err != nil {
		t.Fatalf("UpdateRule failed: %v", err)
	}

	if updated.Name != "Updated" {
		t.Errorf("expected name Updated, got %s", updated.Name)
	}
	if updated.Threshold != 90.0 {
		t.Errorf("expected threshold 90.0, got %f", updated.Threshold)
	}
}

func TestUpdateRuleNotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.UpdateRule(ctx, &alertv1.UpdateRuleRequest{
		Rule: &alertv1.AlertRule{
			Id:        "nonexistent",
			Name:      "Ghost",
			TenantId:  "tenant-1",
			Metric:    "cpu_usage",
			Op:        ">",
			Threshold: 80.0,
		},
	})
	if err != ErrRuleNotFound {
		t.Fatalf("expected ErrRuleNotFound, got: %v", err)
	}
}

func TestDeleteRule(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.CreateRule(ctx, &alertv1.CreateRuleRequest{
		Rule: &alertv1.AlertRule{
			Name:      "ToDelete",
			TenantId:  "tenant-1",
			Metric:    "cpu_usage",
			Op:        ">",
			Threshold: 80.0,
			Severity:  "warning",
			Enabled:   true,
		},
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	err = svc.DeleteRule(ctx, &alertv1.DeleteRuleRequest{Id: created.Id})
	if err != nil {
		t.Fatalf("DeleteRule failed: %v", err)
	}

	_, err = svc.GetRule(ctx, &alertv1.GetRuleRequest{Id: created.Id})
	if err != ErrRuleNotFound {
		t.Fatalf("expected ErrRuleNotFound after delete, got: %v", err)
	}
}

func TestDeleteRuleNotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	err := svc.DeleteRule(ctx, &alertv1.DeleteRuleRequest{Id: "nonexistent"})
	if err != ErrRuleNotFound {
		t.Fatalf("expected ErrRuleNotFound, got: %v", err)
	}
}

func TestEvaluate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.CreateRule(ctx, &alertv1.CreateRuleRequest{
		Rule: &alertv1.AlertRule{
			Name:      "High CPU",
			TenantId:  "tenant-1",
			Metric:    "cpu_usage",
			Op:        ">",
			Threshold: 80.0,
			Severity:  "critical",
			Enabled:   true,
		},
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}

	// 修前这条测试以 `_ = resp` 结尾（断言为零），所以"引擎根本不读指标"在它眼皮下
	// 长期存在。现在必须逐项验证读数确实决定了结论与响应口径（#60）。
	resp, err := svc.Evaluate(ctx, &alertv1.EvaluateRequest{
		TenantId: "tenant-1",
		DeviceId: "device-1",
		Metrics:  map[string]float64{"cpu_usage": 95.0},
	})
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if len(resp.GetAlerts()) != 1 {
		t.Fatalf("95 > 80 应触发 1 条告警，实际 %d 条", len(resp.GetAlerts()))
	}
	a := resp.GetAlerts()[0]
	if a.GetTenantId() != "tenant-1" || a.GetDeviceId() != "device-1" {
		t.Fatalf("告警归属不对：tenant=%q device=%q", a.GetTenantId(), a.GetDeviceId())
	}
	if !strings.Contains(a.GetMessage(), "cpu_usage=95") {
		t.Fatalf("Message 应带现场值：%q", a.GetMessage())
	}
	if a.GetValues()["cpu_usage"] != 95 {
		t.Fatalf("Values 丢了现场读数：%v", a.GetValues())
	}
	if resp.GetEvaluatedRules() != 1 || resp.GetMetricsSupplied() != 1 {
		t.Fatalf("评估面计数不对：evaluated=%d supplied=%d", resp.GetEvaluatedRules(), resp.GetMetricsSupplied())
	}
	if len(resp.GetNoDataRules()) != 0 {
		t.Fatalf("有读数时不该有 no_data：%v", resp.GetNoDataRules())
	}

	// 读数降到 50：同一条规则必须不触发（证明结论跟着读数走，而不是恒触发）。
	resp2, err := svc.Evaluate(ctx, &alertv1.EvaluateRequest{
		TenantId: "tenant-1",
		DeviceId: "device-2",
		Metrics:  map[string]float64{"cpu_usage": 50.0},
	})
	if err != nil {
		t.Fatalf("Evaluate(50) failed: %v", err)
	}
	if len(resp2.GetAlerts()) != 0 {
		t.Fatalf("50 不满足 > 80，却触发了：%d 条", len(resp2.GetAlerts()))
	}
	if resp2.GetEvaluatedRules() != 1 {
		t.Fatalf("该轮应算作已评估，实际 evaluated=%d", resp2.GetEvaluatedRules())
	}

	// 完全不给读数：必须显式落 no_data，而不是"空告警"假装健康。
	resp3, err := svc.Evaluate(ctx, &alertv1.EvaluateRequest{
		TenantId: "tenant-1",
		DeviceId: "device-3",
	})
	if err != nil {
		t.Fatalf("Evaluate(no metrics) failed: %v", err)
	}
	if len(resp3.GetAlerts()) != 0 || resp3.GetEvaluatedRules() != 0 {
		t.Fatalf("无读数时 alerts 与 evaluated 都应为 0：%+v", resp3)
	}
	if len(resp3.GetNoDataRules()) != 1 {
		t.Fatalf("无读数必须回进 no_data_rules（没读数≠没命中），实际 %v", resp3.GetNoDataRules())
	}
}

// TestEvaluate_RuleMetricFieldIsMetricNotRuleID 钉住落库口径：
// 修前 store.Alert.Metric 被填成 RuleID，于是"按指标查告警"整条口径是错的。
func TestEvaluate_RuleMetricFieldIsMetricNotRuleID(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	created, err := svc.CreateRule(ctx, &alertv1.CreateRuleRequest{
		Rule: &alertv1.AlertRule{
			Name: "Hot", TenantId: "tenant-1", Metric: "temperature",
			Op: ">", Threshold: 60, Severity: "warning", Enabled: true,
		},
	})
	if err != nil {
		t.Fatalf("CreateRule: %v", err)
	}
	if _, err := svc.Evaluate(ctx, &alertv1.EvaluateRequest{
		TenantId: "tenant-1", DeviceId: "dev-9", Metrics: map[string]float64{"temperature": 88},
	}); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	list, err := svc.ListAlerts(ctx, &alertv1.ListAlertsRequest{TenantId: "tenant-1"})
	if err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}
	if len(list.GetAlerts()) != 1 {
		t.Fatalf("落库告警 %d 条", len(list.GetAlerts()))
	}
	got := list.GetAlerts()[0]
	if got.GetMetric() == created.GetId() {
		t.Fatalf("Metric 被填成了 RuleID（就是修前那个 bug）：%q", got.GetMetric())
	}
	if got.GetMetric() != "temperature" {
		t.Fatalf("Metric = %q, want temperature", got.GetMetric())
	}
	// 这一段原本断言 rule_id **必须为空**（当时 alerts 表没有 rule_id 列，缺口显式留在
	// storeToProtoAlert 的注释里）。2026-10-04 补列之后，断言随之翻转为"必须等于规则 ID"——
	// 断言写在读侧（ListAlerts 走完整存储往返），所以"列加了但写/读任一侧漏了"都会被判红。
	if got.GetRuleId() != created.GetId() {
		t.Fatalf("rule_id = %q，期望回读到规则 ID %q（评估响应里是对的，落库读回却丢了）",
			got.GetRuleId(), created.GetId())
	}
	if created.GetId() == "" {
		t.Fatal("前置不成立：规则没有 ID")
	}
}

func TestAckAlert(t *testing.T) {
	svc := newTestService()

	st := store.NewMemoryStore()
	st.AddAlert(&store.Alert{
		AlertID:  "alert-1",
		TenantID: "tenant-1",
		Status:   "firing",
	})

	svc.store = st

	err := svc.AckAlert(context.Background(), &alertv1.AckAlertRequest{Id: "nonexistent"})
	if err != ErrAlertNotFound {
		t.Fatalf("expected ErrAlertNotFound, got: %v", err)
	}

	err = svc.AckAlert(context.Background(), &alertv1.AckAlertRequest{Id: "alert-1"})
	if err != nil {
		t.Fatalf("AckAlert failed: %v", err)
	}
}

func TestSilenceAlert(t *testing.T) {
	svc := newTestService()

	st := store.NewMemoryStore()
	st.AddAlert(&store.Alert{
		AlertID:  "alert-1",
		TenantID: "tenant-1",
		Status:   "firing",
	})

	svc.store = st

	err := svc.SilenceAlert(context.Background(), &alertv1.SilenceAlertRequest{
		Id:              "nonexistent",
		DurationMinutes: 30,
		Comment:         "maintenance",
	})
	if err != ErrAlertNotFound {
		t.Fatalf("expected ErrAlertNotFound, got: %v", err)
	}

	err = svc.SilenceAlert(context.Background(), &alertv1.SilenceAlertRequest{
		Id:              "alert-1",
		DurationMinutes: 30,
		Comment:         "maintenance",
	})
	if err != nil {
		t.Fatalf("SilenceAlert failed: %v", err)
	}
}
