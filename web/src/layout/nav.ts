import {
  IconLayoutDashboard,
  IconBriefcase,
  IconServer,
  IconNetwork,
  IconUsers,
} from '@tabler/icons-react';
import type { ReactNode } from 'react';
import { createElement } from 'react';

const sz = 16;

export const navItems: { path: string; label: string; icon: ReactNode }[] = [
  { path: '/dashboard', label: '仪表盘', icon: createElement(IconLayoutDashboard, { size: sz }) },
  { path: '/projects', label: '项目', icon: createElement(IconBriefcase, { size: sz }) },
  { path: '/sites', label: '站点', icon: createElement(IconServer, { size: sz }) },
  { path: '/services', label: '服务', icon: createElement(IconNetwork, { size: sz }) },
  { path: '/operators', label: '操作员', icon: createElement(IconUsers, { size: sz }) },
];
