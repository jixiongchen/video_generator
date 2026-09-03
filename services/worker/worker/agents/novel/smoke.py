"""显式授权后才运行的文本接口冒烟；不由 CI 或服务启动流程调用。"""
import argparse
import json
from dataclasses import replace

from .agent import NovelAgent
from ...providers.text import TextConfig, TextProvider


def main():
    parser = argparse.ArgumentParser(description="Manual text API smoke test (may incur charges)")
    parser.add_argument("--confirm-cost", action="store_true", required=True)
    parser.parse_args()
    # 限制测试输出额度和原文长度。仅验证请求/响应/JSON，不代表验证长篇改编质量。
    config = replace(TextConfig.from_env(), output_tokens=1024)
    result = NovelAgent(TextProvider(config)).execute("summarize", {
        "sourceIds": ["smoke-001"],
        "text": "林舟在车站收到一封旧信。信里写着家人的地址，他收好信，决定第二天出发寻找家人。",
    })
    print(json.dumps({"status": "succeeded", "model": config.model, "usage": result["usage"], "output": result["output"]}, ensure_ascii=False))


if __name__ == "__main__": main()
