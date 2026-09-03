package novel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"video-generator/services/api/internal/agents/core"
)

// Service 持有短临界区锁：仅保护状态读写，网络调用绝不能持锁等待。
// source.txt、文档历史、任务检查点分别保存，不将原文塞入每次 SSE 响应。
type Service struct {
	mu sync.Mutex
	// 单个 Go 服务中的模型步骤串行执行，避免同时分析多本书触发大量并发费用。
	providerMu sync.Mutex
	root       string
	worker     core.Executor
	live       map[string]bool
}

func New(dataDir string, worker core.Executor) (*Service, error) {
	s := &Service{root: filepath.Join(dataDir, "agents", "novel"), worker: worker, live: map[string]bool{}}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return nil, err
	}
	// 重启不会自动重新请求供应商。已完成检查点可复用，状态不明的步骤需人工继续。
	files, err := filepath.Glob(filepath.Join(s.root, "runs", "*.json"))
	if err != nil {
		return nil, err
	}
	for _, path := range files {
		var run core.Run
		if err := core.ReadJSON(path, &run); err != nil {
			return nil, err
		}
		if core.Active(run.Status) {
			run.Status = "paused"
			run.Error = "服务曾中断；检查点已保留，未完成调用可能已经计费，请确认后继续"
			if err := s.saveRun(&run); err != nil {
				return nil, err
			}
		}
	}
	return s, nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func (s *Service) projectPath(id string) string {
	return filepath.Join(s.root, "projects", id, "project.json")
}
func (s *Service) sourcePath(id string) string {
	return filepath.Join(s.root, "projects", id, "source.txt")
}
func (s *Service) documentPath(pid, id string, rev int) string {
	return filepath.Join(s.root, "projects", pid, "documents", id, fmt.Sprintf("%06d.json", rev))
}
func (s *Service) runPath(id string) string { return filepath.Join(s.root, "runs", id+".json") }
func (s *Service) checkpointPath(pid, key string) string {
	return filepath.Join(s.root, "projects", pid, "checkpoints", key+".json")
}

func (s *Service) loadProject(id string) (Project, error) {
	var p Project
	if !core.ValidID(id) {
		return p, errors.New("项目 ID 无效")
	}
	err := core.ReadJSON(s.projectPath(id), &p)
	return p, err
}

func (s *Service) saveProject(p *Project) error {
	p.UpdatedAt = now()
	return core.WriteJSON(s.projectPath(p.ID), p)
}

func (s *Service) Get(id string) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadProject(id)
}

func (s *Service) List() ([]Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	files, err := filepath.Glob(filepath.Join(s.root, "projects", "*", "project.json"))
	if err != nil {
		return nil, err
	}
	items := []Project{}
	for _, path := range files {
		var p Project
		if err := core.ReadJSON(path, &p); err != nil {
			return nil, err
		}
		// 列表不返回长篇章节索引，详情页再加载。
		p.Chapters = nil
		p.Sources = nil
		items = append(items, p)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt > items[j].UpdatedAt })
	return items, nil
}

func (s *Service) Import(ctx context.Context, title, encoding string, raw []byte) (Project, error) {
	var parsed struct {
		Text     string    `json:"text"`
		Encoding string    `json:"encoding"`
		Chapters []Chapter `json:"chapters"`
		Warnings []string  `json:"warnings"`
	}
	if err := s.worker.Call(ctx, "/v1/agents/novel/import", map[string]any{"content": base64.StdEncoding.EncodeToString(raw), "encoding": encoding}, &parsed); err != nil {
		return Project{}, err
	}
	if title == "" {
		title = "未命名小说"
	}
	if len([]rune(title)) > 120 {
		return Project{}, errors.New("书名最多 120 字符")
	}
	p := Project{ID: core.ID("novel"), Title: title, Revision: 1, CharacterCount: len([]rune(parsed.Text)), Encoding: parsed.Encoding,
		Chapters: parsed.Chapters, TargetSeconds: 120, Documents: map[string]DocumentRef{}, Warnings: parsed.Warnings, RunIDs: []string{}, CreatedAt: now()}
	p.Sources = splitSources([]rune(parsed.Text), p.Chapters)
	if err := core.WriteFile(s.sourcePath(p.ID), []byte(parsed.Text)); err != nil {
		return p, err
	}
	err := s.saveProject(&p)
	return p, err
}

// splitSources 给每段原文稳定编号；超长章节也会再切块，完整覆盖 [0,len(text))。
func splitSources(text []rune, chapters []Chapter) []Source {
	parts := []Source{}
	for _, c := range chapters {
		for start := c.Start; start < c.End; {
			end := min(start+2400, c.End)
			if end < c.End {
				for i := end - 1; i > start+1200; i-- {
					if text[i] == '\n' {
						end = i + 1
						break
					}
				}
			}
			parts = append(parts, Source{ID: fmt.Sprintf("source-%04d", len(parts)+1), ChapterID: c.ID, Start: start, End: end})
			start = end
		}
	}
	return parts
}

func (s *Service) Source(id, sourceID string) (map[string]any, error) {
	p, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.sourcePath(id))
	if err != nil {
		return nil, err
	}
	runes := []rune(string(data))
	for _, part := range p.Sources {
		if part.ID == sourceID {
			return map[string]any{"source": part, "text": string(runes[part.Start:part.End])}, nil
		}
	}
	return nil, errors.New("原文片段不存在")
}

func (s *Service) loadDocument(p Project, id string, rev int) (Document, error) {
	var d Document
	if !core.ValidID(id) {
		return d, errors.New("文档 ID 无效")
	}
	if rev == 0 {
		rev = p.Documents[id].Current
	}
	if rev < 1 || rev > p.Documents[id].Current {
		return d, errors.New("文档版本不存在")
	}
	err := core.ReadJSON(s.documentPath(p.ID, id, rev), &d)
	return d, err
}

func (s *Service) Document(pid, id string, rev int) (Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.loadProject(pid)
	if err != nil {
		return Document{}, err
	}
	return s.loadDocument(p, id, rev)
}

// writeDocument 只创建新版本；引用索引最后由调用方提交。
func (s *Service) writeDocument(p *Project, id string, content json.RawMessage, origin string) error {
	ref := p.Documents[id]
	d := Document{ID: id, Revision: ref.Current + 1, Content: content, Origin: origin, CreatedAt: now()}
	if err := core.WriteJSON(s.documentPath(p.ID, id, d.Revision), d); err != nil {
		return err
	}
	ref.Current = d.Revision
	ref.Stale = false
	p.Documents[id] = ref
	return nil
}

func (s *Service) markDownstream(p *Project, id string) {
	for key, ref := range p.Documents {
		if (id == "bible" && key != "bible") || (id == "outline" && key != "bible" && key != "outline") || (id != "bible" && id != "outline" && key > id && key != "outline") {
			ref.Stale = true
			p.Documents[key] = ref
		}
	}
}
