import { useEffect, useState } from "react";
import DocumentEditor from "./DocumentEditor";
import { novelApi } from "./api";
import { useNovelWorkspace } from "./useNovelWorkspace";
import { isConfirmed, isRunning, type AgentRun, type Artifact, type Chapter, type Episode, type JsonValue } from "./types";
import "./novel.css";

const runLabels: Record<AgentRun["status"], string> = { running: "运行中", pausing: "当前步骤完成后暂停", paused: "已暂停", succeeded: "等待审核", failed: "需要处理", canceled: "已取消" };

/** 小说工作台以“原文依据”为右侧常驻区域，提醒用户：Agent 稿件不是原文事实。
 * 网络与任务订阅在 hook；文档表单在 DocumentEditor；这里只编排用户操作。
 */
export default function NovelWorkspace() {
  const w = useNovelWorkspace();
  const p = w.project;
  const [docId, setDocId] = useState("bible");
  const [document, setDocument] = useState<Artifact | null>(null);
  const [version, setVersion] = useState(0);
  const [episodes, setEpisodes] = useState<Episode[]>([]);
  const [episodePage, setEpisodePage] = useState(0);
  const [sourceId, setSourceId] = useState("");
  const [sourceText, setSourceText] = useState("");
  const [dirty, setDirty] = useState(false);
  const [consent, setConsent] = useState(false);
  const [instruction, setInstruction] = useState("");
  const [sceneId, setSceneId] = useState("");
  const [chapterDraft, setChapterDraft] = useState("");
  const [notice, setNotice] = useState("");
  const [settings, setSettings] = useState({ title: "", targetSeconds: 120, targetEpisodes: 0 });
  const blocked = w.busy || isRunning(w.run);
  const ref = p?.documents[docId];

  useEffect(() => {
    setDocument(null); setDocId("bible"); setVersion(0); setDirty(false); setConsent(false); setSourceId("");
    setEpisodePage(0); setChapterDraft(""); setNotice("");
  }, [p?.id]);
  useEffect(() => { if (p) setSettings({ title: p.title, targetSeconds: p.targetSeconds, targetEpisodes: p.targetEpisodes }); }, [p?.id, p?.revision]);
  useEffect(() => {
    if (!dirty) return;
    const warn = (e: BeforeUnloadEvent) => { e.preventDefault(); };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);
  useEffect(() => {
    let disposed = false;
    if (!p || !ref?.current) { setDocument(null); return; }
    if (dirty) return; // 进度更新不能覆盖编辑中的文字。
    void novelApi.document(p.id, docId, version).then(d => { if (!disposed) setDocument(d); }).catch(e => !disposed && w.setError(e.message));
    return () => { disposed = true; };
  }, [p?.id, docId, ref?.current, version, dirty]);
  useEffect(() => {
    let disposed = false;
    if (!p?.documents.outline?.current) { setEpisodes([]); return; }
    void novelApi.document(p.id, "outline").then(d => { if (!disposed) setEpisodes(d.content.episodes as unknown as Episode[]); }).catch(e => !disposed && w.setError(e.message));
    return () => { disposed = true; };
  }, [p?.id, p?.documents.outline?.current]);
  useEffect(() => {
    let disposed = false;
    const selected = sourceId || p?.sources[0]?.id;
    if (!p || !selected) { setSourceText(""); return; }
    void novelApi.source(p.id, selected).then(v => { if (!disposed) setSourceText(v.text); }).catch(e => !disposed && w.setError(e.message));
    return () => { disposed = true; };
  }, [p?.id, sourceId, p?.sources.length]);

  function leaveDraft() { if (!dirty || window.confirm("有未保存的剧本修改，确定放弃并切换吗？")) { setDirty(false); return true; } return false; }
  function selectDocument(id: string) { if (leaveDraft()) { setDocId(id); setDocument(null); setVersion(0); setSceneId(""); } }
  async function start(stage: AgentRun["stage"], targets: string[] = []) {
    if (!p || dirty) return;
    await w.action(async () => { const run = await novelApi.start(p, stage, consent, targets, instruction, sceneId); w.setRun(run); w.setProject(await novelApi.get(p.id)); });
  }
  const scenes = document && Array.isArray(document.content.scenes) ? document.content.scenes as Record<string, JsonValue>[] : [];
  const sources = [...new Set(episodes.find(e => e.id === docId)?.sourceIds ?? [])];
  const knownSteps = w.run ? Object.values(w.run.steps) : [];
  const knownUsage = knownSteps.length > 0 && knownSteps.every(s => s.usage?.total_tokens !== undefined);

  return <main className="novel-shell">
    <header className="novel-header"><div><p className="eyebrow">NOVEL → EPISODES → SCRIPT</p><h1>把长篇故事，写成一集一集的短剧</h1><p>先整理全书，再确认大纲；试写一集，满意后分批继续。每一步都能回看原文。</p></div><span className="mode-pill">{w.config?.configured ? `文本模型 · ${w.config.model}` : "等待文本 API 配置"}</span></header>
    {w.error && <div className="error-banner" role="alert">{w.error}<button onClick={() => w.setError("")}>关闭提示</button></div>}
    {notice && <p role="status" className="novel-notice">{notice}</p>}
    {!w.config?.configured && <div className="novel-notice">小说可以先导入到本机。分析前请在根目录 .env 配置文本服务，重启 Python 后<button onClick={() => void w.refreshConfig()}>检查配置</button>。这里不填写 Key，也不会调用视频接口。</div>}

    <div className="novel-projectbar"><label><span>小说项目</span><select value={p?.id ?? ""} disabled={w.busy} onChange={e => {
      if (!leaveDraft()) return;
      const id = e.target.value;
      if (!id) { w.setProject(null); return; }
      void w.action(async () => w.setProject(await novelApi.get(id)));
    }}><option value="">导入一本小说</option>{w.projects.map(item => <option key={item.id} value={item.id}>{item.title}</option>)}</select></label>
      {p && <><span>{p.characterCount.toLocaleString()} 字符 · {p.chapters.length} 章 · {episodes.length} 集</span><a href={novelApi.exportUrl(p.id, "markdown")}>导出全剧 Markdown</a><a href={novelApi.exportUrl(p.id, "json")}>导出 JSON</a></>}
    </div>

    {!p ? <section className="panel novel-import"><h2>导入小说</h2><p>支持 TXT 或粘贴文字，最多 100 万字符、20 MiB。上传只保存到本机；分析时才发送给你的文本服务。</p>
      <form onSubmit={e => { e.preventDefault(); const form = new FormData(e.currentTarget); void w.action(async () => { const project = await novelApi.import(form); await w.list(); w.setProject(project); }); }}>
        <label><span>书名</span><input name="title" required maxLength={120} placeholder="例如：山城来信" /></label>
        <div className="novel-inputrow"><label><span>TXT 文件（文件优先于粘贴内容）</span><input name="file" type="file" accept=".txt,text/plain" /></label><label><span>文件编码</span><select name="encoding"><option value="auto">自动识别</option><option value="utf-8">UTF-8</option><option value="gb18030">GB18030</option></select></label></div>
        <label><span>或粘贴小说文字</span><textarea name="text" rows={10} maxLength={2000000} placeholder="粘贴带章节标题的正文。导入后先检查原文和章节，再开始分析。" /></label>
        <button className="primary-button" disabled={w.busy}>{w.busy ? "正在导入…" : "导入并检查章节"}</button>
      </form></section> : <>
      <section className="novel-stagebar" aria-label="改编流程">
        {["导入检查", "故事资料", "分集大纲", "首集试写", "分批编剧", "审核导出"].map((label, i) => <span key={label}><b>{i + 1}</b>{label}</span>)}
      </section>
      <details className="panel novel-settings" open={!p.chaptersConfirmed}><summary>改编设置与章节检查 {p.chaptersConfirmed ? "· 已确认" : "· 请先检查"}</summary>
        <div className="novel-inputrow"><label><span>书名</span><input value={settings.title} disabled={blocked} onChange={e => setSettings({ ...settings, title: e.target.value })} /></label><label><span>每集目标时长（秒）</span><input type="number" min={30} max={600} value={settings.targetSeconds} disabled={blocked} onChange={e => setSettings({ ...settings, targetSeconds: Number(e.target.value) })} /></label><label><span>目标集数（0 = Agent 推荐）</span><input type="number" min={0} max={1000} value={settings.targetEpisodes} disabled={blocked} onChange={e => setSettings({ ...settings, targetEpisodes: Number(e.target.value) })} /></label></div>
        <p>编码：{p.encoding}。右侧可检查原文。忠于主线，允许压缩重排；时长是剧本估算，不是单次视频生成长度。</p>
        {!p.documents.analysis && <details><summary>调整章节边界（高级）</summary><p>每行：章节 ID | 标题 | 起始字符 | 结束字符。位置为 Unicode 字符下标，必须连续覆盖全文。</p>
          <button disabled={blocked} onClick={() => setChapterDraft(p.chapters.map(c => `${c.id} | ${c.title} | ${c.start} | ${c.end}`).join("\n"))}>载入章节边界</button>
          <textarea rows={8} value={chapterDraft} disabled={blocked} onChange={e => setChapterDraft(e.target.value)} aria-label="章节边界" />
        </details>}
        <button disabled={blocked || dirty} onClick={() => void w.action(async () => {
          const chapters: Chapter[] | undefined = chapterDraft.trim() ? chapterDraft.trim().split("\n").map(line => { const [id, title, start, end] = line.split("|").map(s => s.trim()); return { id, title, start: Number(start), end: Number(end) }; }) : undefined;
          w.setProject(await novelApi.settings(p, { ...settings, chaptersConfirmed: true }, chapters)); setChapterDraft(""); setNotice("设置已保存，章节已确认。"); await w.list();
        })}>保存设置并确认章节</button>
      </details>

      <section className="panel novel-runpanel">
        <p>本书有 {p.sources.length} 个阅读片段。首次完整分析约需 {p.sources.length * 2} 次文本步骤请求（修复／限流重试另计，缓存复用会减少调用）；分集与逐集编写另计。建议先用短篇试跑。</p>
        <label className="novel-consent"><input type="checkbox" checked={consent} onChange={e => setConsent(e.target.checked)} /><span>我确认拥有改编权限，同意将相关正文发送到配置的文本服务，并承担该服务产生的费用。</span></label>
        <div className="novel-actions">
          <button disabled={blocked || dirty || !consent || !w.config?.configured || !p.chaptersConfirmed} onClick={() => void start("analyze")}>{p.documents.bible ? "重新整理故事资料" : "开始阅读与分析"}</button>
          <button disabled={blocked || dirty || !consent || !w.config?.configured || !isConfirmed(p.documents.bible)} onClick={() => void start("outline")}>规划全剧分集</button>
          <button className="primary-button" disabled={blocked || dirty || !consent || !w.config?.configured || !isConfirmed(p.documents.outline)} onClick={() => void start("script")}>{p.documents["episode-0001"] ? "继续生成 · 最多 5 集" : "试写第 1 集"}</button>
        </div>
        {w.run && <div className="novel-runstate" aria-live="polite"><strong>{runLabels[w.run.status]}</strong><span>{w.run.current}</span><small>已保存 {w.run.completed} 个检查点 · 用量 {knownUsage ? `${knownSteps.reduce((sum, s) => sum + (s.usage?.total_tokens ?? 0), 0)} tokens（含复用步骤原用量）` : "未知"}</small>
          {w.run.error && <p className="job-error">{w.run.error}</p>}
          <div className="novel-actions">
            {w.run.status === "running" && <button disabled={w.busy} onClick={() => void w.action(async () => w.setRun(await novelApi.control(w.run!.id, "pause")))}>在检查点暂停</button>}
            {["paused", "failed"].includes(w.run.status) && <button disabled={w.busy || !consent} onClick={() => void w.action(async () => w.setRun(await novelApi.control(w.run!.id, "resume")))}>确认费用风险并继续</button>}
            {w.run.status !== "succeeded" && w.run.status !== "canceled" && <button disabled={w.busy} onClick={() => void w.action(async () => w.setRun(await novelApi.control(w.run!.id, "cancel")))}>取消后续调用</button>}
          </div>
        </div>}
      </section>

      <div className="novel-desk">
        <aside className="novel-directory"><h2>全剧目录</h2>
          <button className={docId === "bible" ? "selected" : ""} onClick={() => selectDocument("bible")}>故事资料 <small>{isConfirmed(p.documents.bible) ? "已确认" : "待审核"}</small></button>
          <button className={docId === "outline" ? "selected" : ""} onClick={() => selectDocument("outline")}>分集大纲 <small>{episodes.length} 集</small></button>
          <h3>逐集剧本</h3>
          {episodes.slice(episodePage * 20, episodePage * 20 + 20).map((ep, i) => <button className={docId === ep.id ? "selected" : ""} key={ep.id} onClick={() => { selectDocument(ep.id); if (ep.sourceIds[0]) setSourceId(ep.sourceIds[0]); }}><span>第 {episodePage * 20 + i + 1} 集 · {ep.title}</span><small>{p.documents[ep.id]?.stale ? "需要复核" : isConfirmed(p.documents[ep.id]) ? "已确认" : p.documents[ep.id] ? "草稿" : "未生成"}</small></button>)}
          {episodes.length > 20 && <div className="novel-actions"><button disabled={episodePage === 0} onClick={() => setEpisodePage(episodePage - 1)}>上一页</button><button disabled={(episodePage + 1) * 20 >= episodes.length} onClick={() => setEpisodePage(episodePage + 1)}>下一页</button></div>}
        </aside>
        <section className="novel-manuscript"><header><h2>{docId === "bible" ? "故事资料" : docId === "outline" ? "分集大纲" : episodes.find(e => e.id === docId)?.title ?? "逐集剧本"}</h2>
          {ref && <label><span>{ref.stale ? "上游已修改 · 需要复核" : isConfirmed(ref) ? "当前版本已确认" : "草稿待确认"}</span><select value={version} onChange={e => { if (leaveDraft()) setVersion(Number(e.target.value)); }}><option value={0}>最新草稿 · v{ref.current}</option>{Array.from({ length: ref.current }, (_, i) => i + 1).map(v => <option value={v} key={v}>v{v}{ref.approved === v ? " · 上次确认" : ""}</option>)}</select></label>}
        </header>
          {document ? <DocumentEditor key={`${p.id}-${document.id}-${document.revision}`} document={document} editable={document.revision === ref?.current} busy={blocked} onDirty={setDirty}
            onSave={async content => { const ok = await w.action(async () => { w.setProject(await novelApi.edit(p, document, content)); setNotice("新版本已保存，旧版本仍可查看。"); }); if (!ok) throw new Error("保存失败"); }}
            onApprove={async () => { await w.action(async () => { w.setProject(await novelApi.approve(p, document)); setNotice("已确认当前版本，可以进入下一阶段。"); }); }} /> : <div className="empty-state">{ref ? "正在加载稿件…" : "这里还没有稿件。按照上方流程生成，完成后在这里编辑和确认。"}</div>}
          {docId.startsWith("episode-") && <section className="novel-rewrite"><h3>局部重写</h3><p>产生新草稿，不覆盖上次确认版本；后续受影响集会标记为需要复核。</p><label><span>重写范围</span><select value={sceneId} onChange={e => setSceneId(e.target.value)}><option value="">整集</option>{scenes.map(scene => <option key={String(scene.id)} value={String(scene.id)}>{String(scene.id)} · {String(scene.location)}</option>)}</select></label><label><span>修改要求</span><textarea rows={3} maxLength={1000} value={instruction} onChange={e => setInstruction(e.target.value)} placeholder="例如：减少解释性对白，让人物通过动作表现冲突。" /></label><button disabled={blocked || dirty || !consent || !w.config?.configured} onClick={() => void start("script", [docId])}>生成这一集的新草稿</button><a href={novelApi.exportUrl(p.id, "markdown", docId)}>导出这一集</a></section>}
        </section>
        <aside className="novel-evidence"><h2>原文依据</h2><p>模型可能遗漏或误读。确认稿件前，请对照原文。</p>
          {sources.length > 0 && <div className="novel-sourcechips">{sources.map(id => <button key={id} onClick={() => setSourceId(id)}>{id}</button>)}</div>}
          <label><span>选择原文片段</span><select value={sourceId || p.sources[0]?.id || ""} onChange={e => setSourceId(e.target.value)}>{p.sources.map(src => <option key={src.id} value={src.id}>{src.id} · {p.chapters.find(c => c.id === src.chapterId)?.title}</option>)}</select></label>
          <pre className="novel-source">{sourceText || "选择片段后查看正文。"}</pre>
          <h3>检查提示</h3>{w.checks.length > 0 ? <ul>{w.checks.map((check, i) => <li key={i}>{check}</li>)}</ul> : <p>暂无自动检查提示，不代表已通过人工审核。</p>}
        </aside>
      </div>
    </>}
  </main>;
}
