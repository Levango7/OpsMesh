package aigateway

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestInMemoryVectorStoreConcurrentAccess(t *testing.T) {
	s := NewInMemoryVectorStore()
	ctx := context.Background()
	for i := 0; i < 64; i++ {
		if err := s.Upsert(ctx, []VectorDocument{{ID: fmt.Sprint(i), Content: "seed", Embedding: []float64{1}}}); err != nil {
			t.Fatal(err)
		}
	}

	const iterations = 200
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if err := s.Upsert(ctx, []VectorDocument{{ID: fmt.Sprint(i % 64), Content: "update", Embedding: []float64{1, float64(i)}}}); err != nil {
				t.Errorf("Upsert: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if _, err := s.Search(ctx, []float64{1}, 10, nil); err != nil {
				t.Errorf("Search: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if _, err := s.Count(ctx); err != nil {
				t.Errorf("Count: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if err := s.Delete(ctx, []string{fmt.Sprint(i % 64)}); err != nil {
				t.Errorf("Delete: %v", err)
				return
			}
		}
	}()
	wg.Wait()
}

func TestInMemoryVectorStoreClonesMutableDocuments(t *testing.T) {
	s := NewInMemoryVectorStore()
	ctx := context.Background()
	embedding := []float64{1, 0}
	metadata := map[string]interface{}{"kind": "original"}
	if err := s.Upsert(ctx, []VectorDocument{{ID: "doc", Content: "content", Embedding: embedding, Metadata: metadata}}); err != nil {
		t.Fatal(err)
	}

	embedding[0] = 99
	metadata["kind"] = "caller-mutated"
	filter := map[string]interface{}{"kind": "original"}
	docs, err := s.Search(ctx, []float64{1, 0}, 1, filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Embedding[0] != 1 {
		t.Fatalf("stored document was mutated through input aliases: %+v", docs)
	}

	docs[0].Embedding[0] = 42
	docs[0].Metadata["kind"] = "result-mutated"
	again, err := s.Search(ctx, []float64{1, 0}, 1, filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].Embedding[0] != 1 || again[0].Metadata["kind"] != "original" {
		t.Fatalf("stored document was mutated through result aliases: %+v", again)
	}
}
