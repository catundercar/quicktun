// ProfilePage — self-service "个人资料" page reachable from the user menu.
//
// The current operator object is fetched from the WhoAmI RPC on mount so we
// always have a fresh snapshot (the persisted auth store may lag behind a
// recent admin-side change to the operator's role).
//
// The page is split into three cards:
//   1. Profile summary (email / role / created at).
//   2. Change-password form, gated by re-entering the current password.
//   3. Current session info + a "logout" affordance.
import { useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import {
  Alert,
  Badge,
  Button,
  Card,
  Code,
  Group,
  Loader,
  Stack,
  Text,
  Title,
} from '@mantine/core';
import { IconLogout } from '@tabler/icons-react';
import { api } from '../api/client';
import type { WhoAmIResponse } from '../api/types';
import { useAuthStore } from '../auth/store';
import { ChangePasswordForm } from '../components/ChangePasswordForm';
import { formatTime } from '../utils/format';

function truncateToken(token: string | null): string {
  if (!token) return '(空)';
  if (token.length <= 12) return token;
  return `${token.slice(0, 8)}…${token.slice(-4)}`;
}

export function ProfilePage() {
  const nav = useNavigate();
  const token = useAuthStore((s) => s.token);
  const clearSession = useAuthStore((s) => s.clearSession);

  const whoamiQ = useQuery({
    queryKey: ['whoami'],
    queryFn: () => api.get<WhoAmIResponse>('/v1/auth:whoami'),
  });

  const operator = whoamiQ.data?.operator ?? null;

  return (
    <Stack>
      <Title order={3}>个人资料</Title>

      {whoamiQ.isLoading && <Loader />}
      {whoamiQ.error && (
        <Alert color="red">{(whoamiQ.error as Error).message}</Alert>
      )}

      {operator && (
        <Card withBorder padding="md" radius="md">
          <Stack gap="xs">
            <Group justify="space-between">
              <Text fw={600}>账号信息</Text>
              {operator.isAdmin ? (
                <Badge color="violet">管理员</Badge>
              ) : (
                <Badge color="gray">操作员</Badge>
              )}
            </Group>
            <Group gap="xs">
              <Text size="sm" c="dimmed" w={88}>
                邮箱
              </Text>
              <Text size="sm" ff="monospace">
                {operator.email}
              </Text>
            </Group>
            <Group gap="xs">
              <Text size="sm" c="dimmed" w={88}>
                操作员 ID
              </Text>
              <Text size="sm" ff="monospace">
                {operator.operatorId}
              </Text>
            </Group>
            <Group gap="xs">
              <Text size="sm" c="dimmed" w={88}>
                注册时间
              </Text>
              <Text size="sm">{formatTime(operator.createTime)}</Text>
            </Group>
          </Stack>
        </Card>
      )}

      <Card withBorder padding="md" radius="md">
        <Stack gap="md">
          <Text fw={600}>修改密码</Text>
          <Text size="xs" c="dimmed">
            为防止误操作，更新密码前需要再次确认当前密码。
          </Text>
          <ChangePasswordForm
            operator={operator}
            requireOldPassword
            submitLabel="更新密码"
          />
        </Stack>
      </Card>

      <Card withBorder padding="md" radius="md">
        <Stack gap="xs">
          <Text fw={600}>当前会话</Text>
          <Group gap="xs">
            <Text size="sm" c="dimmed" w={88}>
              Token
            </Text>
            <Code>{truncateToken(token)}</Code>
          </Group>
          <Text size="xs" c="dimmed">
            完整的多会话管理（查看/吊销其他设备）将在 Phase 4 提供。
          </Text>
          <Group justify="flex-end" mt="xs">
            <Button
              variant="default"
              leftSection={<IconLogout size={14} />}
              onClick={() => {
                clearSession();
                nav('/login');
              }}
            >
              退出当前会话
            </Button>
          </Group>
        </Stack>
      </Card>
    </Stack>
  );
}
