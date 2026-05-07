import type {ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import Heading from '@theme/Heading';

import styles from './index.module.css';

function HomepageHeader() {
  const {siteConfig} = useDocusaurusContext();
  return (
    <header className={clsx('hero', styles.heroBanner)}>
      <div className="container">
        <Heading as="h1" className={styles.heroTitle}>
          {siteConfig.title}
        </Heading>
        <p className={styles.heroSubtitle}>{siteConfig.tagline}</p>
        <p className={styles.heroDescription}>
          统一控制面 + 反向隧道 + 网络层准入,把多项目内网网点变成 SSH/RDP/AI 工具能直连的端口。
        </p>
        <div className={styles.buttons}>
          <Link
            className="button button--primary button--lg"
            to="/docs/getting-started/overview">
            开始使用 →
          </Link>
          <Link
            className={clsx('button button--secondary button--lg', styles.buttonSpaced)}
            to="https://github.com/catundercar/quicktun">
            GitHub 仓库
          </Link>
        </div>
      </div>
    </header>
  );
}

type FeatureItem = {
  title: string;
  description: string;
};

const features: FeatureItem[] = [
  {
    title: '网络层可达性',
    description:
      'agent 反向隧道穿透 NAT / CGNAT,跳板机一只脚公网、一只脚内网,operator 一行 forward 直连任意 LAN 端口。',
  },
  {
    title: 'Project 隔离',
    description:
      '每个 project 独立 rathole 进程、独立端口段,故障隔离、跨客户配置零串扰。relay_port_range 重叠会被拒。',
  },
  {
    title: '跨平台 agent',
    description:
      'Linux + systemd、macOS + launchd、Windows + NSSM 一键脚本部署。控制面只在 Linux,agent 三端齐全。',
  },
];

function HomepageFeatures() {
  return (
    <section className={styles.features}>
      <div className="container">
        <div className="row">
          {features.map((f) => (
            <div className={clsx('col col--4')} key={f.title}>
              <div className={styles.feature}>
                <Heading as="h3">{f.title}</Heading>
                <p>{f.description}</p>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function Architecture() {
  return (
    <section className={styles.architecture}>
      <div className="container">
        <Heading as="h2" className={styles.sectionTitle}>
          整体架构
        </Heading>
        <pre className={styles.diagram}>
{`┌──────────────────────────────────────────────────────┐
│            quicktun-server (公网 VPS)                 │
│  Control Plane (gRPC + grpc-gateway + SQLite)        │
│       │                  │                            │
│       ▼ supervises       ▼ supervises                 │
│  ┌─────────┐    ┌──────────────────┐                  │
│  │ rathole │    │ quicktun-        │                  │
│  │ :loopback│    │ authproxy :443  │                  │
│  └─────────┘    │ token + CONNECT  │                  │
└─────────────────┴──────────┬───────┴──────────────────┘
                             │ TCP/443 TLS
        ┌────────────────────┼────────────────────┐
        ▼                    ▼                    ▼
   ┌─────────┐          ┌─────────┐          ┌─────────┐
   │ Site A  │          │ Site B  │          │ Site C  │
   │ agent + │          │ agent + │          │ agent + │
   │ rathole │          │ rathole │          │ rathole │
   └────┬────┘          └─────────┘          └─────────┘
        │ 转发到内网 LAN
        ▼
   192.168.x.0/24`}
        </pre>
      </div>
    </section>
  );
}

function CallToAction() {
  return (
    <section className={styles.cta}>
      <div className="container">
        <Heading as="h2">准备好了?</Heading>
        <div className={styles.buttons}>
          <Link
            className="button button--primary button--lg"
            to="/docs/intro">
            查看文档
          </Link>
          <Link
            className={clsx('button button--secondary button--lg', styles.buttonSpaced)}
            to="https://github.com/catundercar/quicktun">
            GitHub 仓库
          </Link>
        </div>
      </div>
    </section>
  );
}

export default function Home(): ReactNode {
  const {siteConfig} = useDocusaurusContext();
  return (
    <Layout
      title={siteConfig.title}
      description={siteConfig.tagline}>
      <HomepageHeader />
      <main>
        <HomepageFeatures />
        <Architecture />
        <CallToAction />
      </main>
    </Layout>
  );
}
