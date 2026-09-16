# Spark Website

Spark 的独立展示与介绍网站，采用 **Astro + Tailwind CSS** 构建。

## 技术栈

- **框架**：[Astro 5](https://astro.build/)（极速零 JS 默认输出，超高 SEO 评分，天然适合 GitHub Pages / 静态托管）
- **样式**：Tailwind CSS
- **图标与动效**：Lucide Icons + 原生轻量交互与平滑滚动

## 页面结构

- **首页 (Hero)**：Spark 核心价值、多 Agent 矩阵概览与一键安装复制
- **产品介绍 (About)**：Codex、Claude Code、Grok Build、One、OpenCode 等支持详情
- **功能亮点 (Features)**：多 Profile 隔离、智能兼容代理、集中式 MCP 编排、Skills 投影、Token 用量统计
- **界面与架构 (Demo)**：TUI 界面截图与底层数据流架构图
- **安装下载 (Download)**：npm 全局安装、bun/pnpm 安装、Go 源码编译与原地自升级
- **常见问题 (FAQ)**：安全隔离、Wire 协议转换、MCP 与 Skills 同步机制
- **联系与社区 (Community)**：LINUX DO 社区交流与 GitHub 仓库反馈

## 本地开发与构建

```bash
# 进入网站目录
cd website

# 安装依赖
npm install

# 启动本地开发服务 (http://localhost:4321)
npm run dev

# 静态打包构建 (输出至 website/dist)
npm run build

# 本地预览打包产物
npm run preview
```
