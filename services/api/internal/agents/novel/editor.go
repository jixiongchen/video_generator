package novel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type SettingsInput struct {
	Revision          int       `json:"revision"`
	Title             string    `json:"title"`
	TargetSeconds     int       `json:"targetSeconds"`
	TargetEpisodes    int       `json:"targetEpisodes"`
	ChaptersConfirmed bool      `json:"chaptersConfirmed"`
	Chapters          []Chapter `json:"chapters"`
}

func (s *Service) Settings(pid string, input SettingsInput) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.loadProject(pid)
	if err != nil {
		return p, err
	}
	if p.Revision != input.Revision {
		return p, errors.New("版本冲突，请刷新项目")
	}
	if s.busy(p) {
		return p, errors.New("请暂停任务并等当前步骤完成后，再修改设置")
	}
	if strings.TrimSpace(input.Title) == "" || len([]rune(input.Title)) > 120 || input.TargetSeconds < 30 || input.TargetSeconds > 600 || input.TargetEpisodes < 0 || input.TargetEpisodes > 1000 {
		return p, errors.New("书名不能为空；目标时长为 30–600 秒；集数为 0（自动）或 1–1000")
	}
	if input.TargetEpisodes > len(p.Sources)*12 {
		return p, errors.New("目标集数过多；每个原文片段最多规划 12 集，请减少目标集数或选择自动推荐")
	}
	if len(input.Chapters) > 0 {
		if p.Documents["analysis"].Current > 0 {
			return p, errors.New("已开始分析的项目不能重排原文引用；需要调整章节时请重新导入")
		}
		end := 0
		seen := map[string]bool{}
		for _, c := range input.Chapters {
			if c.Start != end || c.End <= c.Start || c.End > p.CharacterCount || c.Title == "" || !validChapterID(c.ID) || seen[c.ID] {
				return p, errors.New("章节必须连续完整覆盖原文，ID 唯一且标题非空")
			}
			end = c.End
			seen[c.ID] = true
		}
		if end != p.CharacterCount {
			return p, errors.New("章节末尾必须等于全文字符数")
		}
		data, err := os.ReadFile(s.sourcePath(pid))
		if err != nil {
			return p, err
		}
		p.Chapters = input.Chapters
		p.Sources = splitSources([]rune(string(data)), p.Chapters)
	}
	if p.TargetSeconds != input.TargetSeconds || p.TargetEpisodes != input.TargetEpisodes {
		ref := p.Documents["outline"]
		if ref.Current > 0 {
			ref.Stale = true
			p.Documents["outline"] = ref
		}
		s.markDownstream(&p, "outline")
	}
	p.Title = input.Title
	p.TargetSeconds = input.TargetSeconds
	p.TargetEpisodes = input.TargetEpisodes
	p.ChaptersConfirmed = input.ChaptersConfirmed
	p.Revision++
	err = s.saveProject(&p)
	return p, err
}

func validChapterID(id string) bool {
	if len(id) > 100 || id == "" {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

type EditInput struct {
	Revision        int             `json:"revision"`
	ProjectRevision int             `json:"projectRevision"`
	Content         json.RawMessage `json:"content"`
}

func (s *Service) Edit(ctx context.Context, pid, id string, input EditInput, approve bool) (Project, error) {
	p, err := s.Get(pid)
	if err != nil {
		return p, err
	}
	operation := documentOperation(id)
	if operation == "" {
		return p, errors.New("不支持编辑该文档")
	}
	if p.Revision != input.ProjectRevision || p.Documents[id].Current != input.Revision {
		return p, errors.New("版本冲突，请刷新后合并修改")
	}
	contextData := map[string]any{"manualFullOutline": true}
	refs := []string{}
	for _, source := range p.Sources {
		refs = append(refs, source.ID)
	}
	contextData["sourceIds"] = refs
	if operation == "script" {
		bible, err := s.Document(pid, "bible", 0)
		if err != nil {
			return p, err
		}
		contextData["bible"] = bible.Content
	}
	if approve {
		d, err := s.Document(pid, id, input.Revision)
		if err != nil {
			return p, err
		}
		input.Content = d.Content
	}
	var validated map[string]any
	if err := s.worker.Call(ctx, "/v1/agents/novel/validate", map[string]any{"operation": operation, "input": contextData, "document": input.Content}, &validated); err != nil {
		return p, err
	}
	if id == "outline" {
		var outline Outline
		if err := json.Unmarshal(input.Content, &outline); err != nil {
			return p, err
		}
		for i, ep := range outline.Episodes {
			if ep.ID != fmt.Sprintf("episode-%04d", i+1) {
				return p, errors.New("集 ID 必须按顺序连续，不能改写稳定 ID；请通过重新规划调整集数")
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err = s.loadProject(pid)
	if err != nil {
		return p, err
	}
	if s.busy(p) {
		return p, errors.New("任务运行中，请暂停并等待当前步骤结束后编辑")
	}
	if p.Revision != input.ProjectRevision || p.Documents[id].Current != input.Revision {
		return p, errors.New("版本冲突，校验期间稿件已变化")
	}
	if approve {
		if id != "bible" && !confirmed(p, "bible") {
			return p, errors.New("请先确认最新故事资料")
		}
		if operation == "script" && !confirmed(p, "outline") {
			return p, errors.New("请先确认最新分集大纲")
		}
		ref := p.Documents[id]
		ref.Approved = ref.Current
		ref.Stale = false
		p.Documents[id] = ref
	} else {
		if err := s.writeDocument(&p, id, input.Content, "manual"); err != nil {
			return p, err
		}
		s.markDownstream(&p, id)
	}
	p.Revision++
	err = s.saveProject(&p)
	return p, err
}

// Checks 是确定性校验，不冒充模型能证明“绝对忠实原著”。原文覆盖、引用、
// 时长是可计算的；人物动机和改编质量仍需要人在右侧对照原文审核。
func (s *Service) Checks(pid string) ([]string, error) {
	p, err := s.Get(pid)
	if err != nil {
		return nil, err
	}
	warnings := append([]string{}, p.Warnings...)
	if d, err := s.Document(pid, "outline", 0); err == nil {
		var outline Outline
		_ = json.Unmarshal(d.Content, &outline)
		covered := map[string]bool{}
		for _, ep := range outline.Episodes {
			for _, id := range ep.SourceIDs {
				covered[id] = true
			}
			if d, err := s.Document(pid, ep.ID, 0); err == nil {
				var script struct {
					Scenes []struct {
						EstimatedSeconds int `json:"estimatedSeconds"`
					} `json:"scenes"`
				}
				_ = json.Unmarshal(d.Content, &script)
				seconds := 0
				for _, scene := range script.Scenes {
					seconds += scene.EstimatedSeconds
				}
				if seconds < p.TargetSeconds*8/10 || seconds > p.TargetSeconds*12/10 {
					warnings = append(warnings, fmt.Sprintf("%s 估算 %d 秒，与目标 %d 秒偏差超过 20%%", ep.ID, seconds, p.TargetSeconds))
				}
			}
		}
		missing := 0
		for _, src := range p.Sources {
			if !covered[src.ID] {
				missing++
			}
		}
		if missing > 0 {
			warnings = append(warnings, fmt.Sprintf("有 %d 个原文片段没有被分集引用，请确认是有意删减还是遗漏", missing))
		}
	}
	for id, ref := range p.Documents {
		if ref.Stale && id != "analysis" {
			warnings = append(warnings, id+" 的上游已修改，需要复核")
		}
	}
	return warnings, nil
}
