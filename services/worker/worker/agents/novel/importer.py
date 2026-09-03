"""无模型、无网络的小说导入器。所有位置均是 Unicode 字符下标，非 UTF-8 字节。

Go 使用 []rune，浏览器使用 Array.from(text)，确保中文/emoji 的引用位置一致。
保留完整规范化正文；章节只是正文上的区间，不复制或丢弃任何段落。
"""
from __future__ import annotations

import re

MAX_CHARACTERS = 1_000_000
MAX_BYTES = 20 * 1024 * 1024
HEADING = re.compile(r"(?m)^[ \t]*(?:第[零〇一二三四五六七八九十百千万两\d]+[章节回卷部].{0,70}|(?:序章|序言|楔子|尾声|后记|番外).{0,60}|Chapter\s+\d+.{0,60})[ \t]*$", re.IGNORECASE)


def import_novel(raw: bytes, encoding: str = "auto") -> dict:
    if not raw or len(raw) > MAX_BYTES:
        raise ValueError("小说文件不能为空，且不能超过 20 MiB")
    if encoding not in {"auto", "utf-8", "gb18030"}:
        raise ValueError("编码只支持 auto、utf-8、gb18030")
    candidates = ["utf-8-sig", "gb18030"] if encoding == "auto" else ["utf-8-sig" if encoding == "utf-8" else encoding]
    text = ""
    used = ""
    for candidate in candidates:
        try:
            text = raw.decode(candidate, errors="strict")
            used = candidate
            break
        except UnicodeDecodeError:
            continue
    if not used:
        raise ValueError("无法解码，请将文件另存为 UTF-8，或指定正确编码")
    text = text.replace("\r\n", "\n").replace("\r", "\n")
    if not text.strip() or "\x00" in text:
        raise ValueError("请上传非空的纯文本 TXT 文件")
    if len(text) > MAX_CHARACTERS:
        raise ValueError("首版最多支持 100 万字符，请分卷导入")
    boundaries = [(m.start(), m.group().strip()) for m in HEADING.finditer(text)]
    if len(boundaries) > 5000:
        raise ValueError("章节数超过 5000，请检查标题格式或分卷导入")
    warnings = []
    if not boundaries:
        warnings.append("未识别到章节标题，已按段落建立分段；可编辑章节边界。")
        boundaries = [(0, "分段 1")]
        cursor = 0
        while cursor + 6000 < len(text):
            end = text.rfind("\n", cursor + 3000, cursor + 6000)
            cursor = end + 1 if end >= 0 else cursor + 6000
            boundaries.append((cursor, f"分段 {len(boundaries) + 1}"))
    elif boundaries[0][0] != 0:
        boundaries.insert(0, (0, "卷首"))
    chapters = [
        {"id": f"chapter-{i + 1:04d}", "title": title, "start": start,
         "end": boundaries[i + 1][0] if i + 1 < len(boundaries) else len(text)}
        for i, (start, title) in enumerate(boundaries)
    ]
    if used == "gb18030":
        warnings.append("使用 GB18030 解码，请检查正文预览是否正确。")
    return {"text": text, "encoding": used, "chapters": chapters, "warnings": warnings}
