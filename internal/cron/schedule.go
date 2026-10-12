package cron

import (
	"log"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// Scheduler 定时任务调度器，定期检查带 Schedule 的模板任务并派生实例。
type Scheduler struct {
	store    TaskStore // AllTasks / CreateTask / UpdateTask
	interval time.Duration
	stopCh   chan struct{}
}

// TaskStore 是调度器需要的 store 子集。
type TaskStore interface {
	AllTasks(tenantID string) []*proto.Task
	// CreateTask 2026-10-12 缺口 3：返回 error——派生实例落库失败必须让调度器
	// 看见：签名没改前失败只会静默丢一个周期（模板的 LastFiredAt 照翻，下一次
	// 到点才会再试，这一次的运行就此消失且无迹可查）。
	CreateTask(*proto.Task) (*proto.Task, error)
	UpdateTask(*proto.Task)
}

// NewScheduler 构造调度器。
func NewScheduler(store TaskStore) *Scheduler {
	return &Scheduler{
		store:    store,
		interval: 60 * time.Second, // 每分钟检查一次
		stopCh:   make(chan struct{}),
	}
}

// Start 在后台 goroutine 中启动调度循环。
func (s *Scheduler) Start() {
	go s.loop()
	log.Println("[scheduler] 启动定时任务调度（间隔 60s）")
}

// Stop 停止调度循环。
func (s *Scheduler) Stop() {
	close(s.stopCh)
}

func (s *Scheduler) loop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.tick()
		case <-s.stopCh:
			return
		}
	}
}

func (s *Scheduler) tick() {
	now := time.Now()
	tasks := s.store.AllTasks("") // 全量任务
	if tasks == nil {
		return
	}
	for _, t := range tasks {
		if t.Schedule == "" {
			continue
		}
		// 匹配当前时间
		match, err := Match(t.Schedule, now)
		if err != nil || !match {
			continue
		}
		// 防重入：跳过 LastFiredAt 落在 interval 以内的模板任务
		if !t.LastFiredAt.IsZero() && now.Sub(t.LastFiredAt) < s.interval {
			continue
		}
		// 派生任务实例：克隆模板属性，生成新 ID
		derived := &proto.Task{
			TaskID:    "", // store 自动分配
			AgentID:   t.AgentID,
			TenantID:  t.TenantID,
			Type:      t.Type,
			Command:   t.Command,
			Content:   t.Content,
			Path:      t.Path,
			DependsOn: t.DependsOn,
			ParentID:  t.TaskID,
		}
		if _, err := s.store.CreateTask(derived); err != nil {
			// 缺口 3：派生实例没落库 = 这一轮什么都没跑。记日志并**不动**
			// LastFiredAt——翻了就把这一次运行永久跳过（防重入窗口内不会再试），
			// 留着让它下一轮重试。调度器自身不能因此中断。
			log.Printf("[scheduler] 模板 %s 派生任务落库失败: %v", t.TaskID, err)
			continue
		}
		// 更新模板任务的 LastFiredAt
		t.LastFiredAt = now
		s.store.UpdateTask(t)
	}
}
