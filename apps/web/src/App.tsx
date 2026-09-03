import { useState } from "react";
import VideoWorkspace from "./features/video/VideoWorkspace";
import NovelWorkspace from "./features/agents/novel/NovelWorkspace";

/** 应用壳只负责导航，不负责小说业务和视频调用。
 * 两个页面保持挂载，切换时保留未提交的表单；后续 Agent 可以作为独立入口加入。
 */
export default function App() {
  const [page, setPage] = useState<"video" | "novel">("novel");
  return <>
    <nav className="workspace-nav" aria-label="工作台导航">
      <strong>LOCAL VIDEO LAB</strong>
      <button type="button" aria-current={page === "novel" ? "page" : undefined} onClick={() => setPage("novel")}>小说改编 Agent</button>
      <button type="button" aria-current={page === "video" ? "page" : undefined} onClick={() => setPage("video")}>视频生成</button>
    </nav>
    <div hidden={page !== "novel"}><NovelWorkspace /></div>
    <div hidden={page !== "video"}><VideoWorkspace /></div>
  </>;
}
