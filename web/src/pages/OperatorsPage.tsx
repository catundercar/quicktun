import { useMemo, useState } from 'react';
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query';
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
  Stack,
  Text,
  TextInput,
  Title,
  Tooltip,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import {
  IconChevronDown,
  IconChevronRight,
  IconKey,
  IconLock,
  IconPlus,
  IconSearch,
  IconShieldCheck,
  IconShieldOff,
  IconTrash,
} from '@tabler/icons-react';
import { api, ApiError } from '../api/client';
import type { ListOperatorsResponse, Operator } from '../api/types';
import { useAuthStore } from '../auth/store';
import { ChangePasswordForm } from '../components/ChangePasswordForm';
import { ConfirmModal } from '../components/ConfirmModal';
import { CreateOperatorModal } from '../components/CreateOperatorModal';
import { OperatorAccessPanel } from '../components/OperatorAccessPanel';
import { EmptyState } from '../components/ResourceTable';
import { formatTime } from '../utils/format';

const PAGE_SIZE = 50;

export function OperatorsPage() {
  const isAdmin = useAuthStore((s) => s.isAdmin);
  const operatorEmail = useAuthStore((s) => s.operatorEmail);
  const qc = useQueryClient();

  const [createOpen, setCreateOpen] = useState(false);
  const [toDelete, setToDelete] = useState<Operator | null>(null);
  const [pwTarget, setPwTarget] = useState<Operator | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [search, setSearch] = useState('');

  const {
    data,
    isLoading,
    error,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useInfiniteQuery({
    queryKey: ['operators'],
    enabled: isAdmin,
    initialPageParam: '',
    queryFn: ({ pageParam }) => {
      const params = new URLSearchParams({ page_size: String(PAGE_SIZE) });
      if (pageParam) params.set('page_token', pageParam as string);
      return api.get<ListOperatorsResponse>(`/v1/operators?${params.toString()}`);
    },
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });

  const operators: Operator[] = useMemo(
    () => data?.pages.flatMap((p) => p.operators ?? []) ?? [],
    [data],
  );

  const filtered = useMemo(() => {
    if (!search.trim()) return operators;
    const q = search.trim().toLowerCase();
    return operators.filter(
      (o) =>
        o.email.toLowerCase().includes(q) ||
        o.operatorId.toLowerCase().includes(q),
    );
  }, [operators, search]);

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

  return (
    <Stack>
      <Group justify="space-between">
        <Title order={3}>操作员</Title>
        <Button leftSection={<IconPlus size={16} />} onClick={() => setCreateOpen(true)}>
          创建操作员
        </Button>
      </Group>

      <TextInput
        placeholder="按邮箱或 ID 搜索"
        leftSection={<IconSearch size={14} />}
        value={search}
        onChange={(e) => setSearch(e.currentTarget.value)}
      />

      <Card withBorder padding={0} radius="md">
        {filtered.length === 0 ? (
          <EmptyState
            title={search ? '未找到匹配的操作员' : '暂无操作员'}
            hint={
              search ? (
                '尝试调整搜索关键字。'
              ) : (
                <>
                  点击右上角{' '}
                  <Text span fw={600}>
                    创建操作员
                  </Text>{' '}
                  按钮以新建账号。
                </>
              )
            }
          />
        ) : (
          <Stack gap={0}>
            {filtered.map((op) => {
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
                          loading={
                            toggleAdminMu.isPending &&
                            toggleAdminMu.variables?.operatorId === op.operatorId
                          }
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
                        <ActionIcon variant="subtle" onClick={() => setPwTarget(op)}>
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

      <Group justify="space-between" align="center">
        <Text size="xs" c="dimmed">
          共 {operators.length} 位操作员
          {search ? ` · 匹配 ${filtered.length} 位` : ''}
        </Text>
        <Button
          variant="default"
          onClick={() => fetchNextPage()}
          disabled={!hasNextPage || isFetchingNextPage}
          loading={isFetchingNextPage}
        >
          {hasNextPage ? '加载更多' : '没有更多了'}
        </Button>
      </Group>

      <CreateOperatorModal opened={createOpen} onClose={() => setCreateOpen(false)} />

      <Modal
        opened={pwTarget !== null}
        onClose={() => setPwTarget(null)}
        title={`重置 ${pwTarget?.email ?? ''} 的密码`}
        centered
      >
        <ChangePasswordForm
          operator={pwTarget}
          onCancel={() => setPwTarget(null)}
          onSuccess={() => setPwTarget(null)}
          submitLabel="更新"
        />
      </Modal>

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
