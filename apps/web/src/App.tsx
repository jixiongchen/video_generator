import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import { api } from "./api";
import type { Generation, GenerationRequest } from "./types";

const initialForm: GenerationRequest = {
  model: "minimax-h3",
  prompt: "生成一个红色的奥迪R8跑车，在山地公路上飙车的激情视频。",
  generationMode: "t2v",
  resolutionTier: "768p",
  orientation: "portrait",
  seconds: 15
};

const statusLabels: Record<Generation["status"], string> = {
  pending: "待处理",
  queued: "排队中",
  running: "生成中",
  waiting_provider: "等待供应商",
  succeeded: "已完成",
  failed: "失败",
  canceled: "已取消",
  stale: "结果已过期"
};

const isActive = (status: Generation["status"]) =>
  ["pending", "queued", "running", "waiting_provider"].includes(status);

export default function App() {
  const [form, setForm] = useState<GenerationRequest>(initialForm);
  const [items, setItems] = useState<Generation[]>([]);
  const [selectedGenerationId, setSelectedGenerationId] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  const refresh = useCallback(async () => {
    try {
      setItems(await api.listGenerations());
      setError("");
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : "无法读取任务");
    }
  }, []);

  useEffect(() => {
    void api
      .config()
      .then((config) => setForm((current) => ({ ...current, ...config.defaults })))
      .catch(() => undefined);
    void refresh();
  }, [refresh]);

  const hasActiveTasks = useMemo(() => items.some((item) => isActive(item.status)), [items]);
  const selectedGeneration = useMemo(
    () =>
      items.find(
        (item) => item.id === selectedGenerationId && item.status === "succeeded" && item.videoUrl
      ),
    [items, selectedGenerationId]
  );

  useEffect(() => {
    if (selectedGeneration) return;
    const latestCompleted = items.find((item) => item.status === "succeeded" && item.videoUrl);
    setSelectedGenerationId(latestCompleted?.id ?? "");
  }, [items, selectedGeneration]);

  useEffect(() => {
    if (!hasActiveTasks) return;
    const timer = window.setInterval(() => void refresh(), 1200);
    return () => window.clearInterval(timer);
  }, [hasActiveTasks, refresh]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setError("");
    try {
      const created = await api.createGeneration(form);
      setItems((current) => [created, ...current]);
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : "创建任务失败");
    } finally {
      setSubmitting(false);
    }
  }

  async function cancel(id: string) {
    try {
      const updated = await api.cancelGeneration(id);
      setItems((current) => current.map((item) => (item.id === id ? updated : item)));
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : "取消任务失败");
    }
  }

  return (
    <main className="shell">
      <header className="page-header">
        <div>
          <p className="eyebrow">LOCAL VIDEO LAB · V0.1</p>
          <h1>AI 短视频生成</h1>
          <p className="subtitle">先跑通提示词到视频任务的完整链路，工作流画布与精细 UI 后续迭代。</p>
        </div>
        <span className="mode-pill live-pill">真实 API 模式</span>
      </header>

      {error && <div className="error-banner">{error}</div>}

      <section className="panel form-panel">
        <div className="section-heading">
          <div>
            <span>01</span>
            <h2>提交文生视频任务</h2>
          </div>
          <p>API Key 仅由本机 Worker 环境变量读取，不会进入浏览器。</p>
        </div>

        <form onSubmit={submit}>
          <label className="wide-field">
            <span>提示词</span>
            <textarea
              value={form.prompt}
              maxLength={2000}
              rows={4}
              onChange={(event) => setForm({ ...form, prompt: event.target.value })}
              placeholder="描述画面、主体、动作、镜头与光线"
              required
            />
          </label>

          <div className="form-grid">
            <label>
              <span>模型别名</span>
              <input
                value={form.model}
                onChange={(event) => setForm({ ...form, model: event.target.value })}
                required
              />
            </label>
            <label>
              <span>分辨率</span>
              <select
                value={form.resolutionTier}
                onChange={(event) =>
                  setForm({
                    ...form,
                    resolutionTier: event.target.value as GenerationRequest["resolutionTier"]
                  })
                }
              >
                <option value="480p">480p</option>
                <option value="720p">720p</option>
                <option value="768p">768p</option>
                <option value="1080p">1080p</option>
              </select>
            </label>
            <label>
              <span>画面方向</span>
              <select
                value={form.orientation}
                onChange={(event) =>
                  setForm({
                    ...form,
                    orientation: event.target.value as GenerationRequest["orientation"]
                  })
                }
              >
                <option value="landscape">横屏 16:9</option>
                <option value="portrait">竖屏 9:16</option>
                <option value="square">方形 1:1</option>
              </select>
            </label>
            <label>
              <span>时长</span>
              <select
                value={form.seconds}
                onChange={(event) =>
                  setForm({ ...form, seconds: Number(event.target.value) as 5 | 10 | 15 })
                }
              >
                <option value={5}>5 秒</option>
                <option value={10}>10 秒</option>
                <option value={15}>15 秒</option>
              </select>
            </label>
          </div>

          <button className="primary-button" type="submit" disabled={submitting}>
            {submitting ? "正在提交…" : "开始生成"}
          </button>
        </form>
      </section>

      <section className="panel preview-panel">
        <div className="section-heading">
          <div>
            <span>02</span>
            <h2>成片预览</h2>
          </div>
          <p>视频通过本机服务读取，可直接播放或下载到电脑。</p>
        </div>

        {selectedGeneration ? (
          <div className="video-preview">
            <video
              key={selectedGeneration.id}
              controls
              playsInline
              preload="metadata"
              src={api.generationVideoUrl(selectedGeneration.id)}
            >
              当前浏览器不支持视频播放。
            </video>
            <div className="preview-details">
              <span className="status status-succeeded">已生成</span>
              <h3>{selectedGeneration.request.prompt}</h3>
              <p>
                {selectedGeneration.request.model} · {selectedGeneration.request.resolutionTier} ·{" "}
                {selectedGeneration.request.seconds}s
              </p>
              <a
                className="download-button"
                href={api.generationVideoUrl(selectedGeneration.id, true)}
                download={`${selectedGeneration.id}.mp4`}
              >
                下载视频
              </a>
            </div>
          </div>
        ) : (
          <div className="empty-state">视频生成完成后，将自动出现在这里。</div>
        )}
      </section>

      <section className="panel jobs-panel">
        <div className="section-heading">
          <div>
            <span>03</span>
            <h2>生成记录</h2>
          </div>
          <button className="text-button" type="button" onClick={() => void refresh()}>
            刷新
          </button>
        </div>

        {items.length === 0 ? (
          <div className="empty-state">还没有任务。提交第一个提示词验证链路。</div>
        ) : (
          <div className="job-list">
            {items.map((item) => (
              <article className="job-card" key={item.id}>
                <div className="job-main">
                  <div className="job-meta">
                    <span className={`status status-${item.status}`}>{statusLabels[item.status]}</span>
                    <span>{item.request.model}</span>
                    <span>
                      {item.request.resolutionTier} · {item.request.seconds}s
                    </span>
                  </div>
                  <h3>{item.request.prompt}</h3>
                  <div className="progress-track" aria-label={`进度 ${item.progress}%`}>
                    <div className="progress-value" style={{ width: `${item.progress}%` }} />
                  </div>
                  <div className="job-footer">
                    <code>{item.providerId || item.id}</code>
                    <span>{item.progress}%</span>
                  </div>
                  {item.error && <p className="job-error">{item.error}</p>}
                </div>
                <div className="job-output">
                  {item.status === "succeeded" && item.videoUrl ? (
                    <div className="result-actions">
                      <button
                        className="secondary-button"
                        type="button"
                        onClick={() => setSelectedGenerationId(item.id)}
                      >
                        预览
                      </button>
                      <a
                        className="secondary-button"
                        href={api.generationVideoUrl(item.id, true)}
                        download={`${item.id}.mp4`}
                      >
                        下载
                      </a>
                    </div>
                  ) : isActive(item.status) ? (
                    <button className="secondary-button" onClick={() => void cancel(item.id)}>
                      取消
                    </button>
                  ) : null}
                </div>
              </article>
            ))}
          </div>
        )}
      </section>
    </main>
  );
}
