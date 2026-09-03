# 小说拆剧 Agent：架构、开发与验收指南

本文面向第一次同时使用 React、Go、Python 和 AI API 的开发者。目标不只是知道“能运行”，还要能解释数据为什么这样流动，今后如何加入新的 Agent。

## 1. 模块职责

```text
apps/web/src/
  App.tsx                         只负责工作台导航
  features/video/                 原有视频页面
  features/agents/novel/
    NovelWorkspace.tsx            小说工作台、阶段操作、原文依据
    useNovelWorkspace.ts          请求生命周期、SSE 与轮询兜底
    DocumentEditor.tsx            人物／分集／场景／对白的结构化编辑
    api.ts                        仅请求本机 Go，不接触供应商 Key
    types.ts                      前后端传输数据的 TypeScript 类型

services/api/internal/agents/
  core/
    run.go                        可复用的运行与检查点数据类型
    storage.go                    安全 ID 与原子文件写入
    worker.go                     Go → Python 的长请求客户端
  novel/
    model.go                      小说、章节、原文引用、文档版本
    repository.go                 本机存储与版本索引
    engine.go                     阶段编排、缓存、暂停恢复与场景合并
    editor.go                     设置、人工编辑、确认、下游复核
    http.go                       小说 HTTP、SSE 与导出
    novel_test.go                 无费用工作流测试

services/worker/worker/
  agents/
    http.py                       Agent 白名单注册与请求分发
    novel/
      importer.py                 编码、章节识别（不调用模型）
      agent.py                    提示词、输出协议、结构校验
      smoke.py                    显式授权的线上冒烟入口
  providers/text.py               聚算文本协议、额度预算、错误分类
  provider.py                     现有视频适配，保持独立
```

这里刻意没有引入大型 Agent 框架。首版 Agent 是“受约束的多步骤任务”：每个阶段有确定的输入、输出和审核门槛。模型负责理解与编写，程序负责次序、校验和保存。AI 返回一句“继续执行”不能改变系统状态，也不能让服务器执行命令。

React 不决定哪一集可以生成；按钮禁用只是体验，Go 会再次检查审核状态。Python 不保存项目状态，只有 Go 可以确认版本和发布新稿。这种边界可以避免三种语言各维护一套互相矛盾的业务状态。

## 2. 一次点击如何经过三种语言

以“试写第 1 集”为例：

1. React 把阶段 `script`、项目版本和授权确认发送给 Go。
2. Go 检查章节、故事资料、大纲是否已确认，确定目标为第 1 集。
3. Go **先保存 Run，再启动协程**。因此关闭网页不会丢失任务。
4. Go 读取确认稿、相关原文摘要和前集结束状态，形成当前步骤的材料。
5. Go 按输入内容、模型、协议和 Agent 版本计算哈希。已有成功检查点就复用；没有才调用 Python。
6. Python 组装系统规则与不可信资料，检查保守上下文预算，再发送文本 API 请求。
7. Python 校验返回 JSON 的字段、引用、人物 ID、场景数和时长。格式不对最多再生成一次，不返回伪造的成功稿。
8. Go 保存检查点和不可变文档版本，最后更新项目索引。SSE 推送新的状态快照。
9. React 同步稿件和状态。用户确认当前稿后，下一阶段才被允许执行。

注意前端时序：不能先把任务置为“完成”并卸载事件订阅，再等待稿件刷新，否则可能出现“任务成功，页面没有新剧本”。代码先读取最新项目索引，再同时更新任务状态和项目。

## 3. 长篇阅读与分集算法

