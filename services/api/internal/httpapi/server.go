package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"video-generator/services/api/internal/model"
	"video-generator/services/api/internal/store"
	"video-generator/services/api/internal/worker"
)

type Server struct {
	store   *store.Store
	worker  *worker.Client
	media   *http.Client
	webDist string
	mux     *http.ServeMux
}

// RouteRegistrar 是 Agent 与 HTTP 壳的最小接口。新 Agent 只需注册自己的路由，
// Server 不需要导入每个具体业务包，也不需要知道它有哪些提示词或步骤。
type RouteRegistrar interface{ Register(*http.ServeMux) }

func New(s *store.Store, w *worker.Client, webDist string, agents ...RouteRegistrar) *Server {
	server := &Server{
		store:   s,
		worker:  w,
		media:   &http.Client{Timeout: 10 * time.Minute},
		webDist: webDist,
		mux:     http.NewServeMux(),
	}
	server.routes()
	for _, agent := range agents {
		agent.Register(server.mux)
	}
	return server
}

func (s *Server) Handler() http.Handler {
	return withCORS(withLogging(s.mux))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("GET /api/v1/config", s.config)
	s.mux.HandleFunc("POST /api/v1/assets/input", s.uploadInput)
	s.mux.HandleFunc("POST /api/v1/generations", s.createGeneration)
	s.mux.HandleFunc("GET /api/v1/generations", s.listGenerations)
	s.mux.HandleFunc("GET /api/v1/generations/{id}", s.getGeneration)
	s.mux.HandleFunc("GET /api/v1/generations/{id}/video", s.generationVideo)
	s.mux.HandleFunc("POST /api/v1/generations/{id}/cancel", s.cancelGeneration)
	s.mux.HandleFunc("GET /api/v1/generations/{id}/events", s.generationEvents)

	if s.webDist != "" {
		s.mux.HandleFunc("GET /", s.serveWeb)
	}
}

func (s *Server) config(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"defaults": map[string]any{
			"model":          "minimax-h3",
			"generationMode": "t2v",
			"resolutionTier": "768p",
			"orientation":    "portrait",
			"seconds":        15,
		},
		"capabilities": map[string]any{
			"generationModes": []string{"t2v", "universal_reference_video"},
			"resolutions":     []string{"480p", "720p", "768p", "1080p"},
			"orientations":    []string{"landscape", "portrait", "square"},
			"seconds":         []int{5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
		},
	})
}

