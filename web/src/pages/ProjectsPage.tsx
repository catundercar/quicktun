import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Badge,
  Button,
  Card,
  Group,
  Loader,
  Modal,
  Stack,
  Text,
  TextInput,
  Title,
  Tooltip,
  ActionIcon,
} from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import {
  IconArrowRight,
  IconEdit,
  IconPlus,
  IconSearch,
  IconTrash,
} from '@tabler/icons-react';
import { api, ApiError } from '../api/client';
import type { ListProjectsResponse, Project } from '../api/types';
import { ResourceTable, EmptyState } from '../components/ResourceTable';
import { ConfirmModal } from '../components/ConfirmModal';
import { EditProjectModal } from '../components/EditProjectModal';
import { formatTime } from '../utils/format';

const SLUG_RE = /^[a-z0-9][a-z0-9-]*[a-z0-9]$/;
const PAGE_SIZE = 50;

function statusBadge(status: string) {
  if (status === 'PROJECT_STATUS_ACTIVE') return <Badge color="green">运行中</Badge>;
  if (status === 'PROJECT_STATUS_DISABLED') return <Badge color="gray">已停用</Badge>;
  return <Badge color="gray">{status || '未知'}</Badge>;
}

export function ProjectsPage() {
  const qc = useQueryClient();
  const nav = useNavigate();
  const [createOpen, setCreateOpen] = useState(false);
  const [toDelete, setToDelete] = useState<Project | null>(null);
  const [toEdit, setToEdit] = useState<Project | null>(null);
  const [search, setSearch] = useState('');

  const {
    data,
    isLoading,
    error,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useInfiniteQuery({
    queryKey: ['projects', 'infinite'],
    initialPageParam: '',
    queryFn: ({ pageParam }) => {
      // ListProjectsRequest carries pagination inside the nested `page`
      // message — gRPC-gateway flattens these to dotted query params.
      const params = new URLSearchParams({ 'page.pageSize': String(PAGE_SIZE) });
      if (pageParam) params.set('page.pageToken', pageParam as string);
      return api.get<ListProjectsResponse>(`/v1/projects?${params.toString()}`);
    },
    getNextPageParam: (last) => last.page?.nextPageToken || undefined,
  });

  const projects: Project[] = useMemo(
    () => data?.pages.flatMap((p) => p.projects ?? []) ?? [],
    [data],
  );

  const filtered = useMemo(() => {
    if (!search.trim()) return projects;
    const q = search.trim().toLowerCase();
    return projects.filter(
      (p) =>
        p.displayName.toLowerCase().includes(q) ||
        p.projectId.toLowerCase().includes(q),
    );
  }, [projects, search]);

  const createForm = useForm({
    initialValues: { projectId: '', displayName: '', relayPortRange: '' },
    validate: {
      projectId: (v) => {
        if (!v) return '项目 ID 必填';
        if (v.length < 1 || v.length > 64) return '长度需在 1-64 之间';
        if (!SLUG_RE.test(v)) return '只允许小写字母、数字与中划线，且首尾必须为字母或数字';
        return null;
      },
      displayName: (v) => (v.trim() ? null : '显示名称必填'),
      relayPortRange: (v) =>
        /^\d+-\d+$/.test(v.trim()) ? null : '格式应为 起始端口-结束端口，如 20000-20099',
    },
  });

  const createMu = useMutation({
    mutationFn: async (vals: typeof createForm.values) => {
      const qs = new URLSearchParams({ project_id: vals.projectId }).toString();
      return api.post<Project>(`/v1/projects?${qs}`, {
        displayName: vals.displayName,
        relayPortRange: vals.relayPortRange.trim(),
      });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['projects'] });
      notifications.show({ color: 'green', title: '创建成功', message: '项目已创建' });
      setCreateOpen(false);
      createForm.reset();
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError || e instanceof Error ? e.message : '请稍后重试';
      notifications.show({ color: 'red', title: '创建失败', message: msg });
    },
  });

  const deleteMu = useMutation({
    mutationFn: (p: Project) => api.delete<void>(`/v1/${p.name}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['projects'] });
      notifications.show({ color: 'green', title: '删除成功', message: '项目已删除' });
      setToDelete(null);
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError || e instanceof Error ? e.message : '请稍后重试';
      notifications.show({ color: 'red', title: '删除失败', message: msg });
    },
  });

  if (isLoading) return <Loader />;
  if (error) return <Alert color="red">{(error as Error).message}</Alert>;

  return (
    <Stack>
      <Group justify="space-between">
        <Title order={3}>项目</Title>
        <Button leftSection={<IconPlus size={16} />} onClick={() => setCreateOpen(true)}>
          创建项目
        </Button>
      </Group>

      <TextInput
        placeholder="按名称或 ID 搜索"
        leftSection={<IconSearch size={14} />}
        value={search}
        onChange={(e) => setSearch(e.currentTarget.value)}
      />

      <Card withBorder padding={0} radius="md">
        {filtered.length === 0 ? (
          <EmptyState
            title={search ? '未找到匹配的项目' : '暂无项目'}
            hint={
              search ? (
                '尝试调整搜索关键字。'
              ) : (
                <>
                  点击右上角{' '}
                  <Text span fw={600}>
                    创建项目
                  </Text>{' '}
                  按钮以新建第一个项目。
                </>
              )
            }
          />
        ) : (
          <ResourceTable
            data={filtered}
            rowKey={(p) => p.name}
            columns={[
              { key: 'projectId', header: '名称', render: (p) => <Text ff="monospace">{p.projectId}</Text> },
              { key: 'displayName', header: '显示名称', render: (p) => p.displayName },
              { key: 'status', header: '状态', render: (p) => statusBadge(p.status) },
              {
                key: 'relayPortRange',
                header: '中继端口段',
                render: (p) => <Text ff="monospace">{p.relayPortRange}</Text>,
              },
              {
                key: 'createTime',
                header: '创建时间',
                render: (p) => (
                  <Text size="sm" c="dimmed">
                    {formatTime(p.createTime)}
                  </Text>
                ),
              },
              {
                key: 'actions',
                header: '操作',
                width: 200,
                render: (p) => (
                  <Group gap="xs" wrap="nowrap">
                    <Tooltip label="查看该项目下的站点">
                      <ActionIcon
                        variant="subtle"
                        onClick={(e) => {
                          e.stopPropagation();
                          nav(`/sites?project=${p.projectId}`);
                        }}
                      >
                        <IconArrowRight size={16} />
                      </ActionIcon>
                    </Tooltip>
                    <Tooltip label="编辑项目">
                      <ActionIcon
                        variant="subtle"
                        onClick={(e) => {
                          e.stopPropagation();
                          setToEdit(p);
                        }}
                      >
                        <IconEdit size={16} />
                      </ActionIcon>
                    </Tooltip>
                    <Tooltip label="删除项目">
                      <ActionIcon
                        variant="subtle"
                        color="red"
                        onClick={(e) => {
                          e.stopPropagation();
                          setToDelete(p);
                        }}
                      >
                        <IconTrash size={16} />
                      </ActionIcon>
                    </Tooltip>
                  </Group>
                ),
              },
            ]}
          />
        )}
      </Card>

      <Group justify="space-between" align="center">
        <Text size="xs" c="dimmed">
          共 {projects.length} 个项目
          {search ? ` · 匹配 ${filtered.length} 个` : ''}
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

      {/* Create modal */}
      <Modal
        opened={createOpen}
        onClose={() => {
          if (!createMu.isPending) {
            setCreateOpen(false);
            createForm.reset();
          }
        }}
        title="创建项目"
        centered
      >
        <form onSubmit={createForm.onSubmit((vals) => createMu.mutate(vals))}>
          <Stack>
            <TextInput
              required
              label="项目 ID"
              description="作为资源 slug 使用，例如 prod-web。1-64 个字符，仅小写字母、数字、中划线。"
              placeholder="prod-web"
              {...createForm.getInputProps('projectId')}
            />
            <TextInput
              required
              label="显示名称"
              placeholder="生产环境 Web"
              {...createForm.getInputProps('displayName')}
            />
            <TextInput
              required
              label="中继端口段"
              description="rathole 中继侧将在该范围内分配端口；不同项目之间不可重叠。"
              placeholder="20000-20099"
              {...createForm.getInputProps('relayPortRange')}
            />
            <Group justify="flex-end" mt="sm">
              <Button
                variant="default"
                onClick={() => {
                  setCreateOpen(false);
                  createForm.reset();
                }}
                disabled={createMu.isPending}
              >
                取消
              </Button>
              <Button type="submit" loading={createMu.isPending}>
                创建
              </Button>
            </Group>
          </Stack>
        </form>
      </Modal>

      {/* Edit modal */}
      <EditProjectModal
        project={toEdit}
        opened={toEdit !== null}
        onClose={() => setToEdit(null)}
      />

      {/* Delete confirm */}
      <ConfirmModal
        opened={toDelete !== null}
        onClose={() => {
          if (!deleteMu.isPending) setToDelete(null);
        }}
        onConfirm={() => toDelete && deleteMu.mutate(toDelete)}
        loading={deleteMu.isPending}
        title="删除项目"
        description={
          toDelete && (
            <Stack gap={4}>
              <Text size="sm">
                确定要删除项目{' '}
                <Text span fw={600} ff="monospace">
                  {toDelete.projectId}
                </Text>{' '}
                吗？
              </Text>
              <Text size="xs" c="red">
                此操作不可恢复。若该项目下仍存在站点或服务，删除将被拒绝。
              </Text>
            </Stack>
          )
        }
        confirmLabel="删除"
      />
    </Stack>
  );
}
