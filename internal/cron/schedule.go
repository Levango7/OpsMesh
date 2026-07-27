package cron

import (
	"log"
	"time"

	"opsmesh/internal/proto"
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
	CreateTask(*proto.Task) *proto.Task
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
		s.store.CreateTask(derived)
		// 更新模板任务的 LastFiredAt
		t.LastFiredAt = now
		s.store.UpdateTask(t)
	}
}
