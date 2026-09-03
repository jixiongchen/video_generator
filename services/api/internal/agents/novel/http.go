package novel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Register 让小说接口自成模块，不把 Agent 的业务判断继续堆进视频 server.go。
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/agents/config", func(w http.ResponseWriter, r *http.Request) { v, e := s.Config(r.Context()); respond(w, v, e) })
	mux.HandleFunc("GET /api/v1/novels", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.List()
		respond(w, map[string]any{"items": v}, e)
	})
	mux.HandleFunc("POST /api/v1/novels", s.importHTTP)
	mux.HandleFunc("GET /api/v1/novels/{id}", func(w http.ResponseWriter, r *http.Request) { v, e := s.Get(r.PathValue("id")); respond(w, v, e) })
	mux.HandleFunc("PATCH /api/v1/novels/{id}", func(w http.ResponseWriter, r *http.Request) {
		var input SettingsInput
		if !readBody(w, r, &input) {
			return
		}
		v, e := s.Settings(r.PathValue("id"), input)
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/v1/novels/{id}/chapters", func(w http.ResponseWriter, r *http.Request) {
		p, e := s.Get(r.PathValue("id"))
		if e != nil {
			respond(w, nil, e)
			return
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		offset = max(0, min(offset, len(p.Chapters)))
		respond(w, map[string]any{"items": p.Chapters[offset:min(offset+100, len(p.Chapters))], "total": len(p.Chapters)}, nil)
	})
	mux.HandleFunc("GET /api/v1/novels/{id}/sources/{sourceId}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Source(r.PathValue("id"), r.PathValue("sourceId"))
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/v1/novels/{id}/checks", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Checks(r.PathValue("id"))
		respond(w, map[string]any{"items": v}, e)
	})
	// bible、outline、episode-NNNN 共享版本接口，正文协议由各 Agent 校验器负责。
	mux.HandleFunc("GET /api/v1/novels/{id}/documents/{docId}", func(w http.ResponseWriter, r *http.Request) {
		rev, _ := strconv.Atoi(r.URL.Query().Get("revision"))
		v, e := s.Document(r.PathValue("id"), r.PathValue("docId"), rev)
		respond(w, v, e)
	})
	for _, pattern := range []string{"PUT /api/v1/novels/{id}/documents/{docId}", "POST /api/v1/novels/{id}/documents/{docId}/approve"} {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			var input EditInput
			if !readBody(w, r, &input) {
				return
			}
			v, e := s.Edit(r.Context(), r.PathValue("id"), r.PathValue("docId"), input, strings.HasSuffix(r.URL.Path, "/approve"))
			respond(w, v, e)
		})
	}
	mux.HandleFunc("POST /api/v1/novels/{id}/agent-runs", func(w http.ResponseWriter, r *http.Request) {
		var input StartInput
		if !readBody(w, r, &input) {
			return
		}
		v, e := s.Start(r.Context(), r.PathValue("id"), input)
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/v1/agent-runs/{id}", func(w http.ResponseWriter, r *http.Request) { v, e := s.Run(r.PathValue("id")); respond(w, v, e) })
	mux.HandleFunc("POST /api/v1/agent-runs/{id}/{action}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Control(r.Context(), r.PathValue("id"), r.PathValue("action"))
		respond(w, v, e)
	})
	mux.HandleFunc("GET /api/v1/agent-runs/{id}/events", s.eventsHTTP)
	mux.HandleFunc("GET /api/v1/novels/{id}/export", s.exportHTTP)
}

