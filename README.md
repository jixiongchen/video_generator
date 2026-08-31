# AI 短视频工作流

当前版本是“先跑通功能”的纵向切片：输入文生视频参数后，由 Go API 创建并持久化任务，Python Worker 调用 JusuanHub 视频接口，React 页面持续展示任务状态，并通过本机 Go 服务预览和下载成片。

## 已实现

- 文生视频参数表单：模型别名、提示词、分辨率、方向、时长、随机种子。
- Go REST API、本地 JSON 持久化、任务查询、取消和 SSE 事件流。
- Python 异步 Worker、JusuanHub Bearer Token 调用、供应商状态归一化和结果 URL 提取。
- 聚合视频接口：`POST /v1/media/generations`，状态查询默认使用 `GET /v1/media/generations/{task_id}`。
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
VIDEO_API_STATUS_PATH_TEMPLATE=/v1/media/generations/{id}
VIDEO_API_KEY=在这里填写你的Key
```

`.\scripts\start-worker.ps1` 会把 `.env` 内容载入 Worker 进程环境。`VIDEO_API_KEY` 只进入 Python Worker，不会出现在前端响应、Go 数据文件或正常日志中。页面的“模型别名”必须填写供应商实际支持的模型别名。

当前根据提交接口约定，异步状态查询默认使用 `GET /v1/media/generations/{id}`。如果供应商返回的实际查询路径不同，只需修改 `.env` 中的 `VIDEO_API_STATUS_PATH_TEMPLATE` 后重启 Worker。

## 视频供应商适配约定

提交请求保持用户提供的字段：

```json
{
  "model": "<MODEL_ALIAS>",
  "prompt": "生成一个红色的奥迪R8跑车，在山地公路上飙车的激情视频。",
  "generationMode": "t2v",
  "resolutionTier": "480p",
  "orientation": "landscape",
  "seconds": 5,
  "seed": 42
}
```

Worker 会兼容常见的 `id/task_id/request_id`、`status/task_status/state`、`url/video_url/content_url` 及嵌套 `content/output/data/results` 响应。如果真实响应字段不同，应先补充 `services/worker/worker/provider.py` 的归一化测试，再调整适配逻辑。

## 测试

```powershell
./scripts/test.ps1
```

服务运行且已配置 Key 后，可执行真实端到端冒烟。该命令会产生一次实际生成费用：

```powershell
.\scripts\smoke.ps1 -Model "供应商模型别名" -TimeoutSeconds 900
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
