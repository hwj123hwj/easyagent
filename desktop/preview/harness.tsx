// Temporary visual-check harness for SkillMarketPanel (not shipped).
// Renders the real panel against a mocked market API, themed like the app.
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { SkillMarketPanel } from "../src/components/SkillMarketPanel";
import { useStore } from "../src/store";
import "../src/styles/app.css";

const SECTIONS = [
  { id: 1, name: "演示与设计" },
  { id: 2, name: "文档办公" },
  { id: 3, name: "浏览器与测试" },
  { id: 4, name: "社媒运营" },
  { id: 5, name: "开发工具" },
  { id: 6, name: "阅读与知识" },
  { id: 7, name: "视频创作" },
];

const SKILLS = [
  {
    id: 1, name: "sanwan-blackboard-ppt", display_name: "三万同款板书 PPT", category: "demo", version: "1.0.0",
    description: '生成"三万同款"板书风格 PPT——白板满画面 + 硬笔书法字 + 手绘彩色插图 + 每页必有戴红色龙虾帽的拉布拉多吉祥物。当用户要求制作板书风/白板手写风/三万风格的 PPT、演示文稿、讲解图时使用。',
    tags: ["ppt", "手绘"], install_count: 30, featured: true, size_bytes: 12700, sha256: null,
    section_id: 1, section_name: "演示与设计", icon_url: null,
  },
  {
    id: 2, name: "pptx-deck", display_name: "PPT 演示文稿", category: "demo", version: "1.0.0",
    description: "当任务以任何方式涉及 .pptx 或 .potx 文件（作为输入、输出或两者）时使用：创建幻灯片/路演稿/演示文稿，读取解析或提取文本，编辑修改现有演示文稿，合并拆分幻灯片文件，处理模板、版式、演讲者备注或批注。用户提到\"幻灯片\"\"演示文稿\"或 .pptx/.potx 文件名时即触发。",
    tags: ["pptx", "office"], install_count: 60, featured: false, size_bytes: 171000, sha256: null,
    section_id: 1, section_name: "演示与设计", icon_url: null,
  },
  {
    id: 3, name: "web-ppt", display_name: "网页 PPT 生成", category: "demo", version: "1.0.1",
    description: "生成横向翻页网页 PPT（单 HTML 文件），含 WebGL 背景、章节幕封、数据大字报、图片网格与键盘翻页导航，适合在浏览器中直接演示与分享。",
    tags: ["html", "webgl"], install_count: 36, featured: true, size_bytes: 1990000, sha256: null,
    section_id: 1, section_name: "演示与设计", icon_url: null,
  },
  {
    id: 4, name: "word-docx", display_name: "Word 文档", category: "doc", version: "1.0.0",
    description: "当用户需要创建、读取、编辑或处理 Word 文档（.docx）或 Word 模板（.dotx）时使用。涵盖：新建文档、插入标题与段落、表格、图片、页眉页脚、样式与目录，批量替换与格式转换。",
    tags: ["docx", "office"], install_count: 54, featured: false, size_bytes: 168000, sha256: null,
    section_id: 2, section_name: "文档办公", icon_url: "data:image/svg+xml;utf8,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 38 38'%3E%3Crect width='38' height='38' rx='9' fill='%232b7cd3'/%3E%3Ctext x='19' y='25' font-size='14' font-family='sans-serif' font-weight='700' fill='white' text-anchor='middle'%3EWo%3C/text%3E%3C/svg%3E",
  },
  {
    id: 5, name: "pdf-doc", display_name: "PDF 文档", category: "doc", version: "1.0.0",
    description: "处理 PDF 文件的读取、拆分、合并、加水印与格式转换；提取文本与图片，生成报表式 PDF。",
    tags: ["pdf"], install_count: 47, featured: false, size_bytes: 214000, sha256: null,
    section_id: 2, section_name: "文档办公", icon_url: null,
  },
  {
    id: 6, name: "xlsx-sheet", display_name: "电子表格", category: "doc", version: "1.0.1",
    description: "创建与编辑 .xlsx/.csv 表格：公式、图表、数据透视、条件格式与批量数据处理。",
    tags: ["xlsx", "csv"], install_count: 88, featured: false, size_bytes: 190000, sha256: null,
    section_id: 2, section_name: "文档办公", icon_url: null,
  },
  {
    id: 7, name: "browser-auto", display_name: "浏览器自动化", category: "browser", version: "2.1.0",
    description: "驱动本地浏览器完成网页操作：打开页面、填表、点击、截图与数据抓取，支持登录态保持与多标签页。",
    tags: ["browser", "e2e"], install_count: 121, featured: false, size_bytes: 83000, sha256: null,
    section_id: 3, section_name: "浏览器与测试", icon_url: null,
  },
  {
    id: 8, name: "social-calendar", display_name: "社媒内容日历", category: "social", version: "1.2.0",
    description: "规划一周社媒发帖日历：选题、文案、配图建议与最佳发布时间，一键输出为表格或日历视图。这是一个刻意写得非常长的描述用来验证两行截断是否生效，后面还有更多内容更多内容更多内容。",
    tags: ["社媒", "文案", "日历", "extra"], install_count: 19, featured: false, size_bytes: 9000, sha256: null,
    section_id: 4, section_name: "社媒运营", icon_url: null,
  },
  {
    id: 9, name: "git-helper", display_name: "Git 助手", category: "dev", version: "1.0.0",
    description: "规范提交信息、整理分支、生成 changelog 与发布说明。",
    tags: ["git"], install_count: 73, featured: false, size_bytes: 12000, sha256: null,
    section_id: 5, section_name: "开发工具", icon_url: null,
  },
];

