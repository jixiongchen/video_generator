package novel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"video-generator/services/api/internal/agents/core"
)

// fakeWorker 只在 _test.go 中存在。生产服务不提供 Mock 开关，防止误把演示当生成。
type fakeWorker struct {
	mu      sync.Mutex
	calls   map[string]int
	block   chan struct{}
	entered chan struct{}
	fail    bool
}

func fixtureBible() map[string]any {
	return map[string]any{"title": "山城来信", "logline": "送信人寻找失散的家人", "world": "山城", "plot": "发现线索，寻找家人", "ending": "团聚", "characters": []any{map[string]any{"id": "character-001", "name": "林舟", "aliases": []string{}, "description": "送信人", "motivation": "找回家人"}}, "uncertainties": []string{}}
}

func (f *fakeWorker) Call(ctx context.Context, path string, input any, output any) error {
	encode := func(v any) error { b, _ := json.Marshal(v); return json.Unmarshal(b, output) }
	if path == "/v1/agents/config" {
		return encode(map[string]any{"configured": true, "model": "test-only-qwen", "protocol": "openai_chat"})
	}
	b, _ := json.Marshal(input)
	var req map[string]any
	_ = json.Unmarshal(b, &req)
	if strings.HasSuffix(path, "/import") {
		raw, _ := base64.StdEncoding.DecodeString(req["content"].(string))
		text := string(raw)
		return encode(map[string]any{"text": text, "encoding": "utf-8", "chapters": []Chapter{{ID: "chapter-0001", Title: "第一章", Start: 0, End: len([]rune(text))}}, "warnings": []string{}})
	}
	if strings.HasSuffix(path, "/validate") {
		return encode(map[string]any{"output": req["document"]})
	}
	op := req["operation"].(string)
	data := req["input"].(map[string]any)
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[op]++
	block, entered, fail := f.block, f.entered, f.fail
	f.mu.Unlock()
	if fail {
		return errors.New("模拟供应商失败")
	}
	if block != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-block
	}
	var value any
	switch op {
	case "summarize", "reduce":
		value = map[string]any{"summary": "林舟拿到一封旧信，在山城车站寻找家人，经历误会后获得线索，最后团聚。"}
	case "bible":
		value = fixtureBible()
	case "outline":
		count := int(data["targetCount"].(float64))
		if count == 0 {
			count = 6
		}
		episodes := []map[string]any{}
		for i := 0; i < count; i++ {
			episodes = append(episodes, map[string]any{"title": fmt.Sprintf("来信 %d", i+1), "goal": "找到线索", "conflict": "家人失散", "summary": "林舟沿着线索前往车站", "hook": "旧信上出现新的地址", "bridge": "前往下一站", "estimatedSeconds": 120, "sourceIds": data["sourceIds"], "changes": []string{"压缩日常描写"}})
		}
		value = map[string]any{"episodes": episodes}
	case "script":
		value = map[string]any{"title": "车站的旧信", "scenes": []any{map[string]any{"id": "scene-001", "location": "车站", "time": "清晨", "characters": []string{"character-001"}, "purpose": "发现线索", "action": "林舟展开一封泛黄的信", "dialogue": []any{map[string]string{"characterId": "character-001", "text": "我会找到你。"}}, "narration": "", "sound": "列车驶过", "estimatedSeconds": 120, "continuity": "林舟还不知道写信人的身份", "sourceIds": data["sourceIds"], "changes": []string{}}}, "endingState": "林舟带着旧信离开车站，尚未获知家人位置", "warnings": []string{}}
	default:
		return errors.New("未知测试步骤")
	}
	return encode(map[string]any{"output": value, "usage": map[string]int{"total_tokens": 100}, "version": agentVersion})
}

