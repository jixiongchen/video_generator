# AI 短视频工作流

当前版本是“先跑通功能”的纵向切片：输入文生视频参数后，由 Go API 创建并持久化任务，Python Worker 调用 JusuanHub 视频接口，React 页面持续展示任务状态，并通过本机 Go 服务预览和下载成片。

## 已实现

- 文生视频参数表单：模型别名、提示词、分辨率、方向和时长。
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
Copy-Item .env.example .env
# 编辑 .env，填写 VIDEO_API_KEY
docker compose up --build
```

打开 <http://127.0.0.1:8080>。创建任务会真实调用供应商并可能产生费用；未配置 `VIDEO_API_KEY` 时 Compose 会拒绝启动。

## 本地开发

要求 Node.js 22+、pnpm、Go 1.23+、Python 3.12+。

终端一，启动 Worker：

```powershell
Copy-Item .env.example .env
# 使用文本编辑器打开项目根目录 .env，填写 VIDEO_API_KEY
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
