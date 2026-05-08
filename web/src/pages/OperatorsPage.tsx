import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Card,
  Collapse,
  Group,
  Loader,
  Modal,
  PasswordInput,
  Stack,
  Text,
  Title,
  Tooltip,
} from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import {
  IconChevronDown,
  IconChevronRight,
  IconKey,
  IconLock,
  IconPlus,
  IconShieldCheck,
  IconShieldOff,
  IconTrash,
} from '@tabler/icons-react';
import { api, ApiError } from '../api/client';
import type { ListOperatorsResponse, Operator } from '../api/types';
import { useAuthStore } from '../auth/store';
import { ConfirmModal } from '../components/ConfirmModal';
import { CreateOperatorModal } from '../components/CreateOperatorModal';
import { OperatorAccessPanel } from '../components/OperatorAccessPanel';
import { EmptyState } from '../components/ResourceTable';
import { formatTime } from '../utils/format';

export function OperatorsPage() {
  const isAdmin = useAuthStore((s) => s.isAdmin);
  const operatorEmail = useAuthStore((s) => s.operatorEmail);
  const qc = useQueryClient();

  const [createOpen, setCreateOpen] = useState(false);
  const [toDelete, setToDelete] = useState<Operator | null>(null);
  const [pwTarget, setPwTarget] = useState<Operator | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);

  const { data, isLoading, error } = useQuery({
    queryKey: ['operators'],
    queryFn: () => api.get<ListOperatorsResponse>('/v1/operators'),
    enabled: isAdmin,
  });

  const toggleAdminMu = useMutation({
    mutationFn: (op: Operator) =>
      api.patch(`/v1/${op.name}`, {
        operator: {
          name: op.name,
          isAdmin: !op.isAdmin,
        },
        updateMask: 'is_admin',
      }),
    onSuccess: () => {
      notifications.show({ color: 'green', title: '已更新', message: '管理员权限已切换' });
      qc.invalidateQueries({ queryKey: ['operators'] });
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError || e instanceof Error ? e.message : '请稍后重试';
      notifications.show({ color: 'red', title: '更新失败', message: msg });
    },
  });

  const deleteMu = useMutation({
    mutationFn: (op: Operator) => api.delete(`/v1/${op.name}`),
    onSuccess: () => {
      notifications.show({ color: 'green', title: '已删除', message: '操作员已删除' });
      qc.invalidateQueries({ queryKey: ['operators'] });
      setToDelete(null);
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError || e instanceof Error ? e.message : '请稍后重试';
      notifications.show({ color: 'red', title: '删除失败', message: msg });
    },
  });

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

  if (isLoading) return <Loader />;
  if (error) return <Alert color="red">{(error as Error).message}</Alert>;

  const operators = data?.operators ?? [];

  return (
    <Stack>
      <Group justify="space-between">
        <Title order={3}>操作员</Title>
        <Button leftSection={<IconPlus size={16} />} onClick={() => setCreateOpen(true)}>
          创建操作员
        </Button>
      </Group>

      <Card withBorder padding={0} radius="md">
        {operators.length === 0 ? (
          <EmptyState
            title="暂无操作员"
            hint={
              <>
                点击右上角{' '}
                <Text span fw={600}>
                  创建操作员
                </Text>{' '}
                按钮以新建账号。
              </>
            }
          />
        ) : (
          <Stack gap={0}>
            {operators.map((op) => {
              const isSelf = op.email === operatorEmail;
              const isExpanded = expanded === op.operatorId;
              return (
                <div
                  key={op.operatorId}
                  style={{
                    borderBottom: '1px solid var(--mantine-color-default-border)',
                  }}
                >
                  <Group
                    px="md"
                    py="sm"
                    justify="space-between"
                    wrap="nowrap"
                    style={{ cursor: 'pointer' }}
                    onClick={() => setExpanded(isExpanded ? null : op.operatorId)}
                  >
                    <Group gap="md" wrap="nowrap" style={{ flex: 1, minWidth: 0 }}>
                      <ActionIcon variant="subtle" size="sm" tabIndex={-1}>
                        {isExpanded ? (
                          <IconChevronDown size={14} />
                        ) : (
                          <IconChevronRight size={14} />
                        )}
                      </ActionIcon>
                      <div style={{ flex: 1, minWidth: 0 }}>
                        <Group gap="xs" wrap="nowrap">
                          <Text ff="monospace" size="sm" truncate>
                            {op.email}
                          </Text>
                          {op.isAdmin ? (
                            <Badge color="violet">管理员</Badge>
                          ) : (
                            <Badge color="gray">操作员</Badge>
                          )}
                          {isSelf && (
                            <Badge color="cyan" variant="light">
                              当前账号
                            </Badge>
                          )}
                        </Group>
                        <Text size="xs" c="dimmed">
                          {formatTime(op.createTime)} · ID {op.operatorId}
                        </Text>
                      </div>
                    </Group>
                    <Group
                      gap="xs"
                      wrap="nowrap"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <Tooltip
                        label={
                          isSelf
                            ? '不能修改自己的管理员权限'
                            : op.isAdmin
                            ? '撤销管理员权限'
                            : '授予管理员权限'
                        }
                      >
                        <ActionIcon
                          variant="subtle"
                          color={op.isAdmin ? 'orange' : 'violet'}
                          disabled={isSelf}
                          loading={toggleAdminMu.isPending && toggleAdminMu.variables?.operatorId === op.operatorId}
                          onClick={() => toggleAdminMu.mutate(op)}
                        >
                          {op.isAdmin ? (
                            <IconShieldOff size={16} />
                          ) : (
                            <IconShieldCheck size={16} />
                          )}
                        </ActionIcon>
                      </Tooltip>
                      <Tooltip label="重置密码">
                        <ActionIcon
                          variant="subtle"
                          onClick={() => setPwTarget(op)}
                        >
                          <IconKey size={16} />
                        </ActionIcon>
                      </Tooltip>
                      <Tooltip label={isSelf ? '不能删除自己' : '删除操作员'}>
                        <ActionIcon
                          variant="subtle"
                          color="red"
                          disabled={isSelf}
                          onClick={() => setToDelete(op)}
                        >
                          <IconTrash size={16} />
                        </ActionIcon>
                      </Tooltip>
                    </Group>
                  </Group>
                  <Collapse expanded={isExpanded}>
                    <div
                      style={{
                        padding: '0 16px 16px 48px',
                        background: 'var(--mantine-color-default-hover)',
                      }}
                    >
                      {isExpanded && <OperatorAccessPanel operator={op} />}
                    </div>
                  </Collapse>
                </div>
              );
            })}
          </Stack>
        )}
      </Card>

      <CreateOperatorModal opened={createOpen} onClose={() => setCreateOpen(false)} />

      <ChangePasswordModal
        operator={pwTarget}
        opened={pwTarget !== null}
        onClose={() => setPwTarget(null)}
      />

      <ConfirmModal
        opened={toDelete !== null}
        onClose={() => {
          if (!deleteMu.isPending) setToDelete(null);
        }}
        onConfirm={() => toDelete && deleteMu.mutate(toDelete)}
        loading={deleteMu.isPending}
        title="删除操作员"
        description={
          toDelete && (
            <Stack gap={4}>
              <Text size="sm">
                确定要删除操作员{' '}
                <Text span fw={600} ff="monospace">
                  {toDelete.email}
                </Text>{' '}
                吗？
              </Text>
              <Text size="xs" c="red">
                此操作不可恢复。删除后，该账号的会话将立即失效，所有项目权限同时被撤销。
              </Text>
            </Stack>
          )
        }
        confirmLabel="删除"
      />
    </Stack>
  );
}

