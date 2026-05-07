import { Alert, Badge, Card, Code, Stack, Text, Title } from '@mantine/core';
import { IconInfoCircle, IconLock } from '@tabler/icons-react';
import { useAuthStore } from '../auth/store';
import { ResourceTable } from '../components/ResourceTable';

/**
 * Operators page.
 *
 * Phase 1 has no OperatorService — operators are seeded via the
 * `quicktun-server admin create-operator` CLI. There is no REST/gRPC endpoint
 * for listing or creating operators, so this page is intentionally read-only
 * and explains the CLI workflow.
 *
 * The current operator is still shown as a single row (sourced from the auth
 * store) so the page is not empty.
 */
export function OperatorsPage() {
  const isAdmin = useAuthStore((s) => s.isAdmin);
  const operatorEmail = useAuthStore((s) => s.operatorEmail);

  if (!isAdmin) {
    return (
      <Stack>
        <Title order={3}>操作员</Title>
        <Alert color="gray" icon={<IconLock size={16} />} title="无权访问">
          您没有权限管理操作员。请联系管理员账号。
        </Alert>
      </Stack>
    );
  }

  const rows = operatorEmail
    ? [
        {
          name: 'self',
          email: operatorEmail,
          isAdmin: true,
        },
      ]
    : [];

  return (
    <Stack>
      <Title order={3}>操作员</Title>

      <Alert
        color="blue"
        icon={<IconInfoCircle size={16} />}
        title="操作员管理仅支持 CLI"
      >
        <Stack gap={4}>
          <Text size="sm">
            当前控制平面尚未提供操作员管理的 gRPC 接口。请在服务端使用
            CLI 创建或删除操作员：
          </Text>
          <Code block>
            quicktun-server admin create-operator --email=alice@example.com --password=&lt;pw&gt; --admin
          </Code>
          <Text size="xs" c="dimmed">
            列出现有操作员可使用 <Code>quicktun-server admin list-operators</Code>。
          </Text>
        </Stack>
      </Alert>

      <Card withBorder padding={0} radius="md">
        <ResourceTable
          data={rows}
          rowKey={(r) => r.name}
          empty="尚未登录任何操作员账户。"
          columns={[
            { key: 'email', header: '邮箱', render: (r) => <Text ff="monospace">{r.email}</Text> },
            {
              key: 'role',
              header: '角色',
              render: (r) =>
                r.isAdmin ? (
                  <Badge color="violet">管理员</Badge>
                ) : (
                  <Badge color="gray">操作员</Badge>
                ),
            },
            {
              key: 'note',
              header: '备注',
              render: () => (
                <Text size="xs" c="dimmed">
                  当前会话
                </Text>
              ),
            },
          ]}
        />
      </Card>
    </Stack>
  );
}
