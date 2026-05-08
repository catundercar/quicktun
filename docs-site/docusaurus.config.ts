import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';
// @ts-expect-error — no type declarations shipped by this package
import searchLocal from '@easyops-cn/docusaurus-search-local';

const config: Config = {
  title: 'quicktun',
  tagline: '多站点远程访问脚手架',
  favicon: 'img/favicon.ico',

  future: {
    v4: true,
  },

  url: 'https://catundercar.github.io',
  baseUrl: '/quicktun/',

  organizationName: 'catundercar',
  projectName: 'quicktun',
  deploymentBranch: 'gh-pages',
  trailingSlash: false,

  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'warn',

  i18n: {
    defaultLocale: 'zh-CN',
    locales: ['zh-CN', 'en'],
    localeConfigs: {
      'zh-CN': {label: '简体中文'},
      en: {label: 'English'},
    },
  },

  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          editUrl:
            'https://github.com/catundercar/quicktun/tree/main/docs-site/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themes: [
    [
      searchLocal,
      {
        hashed: true,
        language: ['en', 'zh'],
        indexBlog: false,
        indexPages: true,
        docsRouteBasePath: '/docs',
        searchResultLimits: 10,
        searchResultContextMaxLength: 50,
      },
    ],
  ],

  themeConfig: {
    image: 'img/docusaurus-social-card.jpg',
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'quicktun',
      logo: {
        alt: 'quicktun',
        src: 'img/logo.svg',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docs',
          position: 'left',
          label: '文档',
        },
        {
          type: 'localeDropdown',
          position: 'right',
        },
        {
          href: 'https://github.com/catundercar/quicktun',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: '文档',
          items: [
            {label: '快速开始', to: '/docs/getting-started/overview'},
            {label: '架构', to: '/docs/architecture/overview'},
            {label: '部署', to: '/docs/deployment/linux'},
          ],
        },
        {
          title: '项目',
          items: [
            {
              label: 'GitHub',
              href: 'https://github.com/catundercar/quicktun',
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} quicktun. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'yaml', 'toml', 'go', 'protobuf'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
