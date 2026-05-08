import {
  AppShell as MantineAppShell,
  NavLink,
  Group,
  Title,
  Menu,
  UnstyledButton,
  Avatar,
  Text,
  ActionIcon,
  Tooltip,
  useMantineColorScheme,
  useComputedColorScheme,
} from '@mantine/core';
import { IconLogout, IconMoon, IconSun, IconUser } from '@tabler/icons-react';
import { Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useAuthStore } from '../auth/store';
import { navItems } from './nav';

export function AppShell() {
  const loc = useLocation();
  const nav = useNavigate();
  const operatorEmail = useAuthStore((s) => s.operatorEmail);
  const isAdmin = useAuthStore((s) => s.isAdmin);
  const clearSession = useAuthStore((s) => s.clearSession);
  const { setColorScheme } = useMantineColorScheme();
  const computed = useComputedColorScheme('light', { getInitialValueInEffect: true });
  const isDark = computed === 'dark';

  return (
    <MantineAppShell
      header={{ height: 56 }}
      navbar={{ width: 220, breakpoint: 'sm' }}
      padding="md"
    >
      <MantineAppShell.Header>
        <Group h="100%" px="md" justify="space-between">
          <Title order={4}>quicktun</Title>
          <Group gap="sm">
            <Tooltip label={isDark ? '切换到浅色模式' : '切换到深色模式'}>
              <ActionIcon
                variant="default"
                size="lg"
                aria-label="切换主题"
                onClick={() => setColorScheme(isDark ? 'light' : 'dark')}
              >
                {isDark ? <IconSun size={16} /> : <IconMoon size={16} />}
              </ActionIcon>
            </Tooltip>
            <Menu>
              <Menu.Target>
                <UnstyledButton>
                  <Group gap="xs">
                    <Avatar size={28} radius="xl">
                      {operatorEmail?.[0]?.toUpperCase()}
                    </Avatar>
                    <div>
                      <Text size="sm">{operatorEmail}</Text>
                      {isAdmin && (
                        <Text size="xs" c="dimmed">
                          管理员
                        </Text>
                      )}
                    </div>
                  </Group>
                </UnstyledButton>
              </Menu.Target>
              <Menu.Dropdown>
                <Menu.Item
                  leftSection={<IconUser size={14} />}
                  onClick={() => nav('/profile')}
                >
                  个人资料
                </Menu.Item>
                <Menu.Item
                  leftSection={<IconLogout size={14} />}
                  onClick={() => {
                    clearSession();
                    nav('/login');
                  }}
                >
                  退出登录
                </Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
        </Group>
      </MantineAppShell.Header>
      <MantineAppShell.Navbar p="xs">
        {navItems
          .filter((it) => !it.adminOnly || isAdmin)
          .map((it) => (
            <NavLink
              key={it.path}
              label={it.label}
              leftSection={it.icon}
              active={loc.pathname.startsWith(it.path)}
              onClick={() => nav(it.path)}
            />
          ))}
      </MantineAppShell.Navbar>
      <MantineAppShell.Main>
        <Outlet />
      </MantineAppShell.Main>
    </MantineAppShell>
  );
}
