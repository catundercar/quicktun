import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Group,
  Loader,
  Modal,
  Select,
  Stack,
  Table,
  Text,
  Tooltip,
} from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import { IconPlus, IconTrash } from '@tabler/icons-react';
import { api, ApiError } from '../api/client';
import type {
  ListProjectAccessResponse,
  ListProjectsResponse,
  Operator,
  OperatorProjectAccess,
} from '../api/types';
import { formatTime } from '../utils/format';

const ROLE_OPTIONS = [
  { value: 'viewer', label: '查看者 (viewer)' },
  { value: 'operator', label: '操作员 (operator)' },
  { value: 'admin', label: '项目管理员 (admin)' },
];

function roleBadge(role: string) {
  if (role === 'admin') return <Badge color="violet">admin</Badge>;
  if (role === 'operator') return <Badge color="blue">operator</Badge>;
  if (role === 'viewer') return <Badge color="gray">viewer</Badge>;
  return <Badge color="gray">{role}</Badge>;
}

type Props = {
  operator: Operator;
};

/**
 * Inline panel listing the OperatorProjectAccess rows for one operator,
 * with controls to grant a new project access or revoke an existing one.
 *
 * Note: when `operator.isAdmin === true`, the backend returns an empty list
 * because admins have implicit access to everything; we surface this as a
 * banner instead of "no records".
 */
export function OperatorAccessPanel({ operator }: Props) {
  const qc = useQueryClient();
  const [grantOpen, setGrantOpen] = useState(false);

  const accessQ = useQuery({
    queryKey: ['operator-access', operator.operatorId],
    queryFn: () =>
      api.get<ListProjectAccessResponse>(`/v1/${operator.name}/projectAccess`),
  });

  // Cached across panels (5 min staleTime) to avoid hammering the API.
  const projectsQ = useQuery({
    queryKey: ['projects'],
    queryFn: () => api.get<ListProjectsResponse>('/v1/projects'),
    staleTime: 60_000,
  });

  const grantForm = useForm({
    initialValues: { projectSlug: '', role: 'viewer' as string },
    validate: {
      projectSlug: (v) => (v ? null : '请选择项目'),
      role: (v) => (v ? null : '请选择角色'),
    },
  });

  const grantMu = useMutation({
    mutationFn: (vals: { projectSlug: string; role: string }) =>
      api.post(`/v1/${operator.name}/projectAccess`, {
        operator: operator.name,
        projectSlug: vals.projectSlug,
        role: vals.role,
      }),
    onSuccess: () => {
      notifications.show({ color: 'green', title: '已授权', message: '项目访问已添加' });
      qc.invalidateQueries({ queryKey: ['operator-access', operator.operatorId] });
      setGrantOpen(false);
      grantForm.reset();
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError || e instanceof Error ? e.message : '请稍后重试';
      notifications.show({ color: 'red', title: '授权失败', message: msg });
    },
  });

  const revokeMu = useMutation({
    mutationFn: (a: OperatorProjectAccess) =>
      api.delete(`/v1/${operator.name}/projectAccess/${a.projectSlug}`),
    onSuccess: () => {
      notifications.show({ color: 'green', title: '已撤销', message: '项目访问已撤销' });
      qc.invalidateQueries({ queryKey: ['operator-access', operator.operatorId] });
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError || e instanceof Error ? e.message : '请稍后重试';
      notifications.show({ color: 'red', title: '撤销失败', message: msg });
    },
  });

  if (operator.isAdmin) {
    return (
      <Alert color="blue" variant="light">
        管理员对所有项目均有完整访问权限，无需单独授权。
      </Alert>
    );
  }

  if (accessQ.isLoading) {
    return (
      <Group justify="center" p="md">
        <Loader size="sm" />
      </Group>
    );
  }
  if (accessQ.error) {
    return <Alert color="red">{(accessQ.error as Error).message}</Alert>;
  }

  const access = accessQ.data?.access ?? [];
  const projects = projectsQ.data?.projects ?? [];
  const grantedSlugs = new Set(access.map((a) => a.projectSlug));
  const availableProjects = projects.filter((p) => !grantedSlugs.has(p.projectId));

  return (
    <Stack gap="xs">
      <Group justify="space-between">
        <Text size="sm" fw={600}>
          项目访问
        </Text>
        <Button
          size="xs"
          leftSection={<IconPlus size={14} />}
          onClick={() => setGrantOpen(true)}
          disabled={availableProjects.length === 0}
        >
          授权项目
        </Button>
      </Group>

      {access.length === 0 ? (
        <Text size="xs" c="dimmed">
          该操作员尚未被授权访问任何项目。
        </Text>
      ) : (
        <Table withTableBorder withColumnBorders verticalSpacing="xs">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>项目</Table.Th>
              <Table.Th style={{ width: 140 }}>角色</Table.Th>
              <Table.Th style={{ width: 160 }}>授权时间</Table.Th>
              <Table.Th style={{ width: 60 }}>操作</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {access.map((a) => (
              <Table.Tr key={a.projectSlug}>
                <Table.Td>
                  <Text ff="monospace" size="sm">
                    {a.projectSlug}
                  </Text>
                </Table.Td>
                <Table.Td>{roleBadge(a.role)}</Table.Td>
                <Table.Td>
                  <Text size="xs" c="dimmed">
                    {formatTime(a.grantTime)}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Tooltip label="撤销该项目的访问">
                    <ActionIcon
                      variant="subtle"
                      color="red"
                      loading={revokeMu.isPending}
                      onClick={() => revokeMu.mutate(a)}
                    >
                      <IconTrash size={14} />
                    </ActionIcon>
                  </Tooltip>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}

      <Modal
        opened={grantOpen}
        onClose={() => {
          if (!grantMu.isPending) {
            setGrantOpen(false);
            grantForm.reset();
          }
        }}
        title={`为 ${operator.email} 授权项目`}
        centered
      >
        <form onSubmit={grantForm.onSubmit((vals) => grantMu.mutate(vals))}>
          <Stack>
            <Select
              required
              label="项目"
              placeholder="选择项目"
              data={availableProjects.map((p) => ({
                value: p.projectId,
                label: `${p.projectId} — ${p.displayName}`,
              }))}
              searchable
              {...grantForm.getInputProps('projectSlug')}
            />
            <Select
              required
              label="角色"
              data={ROLE_OPTIONS}
              allowDeselect={false}
              {...grantForm.getInputProps('role')}
            />
            <Group justify="flex-end" mt="sm">
              <Button
                variant="default"
                onClick={() => {
                  setGrantOpen(false);
                  grantForm.reset();
                }}
                disabled={grantMu.isPending}
              >
                取消
              </Button>
              <Button type="submit" loading={grantMu.isPending}>
                授权
              </Button>
            </Group>
          </Stack>
        </form>
      </Modal>
    </Stack>
  );
}