// ---- ChangePasswordModal (defined here to keep file count tight) ----

type PwProps = {
  operator: Operator | null;
  opened: boolean;
  onClose: () => void;
};

function ChangePasswordModal({ operator, opened, onClose }: PwProps) {
  const form = useForm({
    initialValues: { password: '', confirm: '' },
    validate: {
      password: (v) => (v.length >= 8 ? null : '密码至少 8 位'),
      confirm: (v, vals) => (v === vals.password ? null : '两次输入的密码不一致'),
    },
  });

  const mu = useMutation({
    mutationFn: (vals: { password: string }) => {
      if (!operator) return Promise.resolve();
      return api.patch(`/v1/${operator.name}`, {
        operator: { name: operator.name },
        updateMask: 'password',
        password: vals.password,
      });
    },
    onSuccess: () => {
      notifications.show({ color: 'green', title: '密码已更新', message: '已重置该操作员密码' });
      form.reset();
      onClose();
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError || e instanceof Error ? e.message : '请稍后重试';
      notifications.show({ color: 'red', title: '更新失败', message: msg });
    },
  });

  return (
    <Modal
      opened={opened}
      onClose={() => {
        if (!mu.isPending) {
          form.reset();
          onClose();
        }
      }}
      title={`重置 ${operator?.email ?? ''} 的密码`}
      centered
    >
      <form onSubmit={form.onSubmit((vals) => mu.mutate(vals))}>
        <Stack>
          <PasswordInput
            required
            label="新密码"
            description="密码至少 8 位。"
            {...form.getInputProps('password')}
          />
          <PasswordInput
            required
            label="重复新密码"
            {...form.getInputProps('confirm')}
          />
          <Group justify="flex-end" mt="sm">
            <Button
              variant="default"
              onClick={() => {
                if (!mu.isPending) {
                  form.reset();
                  onClose();
                }
              }}
              disabled={mu.isPending}
            >
              取消
            </Button>
            <Button type="submit" loading={mu.isPending}>
              更新
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
