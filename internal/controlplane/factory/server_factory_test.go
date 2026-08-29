package factory

import (
	"testing"

	"opsmesh/internal/config"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"all empty", []string{"", "", ""}, ""},
		{"first wins", []string{"a", "b", "c"}, "a"},
		{"second wins", []string{"", "b", "c"}, "b"},
		{"third wins", []string{"", "", "c"}, "c"},
		{"single", []string{"only"}, "only"},
		{"nil input", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FirstNonEmpty(tt.in...)
			if got != tt.want {
				t.Fatalf("FirstNonEmpty(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSelectStore_Memory(t *testing.T) {
	cfg := &config.Config{
		Store:     "memory",
		Demo:      true,
		RedisAddr: "",
	}
	st, err := SelectStore(cfg, nil)
	if err != nil {
		t.Fatalf("SelectStore(memory) error: %v", err)
	}
	if _, ok := st.(*store.MemoryStore); !ok {
		t.Fatalf("expected *store.MemoryStore, got %T", st)
	}
}

func TestSelectStore_EmptyDSNFallsBack(t *testing.T) {
	cfg := &config.Config{
		Store:       "mysql",
		MySQLDSN:    "",
		MultiSchema: false,
		Demo:        true,
	}
	st, err := SelectStore(cfg, nil)
	if err != nil {
		t.Fatalf("SelectStore(mysql, empty DSN) error: %v", err)
	}
	if _, ok := st.(*store.MemoryStore); !ok {
		t.Fatalf("expected fallback *store.MemoryStore, got %T", st)
	}
}

func TestSelectSessionStore_InProcess(t *testing.T) {
	cfg := &config.Config{SessionStore: ""}
	ss, err := SelectSessionStore(cfg)
	if err != nil {
		t.Fatalf("SelectSessionStore(empty) error: %v", err)
	}
	if _, ok := ss.(*store.InProcessSessionStore); !ok {
		t.Fatalf("expected *store.InProcessSessionStore, got %T", ss)
	}
}

func TestSelectSessionStore_InvalidFormat(t *testing.T) {
	cfg := &config.Config{SessionStore: "not-a-url"}
	_, err := SelectSessionStore(cfg)
	if err == nil {
		t.Fatal("expected error for invalid session store format")
	}
}

func TestStoreDispatcher_TaskStatesEmpty(t *testing.T) {
	st := store.NewMemoryStore()
	d := &StoreDispatcher{Store: st}

	emptyStates := d.TaskStates(nil, "default")
	if len(emptyStates) != 0 {
		t.Fatalf("TaskStates(nil) = %v, want empty", emptyStates)
	}
}

func TestStoreDispatcher_CreateTask(t *testing.T) {
	st := store.NewMemoryStore()
	d := &StoreDispatcher{Store: st}

	task := &proto.Task{TaskID: "task-disp-1", TenantID: "default", Status: "running"}
	created := d.CreateTask(task)
	if created == nil {
		t.Fatal("CreateTask returned nil")
	}
	if created.TaskID != "task-disp-1" {
		t.Fatalf("CreateTask returned taskID = %q, want task-disp-1", created.TaskID)
	}
}
