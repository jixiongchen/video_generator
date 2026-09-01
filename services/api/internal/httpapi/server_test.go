package httpapi

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"video-generator/services/api/internal/model"
	"video-generator/services/api/internal/store"
	"video-generator/services/api/internal/worker"
)

func TestRejectsInvalidGeneration(t *testing.T) {
	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := New(s, worker.New("http://127.0.0.1:1"), "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/generations", bytes.NewBufferString(`{"model":"","prompt":""}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected %d, got %d: %s", http.StatusUnprocessableEntity, resp.Code, resp.Body.String())
	}
}

func TestAcceptsDocumentedMinimaxParameters(t *testing.T) {
	input := model.GenerationRequest{
		Model:          "minimax-h3",
		Prompt:         "test",
		GenerationMode: "t2v",
		ResolutionTier: "768p",
		Orientation:    "portrait",
		Seconds:        15,
	}
	if err := validate(input); err != nil {
		t.Fatalf("expected request to be valid, got %v", err)
	}
}

func TestGenerationVideoProxiesRangeAndDownload(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=0-3" {
			t.Fatalf("expected Range header, got %q", got)
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", "bytes 0-3/4")
		w.Header().Set("Content-Length", "4")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("test"))
	}))
	defer upstream.Close()

	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	item := model.Generation{
		ID:        "gen-video-test",
		Status:    "succeeded",
		VideoURL:  upstream.URL + "/result.mp4",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.Put(item); err != nil {
		t.Fatal(err)
	}

	server := New(s, worker.New("http://127.0.0.1:1"), "")
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/generations/gen-video-test/video?download=1",
		nil,
	)
	req.Header.Set("Range", "bytes=0-3")
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)

	result := resp.Result()
	defer result.Body.Close()
	body, _ := io.ReadAll(result.Body)
	if result.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected %d, got %d: %s", http.StatusPartialContent, result.StatusCode, body)
	}
	if string(body) != "test" {
		t.Fatalf("expected proxied body, got %q", body)
	}
	if got := result.Header.Get("Content-Range"); got != "bytes 0-3/4" {
		t.Fatalf("expected Content-Range, got %q", got)
	}
	if got := result.Header.Get("Content-Disposition"); !strings.Contains(got, "attachment") {
		t.Fatalf("expected download disposition, got %q", got)
	}
}