func setup(t *testing.T) (*Service, *fakeWorker, Project) {
	t.Helper()
	f := &fakeWorker{}
	s, err := New(t.TempDir(), f)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Import(context.Background(), "山城来信", "auto", []byte("第一章 来信\n林舟在车站打开旧信，决定寻找家人。"))
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Settings(p.ID, SettingsInput{Revision: p.Revision, Title: p.Title, TargetSeconds: 120, TargetEpisodes: 6, ChaptersConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	return s, f, p
}

func waitRun(t *testing.T, s *Service, id string) core.Run {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r, err := s.Run(id)
		if err != nil {
			t.Fatal(err)
		}
		if !core.Active(r.Status) {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("任务未在测试期限内结束")
	return core.Run{}
}

func approveDoc(t *testing.T, s *Service, pid, id string) Project {
	t.Helper()
	p, _ := s.Get(pid)
	p, err := s.Edit(context.Background(), pid, id, EditInput{Revision: p.Documents[id].Current, ProjectRevision: p.Revision}, true)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func startStage(t *testing.T, s *Service, p Project, stage string) core.Run {
	t.Helper()
	run, err := s.Start(context.Background(), p.ID, StartInput{Stage: stage, Revision: p.Revision, Consent: true})
	if err != nil {
		t.Fatal(err)
	}
	result := waitRun(t, s, run.ID)
	if result.Status != "succeeded" {
		t.Fatalf("%s: %s", result.Status, result.Error)
	}
	return result
}

func TestFullWorkflowAndVersionProtection(t *testing.T) {
	s, _, p := setup(t)
	startStage(t, s, p, "analyze")
	p = approveDoc(t, s, p.ID, "bible")
	startStage(t, s, p, "outline")
	p = approveDoc(t, s, p.ID, "outline")
	startStage(t, s, p, "script")
	p, _ = s.Get(p.ID)
	if _, err := s.Start(context.Background(), p.ID, StartInput{Stage: "script", Revision: p.Revision, Consent: true}); err == nil {
		t.Fatal("未确认试写稿就开始批量")
	}
	p = approveDoc(t, s, p.ID, "episode-0001")
	startStage(t, s, p, "script")
	p, _ = s.Get(p.ID)
	if p.Documents["episode-0006"].Current != 1 {
		t.Fatal("未生成第2–6集")
	}
	d, _ := s.Document(p.ID, "bible", 0)
	p, err := s.Edit(context.Background(), p.ID, "bible", EditInput{Revision: d.Revision, ProjectRevision: p.Revision, Content: d.Content}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Documents["outline"].Stale || !p.Documents["episode-0001"].Stale {
		t.Fatal("下游没有失效")
	}
	if p.Documents["bible"].Approved != 1 || p.Documents["bible"].Current != 2 {
		t.Fatal("确认版本被覆盖")
	}
	if _, err := s.Document(p.ID, "bible", 1); err != nil {
		t.Fatal("旧版本丢失")
	}
}

func TestPauseResumeCacheAndDuplicateStart(t *testing.T) {
	s, f, p := setup(t)
	f.block = make(chan struct{})
	f.entered = make(chan struct{}, 1)
	in := StartInput{Stage: "analyze", Revision: p.Revision, Consent: true}
	r, err := s.Start(context.Background(), p.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	<-f.entered
	duplicate, err := s.Start(context.Background(), p.ID, in)
	if err != nil || duplicate.ID != r.ID {
		t.Fatal("重复创建付费任务")
	}
	if _, err := s.Control(context.Background(), r.ID, "pause"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	block := f.block
	f.block = nil
	f.mu.Unlock()
	close(block)
	paused := waitRun(t, s, r.ID)
	if paused.Status != "paused" {
		t.Fatal(paused.Status)
	}
	if _, err := s.Control(context.Background(), r.ID, "resume"); err != nil {
		t.Fatal(err)
	}
	if result := waitRun(t, s, r.ID); result.Status != "succeeded" {
		t.Fatal(result.Error)
	}
	f.mu.Lock()
	count := f.calls["summarize"]
	f.mu.Unlock()
	if count != 1 {
		t.Fatalf("完成的原文重新计费调用: %d", count)
	}
}

func TestRestartDoesNotAutoResubmit(t *testing.T) {
	s, f, p := setup(t)
	r := core.Run{ID: core.ID("run"), ProjectID: p.ID, Status: "running", Steps: map[string]core.Step{}}
	if err := s.saveRun(&r); err != nil {
		t.Fatal(err)
	}
	restored, err := New(strings.TrimSuffix(s.root, string(os.PathSeparator)+"agents"+string(os.PathSeparator)+"novel"), f)
	if err != nil {
		t.Fatal(err)
	}
	got, err := restored.Run(r.ID)
	if err != nil || got.Status != "paused" {
		t.Fatal("没有恢复为待确认状态", err)
	}
}

func TestMillionCharacterCoverage(t *testing.T) {
	text := []rune(strings.Repeat("文😀", 500000))
	parts := splitSources(text, []Chapter{{ID: "chapter-1", Start: 0, End: len(text)}})
	end := 0
	for _, p := range parts {
		if p.Start != end || p.End-p.Start > 2400 {
			t.Fatal("分块遗漏或越界")
		}
		end = p.End
	}
	if end != 1000000 {
		t.Fatal(end)
	}
}

func TestCancelDiscardsLatePublication(t *testing.T) {
	s, f, p := setup(t)
	f.block = make(chan struct{})
	f.entered = make(chan struct{}, 1)
	r, err := s.Start(context.Background(), p.ID, StartInput{Stage: "analyze", Revision: p.Revision, Consent: true})
	if err != nil {
		t.Fatal(err)
	}
	<-f.entered
	_, _ = s.Control(context.Background(), r.ID, "cancel")
	close(f.block)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		live := s.live[r.ID]
		s.mu.Unlock()
		if !live {
			break
		}
		time.Sleep(time.Millisecond)
	}
	p, _ = s.Get(p.ID)
	if p.Documents["bible"].Current != 0 {
		t.Fatal("取消后仍发布新产物")
	}
}

// TestBrowserHarness 是显式 opt-in 的隔离浏览器验收入口，仅提供合成小说和 Mock。
// 默认 go test 会跳过。数据库在 t.TempDir，绝不访问用户 data 或外部供应商。
func TestBrowserHarness(t *testing.T) {
	addr := os.Getenv("NOVEL_BROWSER_TEST_ADDR")
	if addr == "" {
		t.Skip("browser harness not requested")
	}
	s, _, _ := setup(t)
	mux := http.NewServeMux()
	s.Register(mux)
	mux.HandleFunc("GET /api/v1/generations", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"items":[]}`) })
	mux.HandleFunc("GET /api/v1/config", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"defaults":{},"capabilities":{}}`) })
	mux.Handle("GET /", http.FileServer(http.Dir(os.Getenv("NOVEL_BROWSER_WEB_DIST"))))
	t.Log("Isolated test workspace:", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		t.Fatal(err)
	}
}

func TestIdempotencyKeyAfterCompletion(t *testing.T) {
	s, f, p := setup(t)
	input := StartInput{RequestID: "intent-001", Stage: "analyze", Revision: p.Revision, Consent: true}
	first, err := s.Start(context.Background(), p.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if r := waitRun(t, s, first.ID); r.Status != "succeeded" {
		t.Fatal(r.Error)
	}
	second, err := s.Start(context.Background(), p.ID, input)
	if err != nil || second.ID != first.ID {
		t.Fatal("完成请求重传没有幂等")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls["bible"] != 1 {
		t.Fatal("重复执行了已完成意图")
	}
}

func TestSceneMergePreservesOtherScenes(t *testing.T) {
	old := json.RawMessage(`{"title":"原稿","scenes":[{"id":"scene-001","action":"人工修改 A"},{"id":"scene-002","action":"人工修改 B"}],"endingState":"原始结束状态","warnings":[]}`)
	candidate := json.RawMessage(`{"scenes":[{"id":"scene-001","action":"新场景"},{"id":"scene-002","action":"不应采用的改写"}],"endingState":"错误状态"}`)
	merged, err := mergeScene(old, candidate, "scene-001")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(merged), "人工修改 B") || strings.Contains(string(merged), "不应采用") || strings.Contains(string(merged), "错误状态") {
		t.Fatal(string(merged))
	}
}

func TestStaleEditorCannotOverwriteNewVersion(t *testing.T) {
	s, _, p := setup(t)
	startStage(t, s, p, "analyze")
	p, _ = s.Get(p.ID)
	d, _ := s.Document(p.ID, "bible", 0)
	input := EditInput{Revision: d.Revision, ProjectRevision: p.Revision, Content: d.Content}
	if _, err := s.Edit(context.Background(), p.ID, "bible", input, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Edit(context.Background(), p.ID, "bible", input, false); err == nil {
		t.Fatal("旧页面覆盖新版本")
	}
}
