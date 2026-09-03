/** 与 Go 的 agents/novel/model.go 对应。这里描述传输数据，不复制业务判断。
 * revision 是乐观锁版本：保存时带回服务端，防止旧页面覆盖新稿。
 */
export interface Chapter { id: string; title: string; start: number; end: number }
export interface Source { id: string; chapterId: string; start: number; end: number }
export interface DocumentRef { current: number; approved: number; stale: boolean }
export interface Novel {
  id: string; title: string; revision: number; characterCount: number; encoding: string;
  chapters: Chapter[]; sources: Source[]; chaptersConfirmed: boolean;
  targetSeconds: number; targetEpisodes: number; documents: Record<string, DocumentRef>;
  runIds: string[]; warnings: string[]; createdAt: string; updatedAt: string;
}
export type JsonValue = string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue };
export interface Artifact { id: string; revision: number; content: Record<string, JsonValue>; origin: string; createdAt: string }
export interface Episode { id: string; title: string; sourceIds: string[] }
export interface AgentRun {
  id: string; projectId: string; stage: "analyze" | "outline" | "script";
  status: "running" | "pausing" | "paused" | "succeeded" | "failed" | "canceled";
  sequence: number; current: string; completed: number; error?: string;
  steps: Record<string, { operation: string; usage: { total_tokens?: number } | null }>;
}
export interface TextConfig { configured: boolean; model: string; protocol: string }
export const isRunning = (run: AgentRun | null) => run?.status === "running" || run?.status === "pausing";
export const isConfirmed = (ref?: DocumentRef) => !!ref && ref.current > 0 && ref.current === ref.approved && !ref.stale;
