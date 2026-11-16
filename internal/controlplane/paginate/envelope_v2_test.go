package paginate

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

type sampleItem struct {
	ID string `json:"id"`
}

func TestWantEnvelopeV2(t *testing.T) {
	cases := []struct {
		q    url.Values
		want bool
	}{
		{url.Values{}, false},
		{url.Values{"envelope": {"v1"}}, false},
		{url.Values{"envelope": {"V2"}}, false}, // 大小写敏感
		{url.Values{"envelope": {"v2"}}, true},
	}
	for _, c := range cases {
		if got := WantEnvelopeV2(c.q); got != c.want {
			t.Errorf("WantEnvelopeV2(%v)=%v want %v", c.q, got, c.want)
		}
	}
}

func TestWriteListV2_Paged(t *testing.T) {
	items := []sampleItem{{"a"}, {"b"}, {"c"}, {"d"}, {"e"}}
	rec := httptest.NewRecorder()
	WriteListV2(rec, http.StatusOK, items, 2, 2)

	var got PaginateResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, rec.Body.String())
	}
	if got.Total != 5 || got.Page != 2 || got.PageSize != 2 || !got.HasMore {
		t.Fatalf("envelope meta mismatch: %+v", got)
	}
	// data 应含 c,d 两项
	var data []sampleItem
	b, _ := json.Marshal(got.Data)
	if err := json.Unmarshal(b, &data); err != nil || len(data) != 2 || data[0].ID != "c" || data[1].ID != "d" {
		t.Fatalf("data slice mismatch: %v err=%v", data, err)
	}
}

func TestWriteListV2_NoPage(t *testing.T) {
	items := []sampleItem{{"a"}, {"b"}}
	rec := httptest.NewRecorder()
	WriteListV2(rec, http.StatusOK, items, 0, 0)

	var got PaginateResult
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Total != 2 || got.Page != 1 || got.PageSize != 2 || got.HasMore {
		t.Fatalf("no-page envelope mismatch: %+v", got)
	}
}

func TestWriteListV2_ClampPageSize(t *testing.T) {
	items := make([]sampleItem, 300)
	rec := httptest.NewRecorder()
	WriteListV2(rec, http.StatusOK, items, 1, 1000)

	var got PaginateResult
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.PageSize != 200 || !got.HasMore {
		t.Fatalf("pageSize should clamp to 200: %+v", got)
	}
}

func TestWriteListV2_NilData(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteListV2(rec, http.StatusOK, nil, 1, 10)

	var got PaginateResult
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Total != 0 || got.Page != 1 || got.PageSize != 10 || got.HasMore {
		t.Fatalf("nil data envelope mismatch: %+v", got)
	}
}
