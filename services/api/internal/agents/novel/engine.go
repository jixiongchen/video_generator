package novel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"video-generator/services/api/internal/agents/core"
)

const agentVersion = "novel-v1.0"

var errStopped = errors.New("任务在步骤边界停止")

type StartInput struct {
	RequestID   string   `json:"requestId"`
	Stage       string   `json:"stage"`
	Targets     []string `json:"targets"`
	Instruction string   `json:"instruction"`
	SceneID     string   `json:"sceneId"`
	Revision    int      `json:"revision"`
	Consent     bool     `json:"consent"`
}

type stepResult struct {
	Output  json.RawMessage `json:"output"`
	Usage   json.RawMessage `json:"usage"`
	Version string          `json:"version"`
}

type analysisData struct {
	Summaries map[string]json.RawMessage `json:"summaries"`
	Root      json.RawMessage            `json:"root"`
}

func (s *Service) Config(ctx context.Context) (map[string]any, error) {
	var result map[string]any
	err := s.worker.Call(ctx, "/v1/agents/config", nil, &result)
	return result, err
}

func (s *Service) saveRun(run *core.Run) error {
	run.Sequence++
	run.UpdatedAt = now()
	return core.WriteJSON(s.runPath(run.ID), run)
}

func (s *Service) loadRun(id string) (core.Run, error) {
	var run core.Run
	if !core.ValidID(id) {
		return run, errors.New("任务 ID 无效")
	}
	err := core.ReadJSON(s.runPath(id), &run)
	return run, err
}

func (s *Service) Run(id string) (core.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadRun(id)
}

func (s *Service) busy(p Project) bool {
	for _, id := range p.RunIDs {
		if s.live[id] {
			return true
		}
	}
	return false
}

func confirmed(p Project, id string) bool {
	r := p.Documents[id]
	return r.Current > 0 && r.Current == r.Approved && !r.Stale
}