func (s *Server) uploadInput(w http.ResponseWriter, r *http.Request) {
	modelAlias := strings.TrimSpace(r.URL.Query().Get("model"))
	if modelAlias == "" {
		writeError(w, http.StatusUnprocessableEntity, "model 不能为空")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		writeError(w, http.StatusBadRequest, "上传请求必须使用 multipart/form-data")
		return
	}
	if r.ContentLength <= 0 || r.ContentLength > 51<<20 {
		writeError(w, http.StatusRequestEntityTooLarge, "上传文件不能超过 50 MiB")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 51<<20)
	assetID, err := s.worker.UploadInput(
		r.Context(), modelAlias, r.Header.Get("Content-Type"), r.ContentLength, r.Body,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "上传输入素材失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"asset": map[string]string{"assetId": assetID}})
}

func (s *Server) createGeneration(w http.ResponseWriter, r *http.Request) {
	var input model.GenerationRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validate(input); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	now := time.Now().UTC()
	item := model.Generation{
		ID:        newID("gen"),
		Request:   input,
		Status:    "queued",
		Progress:  0,
		CreatedAt: now,
		UpdatedAt: now,
	}
	job, err := s.worker.Create(r.Context(), input)
	if err != nil {
		item.Status = "failed"
		item.Error = "视频 Worker 暂不可用，请确认 Python 服务已启动"
		item.UpdatedAt = time.Now().UTC()
		_ = s.store.Put(item)
		writeJSON(w, http.StatusServiceUnavailable, item)
		return
	}
	applyWorker(&item, job)
	if err := s.store.Put(item); err != nil {
		writeError(w, http.StatusInternalServerError, "保存任务失败")
		return
	}
	writeJSON(w, http.StatusAccepted, item)
}

func (s *Server) listGenerations(w http.ResponseWriter, r *http.Request) {
	items := s.store.List()
	for i := range items {
		if isActive(items[i].Status) {
			items[i] = s.refresh(r.Context(), items[i])
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) getGeneration(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.Get(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取任务失败")
		return
	}
	if isActive(item.Status) {
		item = s.refresh(r.Context(), item)
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) generationVideo(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.Get(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取任务失败")
		return
	}
	if item.Status != "succeeded" || item.VideoURL == "" {
		writeError(w, http.StatusConflict, "视频尚未生成完成")
		return
	}

	videoURL, err := url.Parse(item.VideoURL)
	if err != nil || (videoURL.Scheme != "http" && videoURL.Scheme != "https") || videoURL.Host == "" {
		writeError(w, http.StatusBadGateway, "供应商返回的视频地址无效")
		return
	}

	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, videoURL.String(), nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "无法创建视频读取请求")
		return
	}
	request.Header.Set("Accept", "video/*")
	for _, header := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"} {
		if value := r.Header.Get(header); value != "" {
			request.Header.Set(header, value)
		}
	}

	response, err := s.media.Do(request)
	if err != nil {
		writeError(w, http.StatusBadGateway, "读取供应商视频失败")
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		writeError(w, http.StatusBadGateway, "供应商视频暂不可用")
		return
	}

	for _, header := range []string{
		"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified",
	} {
		if value := response.Header.Get(header); value != "" {
			w.Header().Set(header, value)
		}
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "video/mp4")
	}
	if r.URL.Query().Get("download") == "1" {
		filename := item.ID + videoExtension(videoURL.Path)
		w.Header().Set(
			"Content-Disposition",
			mime.FormatMediaType("attachment", map[string]string{"filename": filename}),
		)
	}
	w.WriteHeader(response.StatusCode)
	if _, err := io.Copy(w, response.Body); err != nil {
		slog.Warn("stream provider video failed", "generation_id", item.ID, "error", err)
	}
}

func videoExtension(videoPath string) string {
	extension := strings.ToLower(path.Ext(videoPath))
	switch extension {
	case ".mp4", ".webm", ".mov", ".m4v":
		return extension
	default:
		return ".mp4"
	}
}

