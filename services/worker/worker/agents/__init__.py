"""Agent 目录：每个业务 Agent 独立成包，不把提示词塞进 HTTP 路由。

目前只有 novel（小说改编）；新增 Agent 时注册新的执行器即可。
供应商 HTTP 协议属于 providers，业务状态与持久化属于 Go，均不在这里实现。
"""