// Start 先持久化用户选择和输入版本，再启动后台协程；浏览器关闭不影响任务。
// 每个项目同一时刻只有一个运行，防止两次改编互相覆盖。
func (s *Service) Start(ctx context.Context, pid string, input StartInput) (core.Run, error) {
	config, err := s.Config(ctx)
	if err != nil {
		return core.Run{}, err
	}
	if config["configured"] != true {
		return core.Run{}, errors.New("文本模型尚未配置，请先确认接口协议并配置环境变量")
	}
	if !input.Consent {
		return core.Run{}, errors.New("请确认拥有改编权限，并同意把相关正文发送至配置的文本服务")
	}
	if len([]rune(input.Instruction)) > 1000 {
		return core.Run{}, errors.New("修改要求最多 1000 字符")
	}
	if input.RequestID != "" && !core.ValidID(input.RequestID) {
		return core.Run{}, errors.New("requestId 格式无效")
	}
	if input.SceneID != "" && (input.Stage != "script" || len(input.Targets) != 1) {
		return core.Run{}, errors.New("局部场景重写必须指定一集")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.loadProject(pid)
	if err != nil {
		return core.Run{}, err
	}
	// 幂等键由浏览器为一次用户意图生成；重传同一请求可取回已完成任务。
	if input.RequestID != "" {
		for _, id := range p.RunIDs {
			old, err := s.loadRun(id)
			if err != nil {
				return core.Run{}, err
			}
			if old.RequestID == input.RequestID {
				return old, nil
			}
		}
	}
	if p.Revision != input.Revision {
		return core.Run{}, errors.New("项目版本已变化，请刷新后重试")
	}
	if s.busy(p) {
		for _, id := range p.RunIDs {
			if s.live[id] {
				old, err := s.loadRun(id)
				if err != nil {
					return old, err
				}
				if old.Stage == input.Stage && old.Instruction == input.Instruction && old.SceneID == input.SceneID {
					return old, nil
				}
				return core.Run{}, errors.New("已有不同任务运行，请等待完成或暂停")
			}
		}
	}
	if !p.ChaptersConfirmed {
		return core.Run{}, errors.New("请先检查编码和章节边界，并确认导入")
	}
	switch input.Stage {
	case "analyze":
	case "outline":
		if !confirmed(p, "bible") {
			return core.Run{}, errors.New("请先确认最新故事资料")
		}
	case "script":
		if !confirmed(p, "bible") || !confirmed(p, "outline") {
			return core.Run{}, errors.New("请先确认最新故事资料和分集大纲")
		}
		d, err := s.loadDocument(p, "outline", 0)
		if err != nil {
			return core.Run{}, err
		}
		var outline Outline
		if err := json.Unmarshal(d.Content, &outline); err != nil {
			return core.Run{}, err
		}
		if len(outline.Episodes) == 0 {
			return core.Run{}, errors.New("分集大纲为空")
		}
		if len(input.Targets) == 0 {
			for i, e := range outline.Episodes {
				if p.Documents[e.ID].Current == 0 {
					input.Targets = append(input.Targets, e.ID)
				}
				if i == 0 && len(input.Targets) > 0 {
					break
				}
				if len(input.Targets) == 5 {
					break
				}
			}
		}
		if len(input.Targets) == 0 || len(input.Targets) > 5 {
			return core.Run{}, errors.New("请指定 1–5 集；已有剧本请使用重写操作")
		}
		seen := map[string]bool{}
		previousIndex := -1
		for _, id := range input.Targets {
			idx := -1
			for i, e := range outline.Episodes {
				if e.ID == id {
					idx = i
					break
				}
			}
			if idx < 0 || seen[id] || idx <= previousIndex {
				return core.Run{}, errors.New("目标集必须存在、不重复，并按集数顺序排列")
			}
			if idx > 0 && !confirmed(p, outline.Episodes[0].ID) {
				return core.Run{}, errors.New("请先确认第 1 集试写稿，再批量生成")
			}
			if idx > 0 && !seen[outline.Episodes[idx-1].ID] && !confirmed(p, outline.Episodes[idx-1].ID) {
				return core.Run{}, errors.New("请先确认前一集，或将它包含在同一批次，保证剧情衔接")
			}
			seen[id] = true
			previousIndex = idx
		}
		if input.SceneID != "" {
			d, err := s.loadDocument(p, input.Targets[0], 0)
			if err != nil {
				return core.Run{}, err
			}
			var script struct {
				Scenes []struct {
					ID string `json:"id"`
				} `json:"scenes"`
			}
			_ = json.Unmarshal(d.Content, &script)
			found := false
			for _, scene := range script.Scenes {
				if scene.ID == input.SceneID {
					found = true
				}
			}
			if !found {
				return core.Run{}, errors.New("指定场景不存在，未创建任务")
			}
		}
	default:
		return core.Run{}, errors.New("stage 只支持 analyze、outline、script")
	}
	run := core.Run{ID: core.ID("run"), RequestID: input.RequestID, Agent: "novel", ProjectID: pid, Stage: input.Stage, Status: "running", Steps: map[string]core.Step{},
		Targets: input.Targets, Instruction: input.Instruction, SceneID: input.SceneID, InputRevision: p.Revision, CreatedAt: now()}
	run.Model, _ = config["model"].(string)
	run.Protocol, _ = config["protocol"].(string)
	if err := s.saveRun(&run); err != nil {
		return run, err
	}
	p.RunIDs = append(p.RunIDs, run.ID)
	if err := s.saveProject(&p); err != nil {
		return run, err
	}
	s.live[run.ID] = true
	go s.execute(run.ID)
	return run, nil
}

func (s *Service) Control(ctx context.Context, id, action string) (core.Run, error) {
	var config map[string]any
	if action == "resume" {
		var err error
		config, err = s.Config(ctx)
		if err != nil {
			return core.Run{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.loadRun(id)
	if err != nil {
		return r, err
	}
	switch action {
	case "pause":
		if r.Status == "running" {
			r.Status = "pausing"
		}
	case "cancel":
		if r.Status != "succeeded" {
			r.Status = "canceled"
			r.Error = "已取消后续调用；在途请求可能仍计费"
		}
	case "resume":
		if s.live[id] || (r.Status != "paused" && r.Status != "failed") {
			return r, errors.New("只有暂停或失败且已停止的任务可以继续")
		}
		p, err := s.loadProject(r.ProjectID)
		if err != nil {
			return r, err
		}
		if p.Revision != r.InputRevision {
			return r, errors.New("输入已修改，请新建任务；旧检查点仍会按内容哈希复用")
		}
		if len(p.RunIDs) == 0 || p.RunIDs[len(p.RunIDs)-1] != id {
			return r, errors.New("已有较新的任务，旧任务不能继续覆盖新稿；请新建任务复用检查点")
		}
		if s.busy(p) {
			return r, errors.New("该小说已有任务运行")
		}
		if config["configured"] != true || config["model"] != r.Model || config["protocol"] != r.Protocol {
			return r, errors.New("文本配置未就绪或模型已变化，请新建任务")
		}
		r.Status = "running"
		r.Error = ""
	default:
		return r, errors.New("不支持的任务操作")
	}
	if err := s.saveRun(&r); err != nil {
		return r, err
	}
	if action == "resume" {
		s.live[id] = true
		go s.execute(id)
	}
	return r, nil
}

// step 的缓存键包含原始输入、模型、协议和 Agent 版本。恢复时重新走确定性的
// 流程，命中已完成检查点只读磁盘，不再次调用模型；未确认完成的步骤才会重试。
func (s *Service) step(run core.Run, operation, name string, input any) (json.RawMessage, error) {
	encoded, _ := json.Marshal(map[string]any{"version": agentVersion, "model": run.Model, "protocol": run.Protocol, "operation": operation, "input": input})
	hash := sha256.Sum256(encoded)
	key := hex.EncodeToString(hash[:])
	s.mu.Lock()
	current, err := s.loadRun(run.ID)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if current.Status != "running" {
		s.mu.Unlock()
		return nil, errStopped
	}
	current.Current = name
	err = s.saveRun(&current)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	var result stepResult
	err = core.ReadJSON(s.checkpointPath(run.ProjectID, key), &result)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if os.IsNotExist(err) {
		err = s.callModelStep(run, operation, input, &result)
		if err != nil {
			return nil, err
		}
		if !json.Valid(result.Output) {
			return nil, errors.New("Worker 未返回有效步骤产物")
		}
		if err := core.WriteJSON(s.checkpointPath(run.ProjectID, key), result); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err = s.loadRun(run.ID)
	if err != nil {
		return nil, err
	}
	if current.Status == "canceled" {
		return nil, errStopped
	}
	current.Steps[key] = core.Step{Key: key, Operation: operation, Usage: result.Usage}
	current.Completed = len(current.Steps)
	if err := s.saveRun(&current); err != nil {
		return nil, err
	}
	return result.Output, nil
}

// 网络调用单独串行化，不占用项目状态锁；等候期间仍然可以查询或取消任务。
func (s *Service) callModelStep(run core.Run, operation string, input any, result *stepResult) error {
	s.providerMu.Lock()
	defer s.providerMu.Unlock()
	current, err := s.Run(run.ID)
	if err != nil {
		return err
	}
	if current.Status != "running" {
		return errStopped
	}
	return s.worker.Call(context.Background(), "/v1/agents/novel/steps", map[string]any{
		"operation": operation, "input": input, "expectedModel": run.Model, "expectedProtocol": run.Protocol,
	}, result)
}

// compress 用二叉分层摘要，不把数百章摘要再次塞入同一个模型上下文。
func (s *Service) compress(run core.Run, summaries []json.RawMessage) (json.RawMessage, error) {
	if len(summaries) == 0 {
		return nil, errors.New("没有可合并的原文材料")
	}
	for len(summaries) > 1 {
		next := []json.RawMessage{}
		for i := 0; i < len(summaries); i += 2 {
			if i+1 == len(summaries) {
				next = append(next, summaries[i])
				continue
			}
			output, err := s.step(run, "reduce", "分层整理剧情", map[string]any{"summaries": summaries[i : i+2]})
			if err != nil {
				return nil, err
			}
			next = append(next, output)
		}
		summaries = next
	}
	return summaries[0], nil
}

func (s *Service) execute(id string) {
	run, err := s.Run(id)
	if err == nil {
		var p Project
		p, err = s.Get(run.ProjectID)
		if err == nil {
			switch run.Stage {
			case "analyze":
				err = s.analyze(run, p)
			case "outline":
				err = s.outline(run, p)
			case "script":
				err = s.scripts(run, p)
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	defer delete(s.live, id)
	r, loadErr := s.loadRun(id)
	if loadErr != nil {
		return
	}
	if r.Status == "canceled" {
		return
	}
	if errors.Is(err, errStopped) || r.Status == "pausing" {
		r.Status = "paused"
	} else if err != nil {
		r.Status = "failed"
		r.Error = err.Error()
	} else {
		r.Status = "succeeded"
		r.Current = "本阶段完成，请审核产物"
	}
	// 持久化失败时保留原有 running 状态，重启恢复流程会把它安全地标为暂停。
	_ = s.saveRun(&r)
}

func (s *Service) publish(run core.Run, id string, output json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.loadRun(run.ID)
	if err != nil {
		return err
	}
	if r.Status == "canceled" {
		return errStopped
	}
	p, err := s.loadProject(run.ProjectID)
	if err != nil {
		return err
	}
	if p.Revision != run.InputRevision {
		return errors.New("输入版本已变化，旧结果未覆盖当前稿件")
	}
	if p.Documents[id].Current > 0 {
		d, err := s.loadDocument(p, id, 0)
		if err != nil {
			return err
		}
		if d.Origin == run.ID {
			return nil
		} // 提交后崩溃再恢复，不重复创建版本。
	}
	if err := s.writeDocument(&p, id, output, run.ID); err != nil {
		return err
	}
	if id != "analysis" {
		s.markDownstream(&p, id)
	}
	return s.saveProject(&p)
}

func (s *Service) analyze(run core.Run, p Project) error {
	data, err := os.ReadFile(s.sourcePath(p.ID))
	if err != nil {
		return err
	}
	text := []rune(string(data))
	analysis := analysisData{Summaries: map[string]json.RawMessage{}}
	summaries := []json.RawMessage{}
	for i, src := range p.Sources {
		out, err := s.step(run, "summarize", fmt.Sprintf("阅读原文 %d / %d", i+1, len(p.Sources)), map[string]any{"sourceIds": []string{src.ID}, "text": string(text[src.Start:src.End])})
		if err != nil {
			return err
		}
		analysis.Summaries[src.ID] = out
		summaries = append(summaries, out)
	}
	analysis.Root, err = s.compress(run, summaries)
	if err != nil {
		return err
	}
	encoded, _ := json.Marshal(analysis)
	if err := s.publish(run, "analysis", encoded); err != nil {
		return err
	}
	out, err := s.step(run, "bible", "建立全剧故事资料", map[string]any{"title": p.Title, "material": analysis.Root})
	if err != nil {
		return err
	}
	return s.publish(run, "bible", out)
}

func (s *Service) materials(p Project) (analysisData, json.RawMessage, error) {
	var analysis analysisData
	d, err := s.Document(p.ID, "analysis", 0)
	if err != nil {
		return analysis, nil, err
	}
	if err := json.Unmarshal(d.Content, &analysis); err != nil {
		return analysis, nil, err
	}
	bible, err := s.Document(p.ID, "bible", 0)
	return analysis, bible.Content, err
}

func (s *Service) outline(run core.Run, p Project) error {
	analysis, bible, err := s.materials(p)
	if err != nil {
		return err
	}
	// 将长篇分段规划，再拼成整剧目录；前一集的连接信息传给下一段。
	groups := (len(p.Sources) + 7) / 8
	if p.TargetEpisodes > 0 {
		groups = max((p.TargetEpisodes+11)/12, min(groups, p.TargetEpisodes))
	}
	groups = min(groups, len(p.Sources))
	result := Outline{Episodes: []Episode{}}
	previous := "故事开端"
	for g := 0; g < groups; g++ {
		parts := p.Sources[g*len(p.Sources)/groups : (g+1)*len(p.Sources)/groups]
		ids := []string{}
		summaries := []json.RawMessage{}
		for _, src := range parts {
			ids = append(ids, src.ID)
			summaries = append(summaries, analysis.Summaries[src.ID])
		}
		material, err := s.compress(run, summaries)
		if err != nil {
			return err
		}
		target := 0
		if p.TargetEpisodes > 0 {
			target = (g+1)*p.TargetEpisodes/groups - g*p.TargetEpisodes/groups
		}
		out, err := s.step(run, "outline", fmt.Sprintf("规划剧情段 %d / %d", g+1, groups), map[string]any{
			"bible": bible, "material": material, "sourceIds": ids, "targetCount": target,
			"targetSeconds": p.TargetSeconds, "previousBridge": previous, "instruction": run.Instruction})
		if err != nil {
			return err
		}
		var section Outline
		if err := json.Unmarshal(out, &section); err != nil {
			return err
		}
		for _, ep := range section.Episodes {
			ep.ID = fmt.Sprintf("episode-%04d", len(result.Episodes)+1)
			result.Episodes = append(result.Episodes, ep)
			previous = ep.Bridge
		}
	}
	encoded, _ := json.Marshal(result)
	return s.publish(run, "outline", encoded)
}

func (s *Service) scripts(run core.Run, p Project) error {
	analysis, bible, err := s.materials(p)
	if err != nil {
		return err
	}
	d, err := s.Document(p.ID, "outline", 0)
	if err != nil {
		return err
	}
	var outline Outline
	if err := json.Unmarshal(d.Content, &outline); err != nil {
		return err
	}
	for _, target := range run.Targets {
		if existing, err := s.Document(p.ID, target, 0); err == nil && existing.Origin == run.ID {
			continue
		}
		var ep Episode
		idx := 0
		for i, e := range outline.Episodes {
			if e.ID == target {
				ep = e
				idx = i
				break
			}
		}
		if ep.ID == "" {
			return errors.New("分集已经不存在")
		}
		summaries := []json.RawMessage{}
		for _, id := range ep.SourceIDs {
			if value, ok := analysis.Summaries[id]; ok {
				summaries = append(summaries, value)
			}
		}
		material, err := s.compress(run, summaries)
		if err != nil {
			return err
		}
		previous := "故事开端"
		if idx > 0 {
			d, err := s.Document(p.ID, outline.Episodes[idx-1].ID, 0)
			if err != nil {
				return errors.New("前一集剧本缺失")
			}
			var previousScript struct {
				EndingState string `json:"endingState"`
			}
			_ = json.Unmarshal(d.Content, &previousScript)
			previous = previousScript.EndingState
		}
		input := map[string]any{"bible": bible, "episode": ep, "material": material, "sourceIds": ep.SourceIDs,
			"previousEnding": previous, "targetSeconds": p.TargetSeconds, "instruction": run.Instruction, "requestId": run.ID}
		// 少量材料可直接读取原文；较多材料采用已经逐段阅读的摘要，不截取一半冒充全段。
		if len(ep.SourceIDs) == 1 && run.SceneID == "" {
			value, err := s.Source(p.ID, ep.SourceIDs[0])
			if err != nil {
				return err
			}
			input["originalText"] = value["text"]
		}
		if run.SceneID != "" {
			old, err := s.Document(p.ID, target, 0)
			if err != nil {
				return err
			}
			var original struct {
				Scenes []json.RawMessage `json:"scenes"`
			}
			if err := json.Unmarshal(old.Content, &original); err != nil {
				return err
			}
			for _, scene := range original.Scenes {
				var identity struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(scene, &identity)
				if identity.ID == run.SceneID {
					input["oldScene"] = scene
					break
				}
			}
			if input["oldScene"] == nil {
				return errors.New("指定场景不存在，未发起模型调用")
			}
			input["sceneId"] = run.SceneID
		}
		out, err := s.step(run, "script", "编写 "+ep.ID+" · "+ep.Title, input)
		if err != nil {
			return err
		}
		if run.SceneID != "" {
			// 模型可能未完全遵守“只改一个场景”。合并由确定性代码完成，其余场景原样保留。
			old, _ := s.Document(p.ID, target, 0)
			out, err = mergeScene(old.Content, out, run.SceneID)
			if err != nil {
				return err
			}
		}
		if err := s.publish(run, target, out); err != nil {
			return err
		}
	}
	return nil
}

func mergeScene(old, candidate json.RawMessage, id string) (json.RawMessage, error) {
	var a, b map[string]json.RawMessage
	if json.Unmarshal(old, &a) != nil || json.Unmarshal(candidate, &b) != nil {
		return nil, errors.New("场景合并格式错误")
	}
	var originals, replacements []map[string]json.RawMessage
	_ = json.Unmarshal(a["scenes"], &originals)
	_ = json.Unmarshal(b["scenes"], &replacements)
	found := false
	for i, scene := range originals {
		var sid string
		_ = json.Unmarshal(scene["id"], &sid)
		if sid != id {
			continue
		}
		for _, replacement := range replacements {
			var rid string
			_ = json.Unmarshal(replacement["id"], &rid)
			if rid == id {
				originals[i] = replacement
				found = true
				break
			}
		}
	}
	if !found {
		return nil, errors.New("指定场景不存在或模型没有返回对应场景")
	}
	a["scenes"], _ = json.Marshal(originals)
	// 场景修改后结束状态需重新确认，不采用模型基于其它改写场景推导的新状态。
	var warnings []string
	_ = json.Unmarshal(a["warnings"], &warnings)
	warnings = append(warnings, "局部场景已重写，请复核本集 endingState 与后续衔接")
	a["warnings"], _ = json.Marshal(warnings)
	return json.Marshal(a)
}

func documentOperation(id string) string {
	if id == "bible" {
		return "bible"
	}
	if id == "outline" {
		return "outline"
	}
	if strings.HasPrefix(id, "episode-") {
		return "script"
	}
	return ""
}