func (s *Server) cancelGeneration(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	if !isActive(item.Status) || item.WorkerJobID == "" {
		writeJSON(w, http.StatusOK, item)
		return
	}
	job, err := s.worker.Cancel(r.Context(), item.WorkerJobID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "取消 Worker 任务失败")
		return
	}
	applyWorker(&item, job)
	_ = s.store.Put(item)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) generationEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "当前服务不支持事件流")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	lastEventID, _ := strconv.Atoi(r.Header.Get("Last-Event-ID"))
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	eventID := lastEventID
	for {
		item, err := s.store.Get(r.PathValue("id"))
		if err != nil {
			fmt.Fprintf(w, "event: error\ndata: {\"message\":\"任务不存在\"}\n\n")
			flusher.Flush()
			return
		}
		if isActive(item.Status) {
			item = s.refresh(r.Context(), item)
		}
		payload, _ := json.Marshal(item)
		eventID++
		fmt.Fprintf(w, "id: %d\nevent: generation.updated\ndata: %s\n\n", eventID, payload)
		flusher.Flush()
		if !isActive(item.Status) {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) refresh(ctx context.Context, item model.Generation) model.Generation {
	if item.WorkerJobID == "" {
		return item
	}
	job, err := s.worker.Get(ctx, item.WorkerJobID)
	if err != nil {
		slog.Warn("refresh worker job failed", "generation_id", item.ID, "error", err)
		return item
	}
	applyWorker(&item, job)
	if err := s.store.Put(item); err != nil {
		slog.Error("persist refreshed job failed", "generation_id", item.ID, "error", err)
	}
	return item
}

func (s *Server) serveWeb(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	requested := filepath.Join(s.webDist, cleanPath)
	distRoot, rootErr := filepath.Abs(s.webDist)
	requestedAbs, requestedErr := filepath.Abs(requested)
	if rootErr != nil || requestedErr != nil ||
		(requestedAbs != distRoot && !strings.HasPrefix(requestedAbs, distRoot+string(os.PathSeparator))) {
		writeError(w, http.StatusBadRequest, "静态资源路径无效")
		return
	}
	if info, err := os.Stat(requested); err == nil && !info.IsDir() {
		http.ServeFile(w, r, requested)
		return
	}
	index := filepath.Join(s.webDist, "index.html")
	if _, err := os.Stat(index); err != nil {
		writeError(w, http.StatusNotFound, "前端尚未构建")
		return
	}
	http.ServeFile(w, r, index)
}

func validate(input model.GenerationRequest) error {
	if strings.TrimSpace(input.Model) == "" {
		return errors.New("model 不能为空")
	}
	if strings.TrimSpace(input.Prompt) == "" {
		return errors.New("prompt 不能为空")
	}
	if len([]rune(input.Prompt)) > 7000 {
		return errors.New("prompt 不能超过 7000 个字符")
	}
	if !oneOf(input.GenerationMode, "t2v", "universal_reference_video") {
		return errors.New("generationMode 不受支持")
	}
	if !oneOf(input.ResolutionTier, "480p", "720p", "768p", "1080p") {
		return errors.New("resolutionTier 不受支持")
	}
	if !oneOf(input.Orientation, "landscape", "portrait", "square") {
		return errors.New("orientation 不受支持")
	}
	if input.Seconds < 5 || input.Seconds > 15 {
		return errors.New("seconds 仅支持 5 到 15 的整数")
	}
	if input.GenerationMode == "t2v" {
		if len(input.ReferenceInputs) != 0 {
			return errors.New("t2v 不接受参考素材")
		}
		return nil
	}
	if input.ResolutionTier != "768p" || !oneOf(input.Orientation, "landscape", "portrait") {
		return errors.New("全能参考仅支持 768p 横屏或竖屏")
	}
	if len(input.ReferenceInputs) == 0 || len(input.ReferenceInputs) > 12 {
		return errors.New("全能参考素材总数必须为 1 到 12")
	}
	counts := map[string]int{}
	seen := map[string]bool{}
	mediaOrder := map[string]int{"image": 1, "video": 2, "audio": 3}
	lastMediaOrder := 0
	for _, reference := range input.ReferenceInputs {
		if strings.TrimSpace(reference.AssetID) == "" || seen[reference.AssetID] {
			return errors.New("全能参考素材 assetId 不能为空或重复")
		}
		seen[reference.AssetID] = true
		expectedMediaType := strings.TrimPrefix(reference.Role, "reference_")
		if !oneOf(reference.Role, "reference_image", "reference_video", "reference_audio") || reference.MediaType != expectedMediaType {
			return errors.New("全能参考素材 role 与 mediaType 不匹配")
		}
		order := mediaOrder[reference.MediaType]
		if order < lastMediaOrder {
			return errors.New("全能参考素材必须按图片、视频、音频排序")
		}
		lastMediaOrder = order
		counts[reference.MediaType]++
	}
	if counts["image"] > 9 || counts["video"] > 1 || counts["audio"] > 3 {
		return errors.New("全能参考最多支持 9 张图片、1 段视频和 3 段音频")
	}
	if counts["image"]+counts["video"] == 0 {
		return errors.New("全能参考至少需要 1 张图片或 1 段视频")
	}
	return nil
}

func applyWorker(item *model.Generation, job model.WorkerJob) {
	item.WorkerJobID = job.ID
	item.ProviderID = job.ProviderID
	item.Status = job.Status
	item.Progress = job.Progress
	item.VideoURL = job.VideoURL
	item.Error = job.Error
	item.UpdatedAt = time.Now().UTC()
}

func isActive(status string) bool {
	return status == "queued" || status == "running" || status == "waiting_provider"
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func newID(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b)
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("请求 JSON 无效: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("请求 JSON 只能包含一个对象")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":      "request_failed",
			"message":   message,
			"retryable": status >= 500,
		},
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 本机免登录不代表任意网页都可以发起付费任务。先拒绝外部 Origin，
		// 而不只是设置 CORS 响应头（简单 POST 即使读不到响应，也可能已被执行）。
		origin := r.Header.Get("Origin")
		if origin != "" {
			u, err := url.Parse(origin)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") {
				writeError(w, http.StatusForbidden, "仅允许本机页面访问本机 API")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Last-Event-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started))
	})
}