func respond(w http.ResponseWriter, value any, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		status := http.StatusBadRequest
		if os.IsNotExist(err) {
			status = http.StatusNotFound
		}
		if strings.Contains(err.Error(), "版本") || strings.Contains(err.Error(), "运行中") {
			status = http.StatusConflict
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": err.Error()}})
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}

func readBody(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	err := d.Decode(value)
	if err == nil {
		if next := d.Decode(new(any)); next != io.EOF {
			err = fmt.Errorf("请求必须包含且只包含一个 JSON 对象")
		}
	}
	if err != nil {
		respond(w, nil, fmt.Errorf("请求 JSON 无效或过大"))
		return false
	}
	return true
}

func (s *Service) importHTTP(w http.ResponseWriter, r *http.Request) {
	// 独立导入路径：小说不经过视频供应商素材上传接口。
	r.Body = http.MaxBytesReader(w, r.Body, 21<<20)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		respond(w, nil, fmt.Errorf("请使用表单上传 TXT 或粘贴文字；文件上限 20 MiB"))
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	raw := []byte(r.FormValue("text"))
	file, header, err := r.FormFile("file")
	if err == nil {
		defer file.Close()
		if !strings.HasSuffix(strings.ToLower(header.Filename), ".txt") {
			respond(w, nil, fmt.Errorf("只支持 TXT 文件"))
			return
		}
		raw, err = io.ReadAll(io.LimitReader(file, 20<<20+1))
		if err != nil {
			respond(w, nil, fmt.Errorf("读取小说失败"))
			return
		}
	}
	if len(raw) == 0 || len(raw) > 20<<20 {
		respond(w, nil, fmt.Errorf("小说不能为空或超过 20 MiB"))
		return
	}
	encoding := r.FormValue("encoding")
	if encoding == "" {
		encoding = "auto"
	}
	value, err := s.Import(r.Context(), strings.TrimSpace(r.FormValue("title")), encoding, raw)
	respond(w, value, err)
}

func (s *Service) eventsHTTP(w http.ResponseWriter, r *http.Request) {
	if _, err := s.Run(r.PathValue("id")); err != nil {
		respond(w, nil, err)
		return
	}
	f, ok := w.(http.Flusher)
	if !ok {
		respond(w, nil, fmt.Errorf("不支持 SSE"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	last, _ := strconv.Atoi(r.Header.Get("Last-Event-ID"))
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		run, err := s.Run(r.PathValue("id"))
		if err != nil {
			return
		}
		// 使用持久化快照序号。断线后发最新快照即可恢复状态，无需假装重放中间进度。
		if run.Sequence > last {
			b, _ := json.Marshal(run)
			fmt.Fprintf(w, "id: %d\nevent: agent.updated\ndata: %s\n\n", run.Sequence, b)
			last = run.Sequence
		} else {
			fmt.Fprint(w, ": heartbeat\n\n")
		}
		f.Flush()
		if run.Status != "running" && run.Status != "pausing" {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) exportHTTP(w http.ResponseWriter, r *http.Request) {
	p, err := s.Get(r.PathValue("id"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	selected := r.URL.Query().Get("episodeId")
	docs := []Document{}
	for _, id := range []string{"bible", "outline"} {
		if d, err := s.Document(p.ID, id, 0); err == nil {
			docs = append(docs, d)
		}
	}
	if d, err := s.Document(p.ID, "outline", 0); err == nil {
		var outline Outline
		_ = json.Unmarshal(d.Content, &outline)
		for _, ep := range outline.Episodes {
			if selected == "" || ep.ID == selected {
				if d, err := s.Document(p.ID, ep.ID, 0); err == nil {
					docs = append(docs, d)
				}
			}
		}
	}
	format := r.URL.Query().Get("format")
	if format != "json" && format != "markdown" {
		respond(w, nil, fmt.Errorf("format 只支持 json 或 markdown"))
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.%s"`, p.ID, map[string]string{"json": "json", "markdown": "md"}[format]))
	if format == "json" {
		respond(w, map[string]any{"schemaVersion": 1, "project": p, "documents": docs}, nil)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprintf(w, "# %s\n\n目标每集 %d 秒（估算）。导出最新草稿，请核对审核状态。\n", p.Title, p.TargetSeconds)
	for _, d := range docs {
		fmt.Fprintf(w, "\n## %s · 版本 %d\n\n", d.ID, d.Revision)
		var value any
		_ = json.Unmarshal(d.Content, &value)
		writeMarkdown(w, value, 0)
	}
}

// JSON 保留机器协议，Markdown 面向阅读；不将 HTML 插入浏览器渲染。
func writeMarkdown(w io.Writer, value any, depth int) {
	switch v := value.(type) {
	case map[string]any:
		keys := []string{"title", "logline", "world", "plot", "ending", "characters", "uncertainties", "episodes", "scenes", "endingState", "warnings"}
		used := map[string]bool{}
		for _, key := range keys {
			if item, ok := v[key]; ok {
				fmt.Fprintf(w, "%s- **%s**: ", strings.Repeat("  ", depth), key)
				writeMarkdown(w, item, depth+1)
				used[key] = true
			}
		}
		for key, item := range v {
			if !used[key] {
				fmt.Fprintf(w, "%s- **%s**: ", strings.Repeat("  ", depth), key)
				writeMarkdown(w, item, depth+1)
			}
		}
	case []any:
		fmt.Fprintln(w)
		for _, item := range v {
			fmt.Fprintf(w, "%s- ", strings.Repeat("  ", depth))
			writeMarkdown(w, item, depth+1)
		}
	default:
		fmt.Fprintf(w, "%v\n", v)
	}
}
