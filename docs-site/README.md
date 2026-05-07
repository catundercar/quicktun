# quicktun docs site

中文优先的 quicktun 文档站,基于 [Docusaurus 3](https://docusaurus.io/) + TypeScript。默认 locale `zh-CN`,英文为 stub。

## 本地运行

```bash
cd docs-site
npm install
npm start              # 默认开 zh-CN dev server
npm start -- --locale en   # 指定 en
```

dev server 默认 `http://localhost:3000/quicktun/`。

## 构建

```bash
npm run build
```

输出静态文件到 `build/`,可以丢任何静态托管(GitHub Pages、Netlify、nginx 等)。

跑 `npm run serve` 在本地预览构建产物。

## 增加新文档页

1. 在 `docs/<category>/<slug>.md` 创建中文页(带 `---\nsidebar_position: N\n---` 头)
2. 在 `sidebars.ts` 把 slug 加进对应 category 的 `items`
3. 在 `i18n/en/docusaurus-plugin-content-docs/current/<category>/<slug>.md` 创建对应的英文 stub:
   ```markdown
   # Title

   > 🚧 English translation in progress. See the [Chinese version](/zh-CN/docs/<path>) for now.
   ```
4. 跑 `npm run build` 验证没有 broken link

## 重新生成 i18n 占位

如果改了 navbar / footer / sidebar 标签,跑:

```bash
npm run write-translations -- --locale en
```

会把新增的 key 写到 `i18n/en/docusaurus-theme-classic/*.json` 和 `i18n/en/docusaurus-plugin-content-docs/current.json`,然后手动改 `message` 字段成英文。

## 目录结构

```
docs-site/
├── docusaurus.config.ts    站点配置(navbar, i18n, footer, prism)
├── sidebars.ts             侧边栏结构(显式定义,不用 autogen)
├── docs/                   中文文档源(zh-CN 是默认 locale)
│   ├── intro.md
│   ├── getting-started/
│   ├── architecture/
│   ├── deployment/
│   ├── cli/
│   └── config/
├── i18n/en/                英文翻译(目前都是 stub)
├── src/
│   ├── pages/index.tsx     自定义首页
│   └── css/custom.css      Infima 变量覆盖
└── static/                 静态资源
```

## 部署

`docusaurus.config.ts` 已配 GitHub Pages:

- `organizationName: catundercar`
- `projectName: quicktun`
- `baseUrl: /quicktun/`
- `deploymentBranch: gh-pages`

跑 `GIT_USER=<your-github> npm run deploy` 构建并推到 `gh-pages` 分支。
