import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  docs: [
    'intro',
    {
      type: 'category',
      label: '快速开始',
      collapsed: false,
      items: [
        'getting-started/overview',
        'getting-started/install-server',
        'getting-started/install-agent',
      ],
    },
    {
      type: 'category',
      label: '架构',
      items: [
        'architecture/overview',
        'architecture/auth-proxy',
        'architecture/agent',
        'architecture/token-contract',
      ],
    },
    {
      type: 'category',
      label: '部署',
      items: [
        'deployment/linux',
        'deployment/macos',
        'deployment/windows',
        'deployment/nginx',
      ],
    },
    {
      type: 'category',
      label: '操作员 CLI',
      items: [
        'cli/login',
        'cli/projects',
        'cli/sites',
        'cli/services',
        'cli/forward',
        'cli/status',
      ],
    },
    {
      type: 'category',
      label: '配置参考',
      items: ['config/server', 'config/authproxy', 'config/agent'],
    },
    'monitoring',
    'troubleshooting',
  ],
};

export default sidebars;
