import { useState } from "react";
import type { Artifact, JsonValue } from "./types";

const labels: Record<string, string> = {
  title: "标题", logline: "一句话主线", world: "世界观与规则", plot: "主线与转折", ending: "结局",
  characters: "人物", uncertainties: "待确认问题", id: "稳定 ID", name: "姓名", aliases: "别名", description: "身份与关系",
  motivation: "人物动机与变化", episodes: "分集大纲", goal: "本集目标", conflict: "主要冲突", summary: "剧情推进",
  hook: "结尾悬念", bridge: "下一集衔接", estimatedSeconds: "预计时长（秒）", sourceIds: "原文片段 ID（每行一个）",
  changes: "改编说明", scenes: "场景", location: "地点", time: "时间", purpose: "场景目的", action: "可拍摄动作",
  dialogue: "对白", characterId: "人物 ID", text: "台词", narration: "旁白", sound: "声音", continuity: "衔接／人物已知信息",
  endingState: "本集结束状态（位置、道具、时间、已知信息）", warnings: "检查提示"
};

/** 递归表单只负责编辑 JSON 值；合法人物 ID、引用、时长最终由后端验证。
 * 更新对象和数组时创建副本，不直接修改 props，符合 React 的单向数据流。
 */
function Field({ name, value, change, disabled }: { name: string; value: JsonValue; change: (v: JsonValue) => void; disabled: boolean }) {
  const label = labels[name] ?? name;
  if (Array.isArray(value)) {
    if (value.every(v => typeof v === "string")) return <label><span>{label}</span><textarea rows={Math.min(5, Math.max(2, value.length))} disabled={disabled} value={value.join("\n")} onChange={e => change(e.target.value.split("\n").filter(v => v.trim()))} /></label>;
    return <section className="novel-fields"><h3>{label} · {value.length}</h3>{value.map((item, i) => <details key={i} open={value.length <= 3}>
      <summary>{label} {i + 1}{item && !Array.isArray(item) && typeof item === "object" ? ` · ${String(item.title ?? item.name ?? item.location ?? "")}` : ""}</summary>
      <Field name={name + "Item"} value={item} disabled={disabled} change={v => change(value.map((old, idx) => idx === i ? v : old))} />
    </details>)}</section>;
  }
  if (value && typeof value === "object") return <div className="novel-fields">{Object.entries(value).map(([key, item]) =>
    <Field key={key} name={key} value={item} disabled={disabled} change={v => change({ ...value, [key]: v })} />)}</div>;
  if (typeof value === "number") return <label><span>{label}</span><input type="number" min={1} disabled={disabled} value={value} onChange={e => change(Number(e.target.value))} /></label>;
  return <label><span>{label}</span>{name === "id" ? <input readOnly value={String(value ?? "")} /> : <textarea disabled={disabled} rows={name === "action" || name === "plot" ? 5 : 2} value={String(value ?? "")} onChange={e => change(e.target.value)} />}</label>;
}

export default function DocumentEditor({ document, editable, busy, onSave, onApprove, onDirty }: {
  document: Artifact; editable: boolean; busy: boolean;
  onSave: (content: Record<string, JsonValue>) => Promise<void>; onApprove: () => Promise<void>; onDirty: (dirty: boolean) => void;
}) {
  const [draft, setDraft] = useState(document.content);
  const [dirty, setDirty] = useState(false);
  const [page, setPage] = useState(0);
  const [saving, setSaving] = useState(false);
  function change(value: JsonValue) { setDraft(value as Record<string, JsonValue>); setDirty(true); onDirty(true); }
  const episodes = Array.isArray(draft.episodes) ? draft.episodes : null;
  return <div className="novel-editor">
    {!editable && <p className="novel-notice">历史版本只读。切换到最新草稿后才能编辑与确认。</p>}
    {episodes ? <>
      <div className="novel-actions"><button disabled={page === 0} onClick={() => setPage(page - 1)}>上一页</button><span>第 {page + 1} / {Math.max(1, Math.ceil(episodes.length / 5))} 页 · 共 {episodes.length} 集</span><button disabled={(page + 1) * 5 >= episodes.length} onClick={() => setPage(page + 1)}>下一页</button></div>
      {episodes.slice(page * 5, page * 5 + 5).map((episode, i) => <details key={page * 5 + i} open><summary>第 {page * 5 + i + 1} 集</summary><Field name="episode" value={episode} disabled={!editable || busy} change={v => change({ ...draft, episodes: episodes.map((old, idx) => idx === page * 5 + i ? v : old) })} /></details>)}
    </> : <Field name="document" value={draft} disabled={!editable || busy} change={change} />}
    <div className="novel-actions novel-savebar">
      <button disabled={!editable || !dirty || busy || saving} onClick={async () => { setSaving(true); try { await onSave(draft); setDirty(false); onDirty(false); } catch { /* 工作台已显示错误，保留草稿供重试，避免未处理的 Promise。 */ } finally { setSaving(false); } }}>保存新版本</button>
      <button className="primary-button" disabled={!editable || dirty || busy || saving} onClick={() => void onApprove()}>确认当前版本</button>
      <small>{dirty ? "有未保存修改，先保存再确认。" : "确认表示你已核对原文及改编内容。"}</small>
    </div>
    <details><summary>查看 JSON 协议（学习／导出用途）</summary><pre>{JSON.stringify(draft, null, 2)}</pre></details>
  </div>;
}
