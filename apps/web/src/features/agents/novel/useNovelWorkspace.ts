import { useCallback, useEffect, useRef, useState } from "react";
import { novelApi } from "./api";
import { isRunning, type AgentRun, type Novel, type TextConfig } from "./types";

/** 把网络和生命周期从视图拆出来。
 * SSE 是通知渠道，Go 的持久化快照才是真相；断线时用查询兜底，不依赖网页一直打开。
 */
export function useNovelWorkspace() {
  const [projects, setProjects] = useState<Novel[]>([]);
  const [project, setProject] = useState<Novel | null>(null);
  const [config, setConfig] = useState<TextConfig | null>(null);
  const [run, setRun] = useState<AgentRun | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const actionInFlight = useRef(false);
  const [checks, setChecks] = useState<string[]>([]);
  const list = useCallback(async () => { setProjects((await novelApi.list()).items); }, []);
  const refreshConfig = useCallback(async () => { try { setConfig(await novelApi.config()); } catch (e) { setError(message(e)); } }, []);
  useEffect(() => { void list().catch(e => setError(message(e))); void refreshConfig(); }, [list, refreshConfig]);

  const projectId = project?.id;
  const lastRunId = project?.runIds.at(-1);
  useEffect(() => {
    let disposed = false;
    setRun(null);
    if (lastRunId && projectId) void novelApi.run(lastRunId).then(async r => {
      // 极快任务可能在 SSE 建立前完成。此时也必须刷新产物索引，而不仅更新状态。
      const latest = !isRunning(r) ? await novelApi.get(projectId) : null;
      if (!disposed) { if (latest) setProject(old => old?.id === projectId ? latest : old); setRun(r); }
    }).catch(e => { if (!disposed) setError(message(e)); });
    return () => { disposed = true; };
  }, [projectId, lastRunId]);

  useEffect(() => {
    if (!projectId) { setChecks([]); return; }
    let disposed = false;
    void novelApi.checks(projectId).then(v => { if (!disposed) setChecks(v.items); }).catch(e => { if (!disposed) setError(message(e)); });
    return () => { disposed = true; };
  }, [projectId, project?.updatedAt]);

  const runId = run?.id;
  const active = isRunning(run);
  useEffect(() => {
    if (!runId || !active || !projectId) return;
    let disposed = false;
    let fallback: number | undefined;
    const stream = new EventSource(novelApi.events(runId));
    const accept = async (next: AgentRun) => {
      if (disposed) return;
      // 每次完成步骤都重新读项目索引，使逐集产物无需等整批结束才出现。
      const latest = await novelApi.get(projectId);
      // 先取产物，再一并发布终态。若先设 succeeded，effect 会立即清理，
      // 进而丢弃这次尚未返回的索引请求，造成“任务成功但页面没有剧本”。
      if (!disposed) {
        setProject(old => old?.id === projectId && old.updatedAt <= latest.updatedAt ? latest : old);
        setRun(old => old && old.id === next.id && old.sequence > next.sequence ? old : next);
      }
    };
    stream.addEventListener("agent.updated", e => {
      try { void accept(JSON.parse((e as MessageEvent).data)).catch(err => !disposed && setError(message(err))); }
      catch { setError("进度事件格式无效，请刷新任务状态"); }
    });
    stream.onerror = () => {
      stream.close();
      if (!fallback) fallback = window.setInterval(() => { void novelApi.run(runId).then(accept).catch(e => !disposed && setError(message(e))); }, 2000);
    };
    return () => { disposed = true; stream.close(); if (fallback) window.clearInterval(fallback); };
  }, [runId, active, projectId]);

  async function action(work: () => Promise<void>): Promise<boolean> {
    // ref 同步生效，补上 setBusy 到下一次渲染之间的双击窗口。
    if (actionInFlight.current) return false;
    actionInFlight.current = true;
    setError(""); setBusy(true);
    try { await work(); return true; } catch (e) { setError(message(e)); return false; } finally { actionInFlight.current = false; setBusy(false); }
  }
  return { projects, project, setProject, config, refreshConfig, run, setRun, error, setError, busy, checks, list, action };
}

function message(error: unknown) { return error instanceof Error ? error.message : "操作失败，请重试"; }
