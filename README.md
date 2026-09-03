# AI 短视频工作流

当前包含两个独立工作台：小说拆剧 Agent（小说 → 故事资料 → 分集 → 场景剧本）和视频生成（提示词／参考素材 → 视频 → 预览下载）。React 负责操作，Go 负责业务状态与持久化，Python 负责文本／视频供应商适配；首版优先跑通功能，后续再接画布与成片工作流。

## 已实现

- 内置小说拆剧 Agent：TXT／文字导入、分层分析、故事资料、分集大纲、首集试写、每批 5 集、场景编辑、审核与 Markdown／JSON 导出。
- Agent 检查点、本地版本历史、SSE 进度、暂停／恢复、取消、局部重写与下游复核提示。
- 文本模型采用聚算 `qwen3-6-35b-a3b`，通过 `/v1/chat/completions` 调用；Key 与视频配置分离。
- 文生视频与 MiniMax H3 全能参考表单：支持图片、视频、音频上传。
- Go REST API、本地 JSON 持久化、任务查询、取消和 SSE 事件流。
- Python 异步 Worker、JusuanHub Bearer Token 调用、幂等提交、状态轮询和资源下载代理。
- 聚合视频接口：`POST /v1/media/generations`，随后轮询 `GET /v1/jobs/{job_id}?model={model}`，再通过 `GET /v1/assets/{asset_id}/content?model={model}` 读取成片。
- React 最小操作页、任务记录、成片预览和下载。
- Docker Compose 单机运行配置。
- Go/Python 单元测试和 Mock 端到端冒烟脚本。

> 当前是纵向切片，不是完整 PRD 中的工作流画布。DAG、模板、素材库、轻量时间线、OpenAI 与 ElevenLabs 会在这条链路稳定后迭代。

## 最快启动：Docker Compose

要求 Docker Desktop 或 Docker Engine + Compose。

```powershell
if (-not (Test-Path .env)) { Copy-Item .env.example .env }
# 编辑 .env，按需填写 TEXT_API_KEY / VIDEO_API_KEY；不要覆盖已有 .env
docker compose up --build
```

打开 <http://127.0.0.1:8080>。创建任务会真实调用供应商并可能产生费用；各功能分别检查自己的 Key。只使用小说功能不需要视频 Key。

## 本地开发

要求 Node.js 22+、pnpm、Go 1.23+、Python 3.12+。

终端一，启动 Worker：

```powershell
if (-not (Test-Path .env)) { Copy-Item .env.example .env }
# 使用文本编辑器打开项目根目录 .env，按需填写 TEXT_API_KEY / VIDEO_API_KEY
.\scripts\start-worker.ps1
```

终端二，启动 Go API：

```powershell
.\scripts\start-api.ps1
```

终端三，启动 Vite：

```powershell
pnpm install
pnpm --dir apps\web run dev
```

开发页面为 <http://127.0.0.1:5173>，Vite 会把 `/api` 转发到 `127.0.0.1:8080`。

## 视频 API 环境变量

在项目根目录复制 `.env.example` 为 `.env`，只在 `.env` 的 `VIDEO_API_KEY` 后填写 Key。`.env` 已被 Git 忽略，不要把 Key 写入源码、浏览器或聊天消息。

```dotenv
VIDEO_API_BASE_URL=https://api.jusuanhub.com:10443
VIDEO_API_SUBMIT_PATH=/v1/media/generations
VIDEO_API_JOB_PATH_TEMPLATE=/v1/jobs/{id}?model={model}
VIDEO_API_ASSET_PATH_TEMPLATE=/v1/assets/{asset_id}/content?model={model}
VIDEO_API_KEY=在这里填写你的Key
```

`.\scripts\start-worker.ps1` 会把 `.env` 内容载入 Worker 进程环境。`VIDEO_API_KEY` 只进入 Python Worker，不会出现在前端响应、Go 数据文件或正常日志中。页面的“模型别名”必须填写供应商实际支持的模型别名。

供应商偶发返回 429 或 5xx 时，提交阶段会使用同一个 `Idempotency-Key` 最多尝试三次，避免重复创建付费任务。

## 视频供应商适配约定

提交请求保持用户提供的字段：

```json
{
  "model": "minimax-h3",
  "prompt": "生成一个红色的奥迪R8跑车，在山地公路上飙车的激情视频。",
  "generationMode": "t2v",
  "resolutionTier": "768p",
  "orientation": "portrait",
  "seconds": 15
}
```

Worker 会读取提交响应中的 `jobId`，任务成功后读取 `assets[0].assetId`。视频下载仍经过 Go 和 Python 后端代理，因此供应商 Key 不会暴露给浏览器。

## 小说拆剧 Agent：配置与使用

已有 `.env` 时不要重新复制覆盖它。在项目根目录 `.env` 追加以下配置：

```dotenv
TEXT_API_PROTOCOL=openai_chat
TEXT_API_BASE_URL=https://api.jusuanhub.com:10443
TEXT_API_PATH=/v1/chat/completions
TEXT_API_MODEL=qwen3-6-35b-a3b
TEXT_API_KEY=在本机填写你的文本APIKey
TEXT_API_TIMEOUT_SECONDS=300
TEXT_API_CONTEXT_TOKENS=32768
TEXT_API_OUTPUT_TOKENS=4096
```

上下文额度是本地保守预算，不表示供应商承诺的模型上限；应按账户实际能力调整。Key 不会自动复用 `VIDEO_API_KEY`。

重启 Go 和 Python，刷新前端。选择“小说改编 Agent”，按以下顺序操作：

1. 导入 TXT／粘贴文字，在右侧检查乱码和原文，保存设置并确认章节。
2. 确认授权和费用提示，开始阅读分析。先用小段小说试流程，再处理长篇。
3. 修改并确认故事资料，再生成和确认分集大纲。
4. 试写第 1 集；满意后确认，再继续生成第 2–6 集。
5. 核对每批剧本，确认后继续。可重写指定集／场景，或导出全剧与选定集。

暂停在当前步骤完成后生效；取消不保证供应商停止计费。重启后不会自动重发状态不明的调用，需要手动继续。新增稿件保留旧版本，上游修改只标记复核，不自动收费重写。

开发学习说明：[小说 Agent 架构与开发指南](docs/novel-agent-development.md)。

配置 Key 后可以手动验证真实文本接口（最多两次短文本调用，可能计费；不会自动执行）：

```powershell
.\scripts\smoke-text.ps1 -ConfirmCost
```

全能参考会先用通用 `file` 字段逐项上传到 `/v1/assets/input?model=minimax-h3`，再按图片、视频、音频顺序提交 `referenceInputs`。页面限制为最多 9 张图片、1 段视频、3 段音频，总数最多 12 项，且至少需要一项图片或视频。

## 测试

```powershell
./scripts/test.ps1
```

服务运行且已配置 Key 后，可执行真实端到端冒烟。该命令会产生一次实际生成费用：

```powershell
.\scripts\smoke.ps1 -TimeoutSeconds 900
```

## 项目结构

```text
apps/web                 React + TypeScript + Vite
services/api             Go API、任务持久化、SSE
services/worker          Python 供应商适配与异步任务
scripts                  测试和冒烟脚本
docs                     PRD 与后续设计文档
compose.yaml             单机容器编排
```