- 上传文件最大 20 MiB，规范化正文最大 100 万 Unicode 字符；最多 5000 个识别章节。
- Python 严格解码 UTF-8／UTF-8 BOM／GB18030，统一换行但保留完整正文。
- 章节以字符区间 `[start,end)` 表示。Go 使用 `[]rune`，不要把 UTF-8 字节位置或 JavaScript UTF-16 长度直接当作该位置。
- 每章继续拆成不超过 2400 字符的片段，优先在换行处切开；所有区间连续覆盖全文。
- 每段阅读后保存不超过 1800 字符的摘要。摘要两两合并形成分层树，因此没有“最后再拼接全部摘要”的上下文爆炸。
- 全书摘要形成核心故事资料；最多 30 位核心人物，其他人物／细节仍保留在局部原文及摘要中，需要审核后纳入核心人物表。
- 分集时默认每组约 8 个原文片段，分层汇总后结合故事资料及前段结尾规划 1–12 集。总集数指定时按组分配额度。
- 这是首版分段规划，不是能证明零遗漏的全文推理。跨段剧情连接、人物别名和关键事件仍需用户核对。原文覆盖提示只能发现“未引用片段”，不能证明引用内容已完整改编。
- 编写一集时读取该集引用的摘要，单片段时额外提供完整原文；多片段材料通过分层摘要控制预算。
- 局部重写只将目标旧场景交给模型。Go 按场景 ID 合并，其余场景原样保留，结束状态标记为需要复核。

上下文预算暂用 UTF-8 字节数作为保守 token 上界并预留输出额度，不依赖未知模型 tokenizer。超过预算直接报错，绝不静默截断。`TEXT_API_CONTEXT_TOKENS` 应按照供应商实际能力配置。较复杂的剧本可能需要更大的额度；长篇处理会产生很多调用，应先小样本验收再批量处理。

## 4. 本地数据、版本与恢复

```text
data/agents/novel/
  projects/<novel-id>/
    source.txt                    规范化原文，独立保存
    project.json                  章节、片段、审核标志、版本引用
    documents/<document-id>/
      000001.json                 不可变的第 1 版
      000002.json                 新草稿，不覆盖第 1 版
    checkpoints/<hash>.json        模型步骤输出与可用的 usage
  runs/<run-id>.json               步骤完成记录、状态、单调事件序号
```

文档 ID 是 `analysis`、`bible`、`outline` 或 `episode-NNNN`。`analysis` 是内部阅读材料，不作为可编辑剧本。

一个文档引用同时保存 `current`（最新草稿）、`approved`（上次确认版）、`stale`（上游变化需复核）。确认旧版后再生成草稿，旧版仍存在；草稿不会被当成已确认稿继续执行。

人工编辑必须提交文档版本和项目版本，Go 检查是否仍然匹配。运行中禁止修改该项目；暂停并等待当前步骤完成后才允许编辑。编辑后旧任务不能拿旧输入继续发布结果，应新建任务；未变的步骤仍可以通过哈希复用。

运行状态：`running → pausing → paused`，或者 `running → succeeded / failed / canceled`。暂停等待当前请求完成并保存；取消停止后续请求并阻止迟到稿件发布，但无法保证供应商不计费。

服务启动扫描未完成任务并标记为暂停，不自动重发。人工继续会重新经过确定性的编排，成功检查点不重发。不声称外部调用 exactly-once：请求发出后、保存检查点前断电，供应商可能已经执行或计费。

每个项目只运行一个任务；不同小说的模型步骤也通过单独的锁串行，查询和取消不受长请求锁阻塞。首版是**单个 Go 进程的本机单用户部署**，不要让多个 Go 进程同时写同一个 data 目录。

原子写入顺序是：产物临时文件 → flush/sync → rename → 项目索引。磁盘故障可能留下未引用的临时／历史文件，不自动删除；错误不会被展示为已保存成功。备份时停止服务并复制整个 data 目录，不要只复制 project.json。

## 5. 接口与配置

所有浏览器请求都是相对路径 `/api/v1/...`，Vite 代理给 Go。文本 Key 只由 Python 的供应商适配器使用。

