import type { AgentRun, Artifact, Chapter, JsonValue, Novel, Source, TextConfig } from "./types";

/** 浏览器只请求本机 Go 的相对地址，不接触文本 Key 或第三方服务器。
 * 请求失败抛出 Error，由工作台统一显示；不把失败响应伪装成成功数据。
 */
async function request<T>(path: string, method = "GET", body?: unknown): Promise<T> {
  const multipart = body instanceof FormData;
  const response = await fetch(`/api/v1${path}`, {
    method, headers: multipart ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : multipart ? body : JSON.stringify(body)
  });
  let value: T & { error?: { message?: string } };
  try { value = await response.json(); } catch { throw new Error("无法读取本机 API 响应，请确认 Go 服务已启动"); }
  if (!response.ok) throw new Error(value.error?.message ?? `请求失败 (${response.status})`);
  return value;
}
const novelPath = (id: string) => `/novels/${encodeURIComponent(id)}`;
export const novelApi = {
  config: () => request<TextConfig>("/agents/config"),
  list: () => request<{ items: Novel[] }>("/novels"),
  import: (body: FormData) => request<Novel>("/novels", "POST", body),
  get: (id: string) => request<Novel>(novelPath(id)),
  settings: (p: Novel, patch: Partial<Novel>, chapters?: Chapter[]) => request<Novel>(novelPath(p.id), "PATCH", {
    revision: p.revision, title: p.title, targetSeconds: p.targetSeconds, targetEpisodes: p.targetEpisodes,
    chaptersConfirmed: p.chaptersConfirmed, ...patch, ...(chapters ? { chapters } : {})
  }),
  document: (id: string, docId: string, revision = 0) => request<Artifact>(`${novelPath(id)}/documents/${encodeURIComponent(docId)}?revision=${revision}`),
  edit: (p: Novel, d: Artifact, content: Record<string, JsonValue>) => request<Novel>(`${novelPath(p.id)}/documents/${d.id}`, "PUT", {
    revision: d.revision, projectRevision: p.revision, content
  }),
  approve: (p: Novel, d: Artifact) => request<Novel>(`${novelPath(p.id)}/documents/${d.id}/approve`, "POST", {
    revision: d.revision, projectRevision: p.revision
  }),
  source: (id: string, sourceId: string) => request<{ source: Source; text: string }>(`${novelPath(id)}/sources/${encodeURIComponent(sourceId)}`),
  checks: (id: string) => request<{ items: string[] }>(`${novelPath(id)}/checks`),
  start: (p: Novel, stage: AgentRun["stage"], consent: boolean, targets: string[] = [], instruction = "", sceneId = "") =>
    request<AgentRun>(`${novelPath(p.id)}/agent-runs`, "POST", { requestId: crypto.randomUUID(), revision: p.revision, stage, consent, targets, instruction, sceneId }),
  run: (id: string) => request<AgentRun>(`/agent-runs/${encodeURIComponent(id)}`),
  control: (id: string, action: "pause" | "resume" | "cancel") => request<AgentRun>(`/agent-runs/${encodeURIComponent(id)}/${action}`, "POST", {}),
  events: (id: string) => `/api/v1/agent-runs/${encodeURIComponent(id)}/events`,
  exportUrl: (id: string, format: "json" | "markdown", episodeId = "") => `/api/v1${novelPath(id)}/export?format=${format}&episodeId=${encodeURIComponent(episodeId)}`
};
