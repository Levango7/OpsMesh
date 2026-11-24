package knowledge

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestInMemoryKnowledgeStoreConcurrentAccess(t *testing.T) {
	s := NewInMemoryKnowledgeStore()
	ctx := context.Background()
	for i := 0; i < 64; i++ {
		if err := s.Upsert(ctx, []KnowledgeDoc{{ID: fmt.Sprint(i), Source: SourceManual, Content: "seed"}}); err != nil {
			t.Fatal(err)
		}
	}

	const iterations = 200
	var wg sync.WaitGroup
	wg.Add(5)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if err := s.Upsert(ctx, []KnowledgeDoc{{ID: fmt.Sprint(i % 64), Source: SourceManual, Content: "update", Embedding: []float64{1, float64(i)}}}); err != nil {
				t.Errorf("Upsert: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if _, err := s.Search(ctx, "", 10, nil); err != nil {
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
			if _, err := s.ListBySource(ctx, SourceManual, 10); err != nil {
				t.Errorf("ListBySource: %v", err)
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

func TestInMemoryKnowledgeStoreClonesMutableDocuments(t *testing.T) {
	s := NewInMemoryKnowledgeStore()
	ctx := context.Background()
	embedding := []float64{1, 0}
	metadata := map[string]interface{}{"kind": "original"}
	citation := &Citation{SourceID: "original"}
	if err := s.Upsert(ctx, []KnowledgeDoc{{ID: "doc", Source: SourceManual, Content: "content", Embedding: embedding, Metadata: metadata, Citation: citation}}); err != nil {
		t.Fatal(err)
	}

	embedding[0] = 99
	metadata["kind"] = "caller-mutated"
	citation.SourceID = "caller-mutated"
	filter := map[string]interface{}{"kind": "original"}
	docs, err := s.Search(ctx, "", 1, filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Embedding[0] != 1 || docs[0].Citation.SourceID != "original" {
		t.Fatalf("stored document was mutated through input aliases: %+v", docs)
	}

	docs[0].Embedding[0] = 42
	docs[0].Metadata["kind"] = "result-mutated"
	docs[0].Citation.SourceID = "result-mutated"
	again, err := s.Search(ctx, "", 1, filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].Embedding[0] != 1 || again[0].Metadata["kind"] != "original" || again[0].Citation.SourceID != "original" {
		t.Fatalf("stored document was mutated through result aliases: %+v", again)
	}
}
