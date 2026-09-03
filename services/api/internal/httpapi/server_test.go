package httpapi

import (
	"bytes"
	"io"
	"mime/multipart"
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
		Seconds:        6,
	}
	if err := validate(input); err != nil {
		t.Fatalf("expected request to be valid, got %v", err)
	}
}

func TestValidatesUniversalReferenceInputs(t *testing.T) {
	input := model.GenerationRequest{
		Model:          "minimax-h3",
		Prompt:         "参考主体和声音生成短片",
		GenerationMode: "universal_reference_video",
		ResolutionTier: "768p",
		Orientation:    "portrait",
		Seconds:        15,
		ReferenceInputs: []model.ReferenceInput{
			{Role: "reference_image", AssetID: "image-1", MediaType: "image"},
			{Role: "reference_audio", AssetID: "audio-1", MediaType: "audio"},
		},
	}
	if err := validate(input); err != nil {
		t.Fatalf("expected universal reference request to be valid, got %v", err)
	}
	input.ReferenceInputs = input.ReferenceInputs[1:]
	if err := validate(input); err == nil {
		t.Fatal("expected audio-only universal reference request to be rejected")
	}
}

func TestUploadInputProxiesMultipartToWorker(t *testing.T) {
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/assets/input" || r.URL.Query().Get("model") != "minimax-h3" ||
			!strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			http.Error(w, "unexpected upload request", http.StatusBadRequest)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"asset":{"assetId":"asset-input"}}`))
	}))
	defer workerServer.Close()

	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, _ := form.CreateFormFile("file", "reference.png")
	_, _ = file.Write([]byte("png"))
	_ = form.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/assets/input?model=minimax-h3", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp := httptest.NewRecorder()
	New(s, worker.New(workerServer.URL), "").Handler().ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated || !strings.Contains(resp.Body.String(), "asset-input") {
		t.Fatalf("expected proxied asset, got %d: %s", resp.Code, resp.Body.String())
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