| 方法与路径 | 用途 |
| --- | --- |
| GET `/agents/config` | 文本配置是否齐全、模型名，不返回 Key |
| GET / POST `/novels` | 项目列表／multipart 导入，字段 title、encoding、text 或 file |
| GET / PATCH `/novels/{id}` | 项目详情／带 revision 的设置更新 |
| GET `/novels/{id}/chapters?offset=0` | 每页 100 个章节 |
| GET `/novels/{id}/sources/{sourceId}` | 原文片段和字符位置 |
| GET `/novels/{id}/checks` | 覆盖、估算时长和失效提示 |
| GET / PUT `/novels/{id}/documents/{docId}` | 查看版本／保存新草稿，GET 可带 revision |
| POST `/novels/{id}/documents/{docId}/approve` | 确认当前文档版本 |
| POST `/novels/{id}/agent-runs` | analyze、outline、script；可指定 targets、sceneId、instruction |
| GET `/agent-runs/{id}` | 持久化任务快照 |
| POST `/agent-runs/{id}/pause|resume|cancel` | 控制任务 |
| GET `/agent-runs/{id}/events` | SSE，事件 agent.updated，ID 为持久化 sequence |
| GET `/novels/{id}/export?format=markdown|json` | 导出最新稿，episodeId 可选 |

为了统一版本操作，故事资料、大纲和逐集剧本采用 `documents` 子资源，替代初始方案中各自独立的保存端点。业务含义不变。

SSE 恢复语义是“发送比 Last-Event-ID 更新的最新快照”，不是逐条事件重放。断线可用查询恢复；关键产物状态保存在磁盘，不能只存在前端事件中。

根目录 `.env` 配置见 README 和 `.env.example`。目前使用用户提供的聚算示例：`https://api.jusuanhub.com:10443/v1/chat/completions`、`qwen3-6-35b-a3b`、`stream:false`、文本 content 块。返回按 Chat Completions 的 `choices[0].message.content` 读取，usage 缺失时显示未知。

HTTP 429 最多退避重试三次。鉴权、余额、参数、5xx 和网络结果不明不盲目重发；界面保留错误供人工判断。结构错误最多修复一次，也可能计费。保守预算／输出截断错误要求调整材料或额度。

## 6. 如何增加下一个 Agent

1. 在 Python `worker/agents/<新名字>/` 定义步骤、系统规则、输出协议和校验器，通过 `agents/http.py` 注册白名单路由。
2. 复用 `providers/text.py`，不在新 Agent 内复制 Authorization、重试和响应解析逻辑。
3. 在 Go `internal/agents/<新名字>/` 定义自己的领域模型与编排；复用 core 的存储、运行类型和 Worker 客户端。
4. 新增独立的前端 `features/agents/<新名字>/` 页面、类型和 API，再加入 App 导航。
5. 编写无网络 fixture 与检查点测试。确认数据协议后，再进行由用户授权的小规模在线冒烟。

先采用清晰的目录和少量公共接口；等第二、第三个 Agent 真的出现后，再提炼重复编排逻辑，避免首版就创建难以学习的万能框架。

## 7. 测试与真实联调边界

`scripts/test.ps1` 先检查被 Git 跟踪的 `.env.example` 中没有非空 Key，再执行 Python 测试、Go 测试与前端类型检查／构建，任一失败返回失败。常规测试使用 Mock，不读用户 Key，不请求外部模型。真实 Key 只能放到被忽略的 `.env`。

已覆盖：编码、百万字分块覆盖、Unicode 下标、输出协议、一次修复、上下文拒绝、429 与不确定错误分类、首集审核门槛、5 集批次、版本保护、下游失效、暂停继续、重启恢复、重复启动与取消迟到结果。

浏览器验收使用仅存在于 `_test.go` 的隔离服务：`TestBrowserHarness`，通过 `NOVEL_BROWSER_TEST_ADDR` 和 `NOVEL_BROWSER_WEB_DIST` 显式启用；默认跳过。它不是生产 Mock 模式，测试数据保存在临时目录。

真实联调：配置 TEXT_API_KEY 后，手动运行 `scripts/smoke-text.ps1 -ConfirmCost`。该命令最多两次短文本请求、每次输出上限 1024 tokens，可能计费。未运行线上冒烟时不能把 Mock 验证称为真实供应商验证。

首版尚不包含：PDF／Word、网页抓取、向量检索、画布、镜头提示词、视频合成、云同步、多用户权限或多人同时写入。同一小说的章节边界在分析前可编辑；分析后若需要重新切章，请重新导入以保护已有引用。
