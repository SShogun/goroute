package tests

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/ziuus/goroute/pkg/providers"
	"github.com/ziuus/goroute/pkg/proxy"
	"github.com/ziuus/goroute/pkg/quota"
	"github.com/ziuus/goroute/pkg/router"
)

type benchmarkMockProvider struct {
	id string
}

func (b *benchmarkMockProvider) ID() string   { return b.id }
func (b *benchmarkMockProvider) Type() string { return "mock" }
func (b *benchmarkMockProvider) IsHealthy() bool {
	return true
}

func (b *benchmarkMockProvider) ChatCompletion(ctx context.Context, req *providers.ChatRequest) (io.ReadCloser, bool, int, error) {
	resp := `{"id":"bench-123","choices":[{"message":{"role":"assistant","content":"ok"}}]}`
	return io.NopCloser(strings.NewReader(resp)), false, http.StatusOK, nil
}

func (b *benchmarkMockProvider) Models(ctx context.Context) ([]providers.ModelInfo, error) {
	return []providers.ModelInfo{{ID: "bench-model", Object: "model"}}, nil
}

func (b *benchmarkMockProvider) Embeddings(ctx context.Context, req *providers.EmbeddingRequest) (*providers.EmbeddingResponse, error) {
	return &providers.EmbeddingResponse{
		Object: "list",
		Model:  req.Model,
		Data:   []providers.EmbeddingItem{{Object: "embedding", Index: 0, Embedding: []float64{0.01, 0.02}}},
	}, nil
}

func TestMemoryAndConcurrency(t *testing.T) {
	runtime.GC()
	var mBefore runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	r := router.NewRouter()
	r.RegisterProvider(&benchmarkMockProvider{id: "mock1"})
	r.RegisterCombo("bench-combo", "failover", []router.Target{
		{ProviderID: "mock1", Model: "bench-model"},
	})

	srv := proxy.NewServer(r, quota.NewLimiter(), false)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 500 requests across pooled workers
	const totalRequests = 500
	const workers = 50
	client := ts.Client()

	reqChan := make(chan struct{}, totalRequests)
	for i := 0; i < totalRequests; i++ {
		reqChan <- struct{}{}
	}
	close(reqChan)

	var wg sync.WaitGroup
	wg.Add(workers)

	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for range reqChan {
				reqBody := `{"model":"bench-combo","messages":[{"role":"user","content":"ping"}]}`
				resp, err := client.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
				if err != nil {
					t.Errorf("request failed: %v", err)
					return
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
	}

	wg.Wait()

	runtime.GC()
	var mAfter runtime.MemStats
	runtime.ReadMemStats(&mAfter)

	allocMB := float64(mAfter.Alloc) / 1024 / 1024
	t.Logf("Total Heap Alloc after 500 concurrent requests: %.2f MB", allocMB)

	// Memory must be well under 25MB
	if allocMB > 25.0 {
		t.Errorf("Memory too high: %.2f MB (target < 25MB)", allocMB)
	}
}
