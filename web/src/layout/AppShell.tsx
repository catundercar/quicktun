import {
  AppShell as MantineAppShell,
  NavLink,
  Group,
  Title,
  Menu,
  UnstyledButton,
  Avatar,
  Text,
} from '@mantine/core';
import { IconLogout } from '@tabler/icons-react';
import { Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useAuthStore } from '../auth/store';
import { navItems } from './nav';

export function AppShell() {
  const loc = useLocation();
  const nav = useNavigate();
  const operatorEmail = useAuthStore((s) => s.operatorEmail);
  const isAdmin = useAuthStore((s) => s.isAdmin);
  const clearSession = useAuthStore((s) => s.clearSession);

  return (
    <MantineAppShell
      header={{ height: 56 }}
      navbar={{ width: 220, breakpoint: 'sm' }}
      padding="md"
    >
      <MantineAppShell.Header>
        <Group h="100%" px="md" justify="space-between">
          <Title order={4}>quicktun</Title>
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