// A second page so the load-more button is exercised.
const SKILLS_PAGE2 = SKILLS.slice(0, 5).map((s) => ({ ...s, id: s.id + 100, name: s.name + "-b", install_count: 5 }));

const INSTALLED = [
  { id: 2, name: "pptx-deck", display_name: "PPT 演示文稿", version: "1.0.0", installed_at: "2026-10-01T10:00:00Z", dir: "/tmp/skills/pptx-deck" },
  { id: 6, name: "xlsx-sheet", display_name: "电子表格", version: "1.0.1", installed_at: "2026-10-03T10:00:00Z", dir: "/tmp/skills/xlsx-sheet" },
  { id: 9, name: "git-helper", display_name: "Git 助手", version: "1.0.0", installed_at: "2026-10-09T10:00:00Z", dir: "/tmp/skills/git-helper" },
];

const realFetch = window.fetch.bind(window);
window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
  const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
  const path = url.replace(/^https?:\/\/[^/]+/, "");
  const reply = (body: unknown) =>
    new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
  if (path.startsWith("/skills/market/sections")) return reply({ sections: SECTIONS });
  if (path.startsWith("/skills/market/installed")) return reply({ installed: INSTALLED });
  if (/^\/skills\/market\/skills\/\d+/.test(path)) {
    return reply({
      ...SKILLS[0],
      usage_example: "帮我制作一套介绍猎豹移动董事长傅盛的板书风 PPT，包含他的个人经历、猎豹移动的发展历程、几次关键转型和 AI 方向，整体像白板手写，配一些彩色插画。",
      preview_images: [
        "data:image/svg+xml;utf8,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 640 360'%3E%3Crect width='640' height='360' fill='%23f5f2ea'/%3E%3Ctext x='320' y='190' font-size='42' font-family='serif' fill='%23222' text-anchor='middle'%3E傅盛 · 板书风 PPT%3C/text%3E%3C/svg%3E",
        "data:image/svg+xml;utf8,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 640 360'%3E%3Crect width='640' height='360' fill='%23eef3f7'/%3E%3Ctext x='320' y='190' font-size='42' font-family='serif' fill='%23222' text-anchor='middle'%3E第 2 页 · 发展历程%3C/text%3E%3C/svg%3E",
      ],
      example_files: [
        { name: "board-style-demo.pptx", url: "https://example.test/board-style-demo.pptx", size: 1048576, mime_type: "application/vnd.openxmlformats-officedocument.presentationml.presentation" },
      ],
    });
  }
  if (path.startsWith("/skills/market/search")) {
    const q = new URL(path, "http://x").searchParams;
    const page = Number(q.get("page") || "1");
    if (q.get("section") === "2") return reply({ skills: SKILLS.filter((s) => s.section_id === 2), total: 3, page: 1 });
    if (page >= 2) return reply({ skills: SKILLS_PAGE2, total: 14, page });
    return reply({ skills: SKILLS, total: 14, page: 1 });
  }
  return realFetch(input as Parameters<typeof realFetch>[0], init);
};

useStore.setState({ lang: "zh" });

const route = new URLSearchParams(location.search).get("route") || "market";
if (route === "dark" || route === "dark-skeleton" || route === "dark-detail") {
  document.documentElement.setAttribute("data-theme", "dark");
}
if (route === "skeleton" || route === "dark-skeleton") {
  // Freeze the loading state by never resolving the first search.
  window.fetch = () => new Promise(() => {});
}

const root = createRoot(document.getElementById("root")!);
root.render(
  <StrictMode>
    <main style={{ maxWidth: 1080, margin: "0 auto", padding: "48px 40px" }}>
      <h1 style={{ fontSize: 28, margin: "0 0 8px" }}>技能市场</h1>
      <p style={{ color: "var(--text-dim)", margin: "0 0 28px" }}>浏览并安装官方技能市场里的技能。</p>
      <SkillMarketPanel />
    </main>
  </StrictMode>,
);

// Switch to the installed-skills view / open a card's detail view.
if (route === "installed" || route === "detail" || route === "dark-detail") {
  setTimeout(() => {
    if (route === "installed") {
      const btn = [...document.querySelectorAll(".skill-market-sort button")].find(
        (b) => b.textContent?.includes("已安装"),
      ) as HTMLButtonElement | undefined;
      btn?.click();
      return;
    }
    const card = document.querySelector<HTMLElement>(".skill-market-card");
    card?.click();
  }, 400);
}
