import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query';
import {
  Alert,
  Badge,
  Button,
  Card,
  Code,
  CopyButton,
  Group,
  Loader,
  Modal,
  Select,
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
  IconCheck,
  IconCopy,
  IconDownload,
  IconEdit,
  IconKey,
  IconPlus,
  IconSearch,
  IconTrash,
} from '@tabler/icons-react';
import { api, ApiError } from '../api/client';
import type {
  ListProjectsResponse,
  ListSitesResponse,
  Project,
  RotateAgentTokenResponse,
  Site,
} from '../api/types';
import { ResourceTable, EmptyState } from '../components/ResourceTable';
import { ConfirmModal } from '../components/ConfirmModal';
import { EditSiteModal } from '../components/EditSiteModal';
import { InstallCommandModal } from '../components/InstallCommandModal';
import { formatTime } from '../utils/format';

const SLUG_RE = /^[a-z0-9][a-z0-9-]*[a-z0-9]$/;
const PAGE_SIZE = 50;

function statusBadge(status: string) {
  if (status === 'SITE_STATUS_ONLINE') return <Badge color="green">在线</Badge>;
  if (status === 'SITE_STATUS_OFFLINE') return <Badge color="red">离线</Badge>;
  if (status === 'SITE_STATUS_PENDING') return <Badge color="yellow">待激活</Badge>;
  return <Badge color="gray">{status || '未知'}</Badge>;
}

export function SitesPage() {
  const qc = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const initialProject = searchParams.get('project') ?? '';
  const [projectSlug, setProjectSlug] = useState<string>(initialProject);
  const [createOpen, setCreateOpen] = useState(false);
  const [toDelete, setToDelete] = useState<Site | null>(null);
  const [toRotate, setToRotate] = useState<Site | null>(null);
  const [rotatedToken, setRotatedToken] = useState<string | null>(null);
  const [toEdit, setToEdit] = useState<Site | null>(null);
  const [toInstall, setToInstall] = useState<Site | null>(null);
  const [search, setSearch] = useState('');

  // Load project list for the selector.
  const projectsQ = useQuery({
    queryKey: ['projects'],
    queryFn: () => api.get<ListProjectsResponse>('/v1/projects'),
  });

  const projects = projectsQ.data?.projects ?? [];

  // Pick the first project automatically once data arrives.
  useEffect(() => {
    if (!projectSlug && projects.length > 0) {
      setProjectSlug(projects[0].projectId);
    }
  }, [projects, projectSlug]);

  // Keep the URL in sync (so a user can share a deep link).
  useEffect(() => {
    if (!projectSlug) return;
    if (searchParams.get('project') !== projectSlug) {
      const next = new URLSearchParams(searchParams);
      next.set('project', projectSlug);
      setSearchParams(next, { replace: true });
    }
  }, [projectSlug, searchParams, setSearchParams]);

  const sitesQ = useInfiniteQuery({
    queryKey: ['sites', 'infinite', projectSlug],
    enabled: !!projectSlug,
    initialPageParam: '',
    queryFn: ({ pageParam }) => {
      const params = new URLSearchParams({ 'page.pageSize': String(PAGE_SIZE) });
      if (pageParam) params.set('page.pageToken', pageParam as string);
      return api.get<ListSitesResponse>(
        `/v1/projects/${projectSlug}/sites?${params.toString()}`,
      );
    },
    getNextPageParam: (last) => last.page?.nextPageToken || undefined,
  });

  const sites: Site[] = useMemo(
    () => sitesQ.data?.pages.flatMap((p) => p.sites ?? []) ?? [],
    [sitesQ.data],
  );

  const filteredSites = useMemo(() => {
    if (!search.trim()) return sites;
    const q = search.trim().toLowerCase();
    return sites.filter(
      (s) =>
        s.displayName.toLowerCase().includes(q) ||
        s.siteId.toLowerCase().includes(q) ||
        (s.hostname ?? '').toLowerCase().includes(q),
    );
  }, [sites, search]);

  const projectOptions = useMemo(
    () =>
      projects.map((p: Project) => ({
        value: p.projectId,
        label: `${p.displayName} (${p.projectId})`,
      })),
    [projects],
  );

  const createForm = useForm({
    initialValues: { siteId: '', displayName: '' },
    validate: {
      siteId: (v) => {
        if (!v) return '站点 ID 必填';
        if (v.length < 1 || v.length > 64) return '长度需在 1-64 之间';
        if (!SLUG_RE.test(v)) return '只允许小写字母、数字与中划线，且首尾必须为字母或数字';
        return null;
      },
      displayName: (v) => (v.trim() ? null : '显示名称必填'),
    },
  });

  const createMu = useMutation({
    mutationFn: async (vals: typeof createForm.values) => {
      const qs = new URLSearchParams({ site_id: vals.siteId }).toString();
      return api.post<Site>(`/v1/projects/${projectSlug}/sites?${qs}`, {
        displayName: vals.displayName,
      });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['sites', projectSlug] });
      notifications.show({ color: 'green', title: '创建成功', message: '站点已创建' });
      setCreateOpen(false);
      createForm.reset();
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError || e instanceof Error ? e.message : '请稍后重试';
      notifications.show({ color: 'red', title: '创建失败', message: msg });
    },
  });

  const deleteMu = useMutation({
    mutationFn: (s: Site) => api.delete<void>(`/v1/${s.name}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['sites', projectSlug] });
      notifications.show({ color: 'green', title: '删除成功', message: '站点已删除' });
      setToDelete(null);
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError || e instanceof Error ? e.message : '请稍后重试';
      notifications.show({ color: 'red', title: '删除失败', message: msg });
    },
  });

  const rotateMu = useMutation({
    mutationFn: (s: Site) =>
      api.post<RotateAgentTokenResponse>(`/v1/${s.name}:rotateAgentToken`, {}),
    onSuccess: (resp) => {
      setRotatedToken(resp.token);
      setToRotate(null);
      notifications.show({
        color: 'green',
        title: '已生成新 token',
        message: '请立即复制并妥善保存。',
      });
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError || e instanceof Error ? e.message : '请稍后重试';
      notifications.show({ color: 'red', title: '生成失败', message: msg });
    },
  });

  if (projectsQ.isLoading) return <Loader />;
  if (projectsQ.error)
    return <Alert color="red">{(projectsQ.error as Error).message}</Alert>;

  if (projects.length === 0) {
    return (
      <Stack>
        <Title order={3}>站点</Title>
        <Card withBorder>
          <EmptyState
            title="尚未创建任何项目"
            hint="请先在「项目」页面创建一个项目，再添加站点。"
          />
        </Card>
      </Stack>
    );
  }

  return (
    <Stack>
      <Group justify="space-between" wrap="wrap">
        <Group>
          <Title order={3}>站点</Title>
          <Select
            data={projectOptions}
            value={projectSlug}
            onChange={(v) => v && setProjectSlug(v)}
            placeholder="选择项目"
            searchable
            allowDeselect={false}
            w={260}
          />
        </Group>
        <Button
          leftSection={<IconPlus size={16} />}
          onClick={() => setCreateOpen(true)}
          disabled={!projectSlug}
        >
          创建站点
        </Button>
      </Group>

      <TextInput
        placeholder="按显示名、ID 或主机名搜索"
        leftSection={<IconSearch size={14} />}
        value={search}
        onChange={(e) => setSearch(e.currentTarget.value)}
      />

      <Card withBorder padding={0} radius="md">
        {sitesQ.isLoading ? (
          <Group p="md">
            <Loader size="sm" />
          </Group>
        ) : sitesQ.error ? (
          <Alert color="red" m="md">
            {(sitesQ.error as Error).message}
          </Alert>
        ) : filteredSites.length === 0 ? (
          <EmptyState
            title={search ? '未找到匹配的站点' : '该项目下暂无站点'}
            hint={
              search
                ? '尝试调整搜索关键字。'
                : '点击右上角创建站点，并将生成的 agent token 部署到目标主机。'
            }
          />
        ) : (
          <ResourceTable
            data={filteredSites}
            rowKey={(s) => s.name}
            columns={[
              { key: 'siteId', header: '名称', render: (s) => <Text ff="monospace">{s.siteId}</Text> },
              { key: 'displayName', header: '显示名称', render: (s) => s.displayName },
              { key: 'status', header: '状态', render: (s) => statusBadge(s.status) },
              {
                key: 'lastSeenTime',
                header: '最后心跳',
                render: (s) => (
                  <Text size="sm" c="dimmed">
                    {formatTime(s.lastSeenTime)}
                  </Text>
                ),
              },
              {
                key: 'hostname',
                header: '主机名',
                render: (s) => (
                  <Text size="sm" ff="monospace">
                    {s.hostname || '-'}
                  </Text>
                ),
              },
              {
                key: 'actions',
                header: '操作',
                width: 220,
                render: (s) => (
                  <Group gap="xs" wrap="nowrap">
                    <Tooltip label="显示安装命令">
                      <ActionIcon
                        variant="subtle"
                        onClick={() => setToInstall(s)}
                      >
                        <IconDownload size={16} />
                      </ActionIcon>
                    </Tooltip>
                    <Tooltip label="编辑站点">
                      <ActionIcon
                        variant="subtle"
                        onClick={() => setToEdit(s)}
                      >
                        <IconEdit size={16} />
                      </ActionIcon>
                    </Tooltip>
                    <Tooltip label="重新生成 agent token">
                      <ActionIcon
                        variant="subtle"
                        onClick={() => setToRotate(s)}
                      >
                        <IconKey size={16} />
                      </ActionIcon>
                    </Tooltip>
                    <Tooltip label="删除站点">
                      <ActionIcon
                        variant="subtle"
                        color="red"
                        onClick={() => setToDelete(s)}
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
          共 {sites.length} 个站点
          {search ? ` · 匹配 ${filteredSites.length} 个` : ''}
        </Text>
        <Button
          variant="default"
          onClick={() => sitesQ.fetchNextPage()}
          disabled={!sitesQ.hasNextPage || sitesQ.isFetchingNextPage}
          loading={sitesQ.isFetchingNextPage}
        >
          {sitesQ.hasNextPage ? '加载更多' : '没有更多了'}
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
        title="创建站点"
        centered
      >
        <form onSubmit={createForm.onSubmit((vals) => createMu.mutate(vals))}>
          <Stack>
            <TextInput
              label="所属项目"
              value={projectSlug}
              readOnly
              styles={{ input: { fontFamily: 'monospace' } }}
            />
            <TextInput
              required
              label="站点 ID"
              description="例如 office-bastion。1-64 个字符，仅小写字母、数字、中划线。"
              placeholder="office-bastion"
              {...createForm.getInputProps('siteId')}
            />
            <TextInput
              required
              label="显示名称"
              placeholder="北京办公室跳板机"
              {...createForm.getInputProps('displayName')}
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
      <EditSiteModal
        site={toEdit}
        opened={toEdit !== null}
        onClose={() => setToEdit(null)}
        projectSlug={projectSlug}
      />

      {/* Install command modal */}
      <InstallCommandModal
        siteName={toInstall?.name ?? null}
        opened={toInstall !== null}
        onClose={() => setToInstall(null)}
      />

      {/* Delete confirm */}
      <ConfirmModal
        opened={toDelete !== null}
        onClose={() => {
          if (!deleteMu.isPending) setToDelete(null);
        }}
        onConfirm={() => toDelete && deleteMu.mutate(toDelete)}
        loading={deleteMu.isPending}
        title="删除站点"
        description={
          toDelete && (
            <Stack gap={4}>
              <Text size="sm">
                确定要删除站点{' '}
                <Text span fw={600} ff="monospace">
                  {toDelete.siteId}
                </Text>{' '}
                吗？
              </Text>
              <Text size="xs" c="red">
                此操作不可恢复。若该站点下仍存在服务，删除将被拒绝。
              </Text>
            </Stack>
          )
        }
        confirmLabel="删除"
      />

      {/* Rotate token confirm */}
      <ConfirmModal
        opened={toRotate !== null}
        onClose={() => {
          if (!rotateMu.isPending) setToRotate(null);
        }}
        onConfirm={() => toRotate && rotateMu.mutate(toRotate)}
        loading={rotateMu.isPending}
        title="重新生成 agent token"
        confirmColor="orange"
        confirmLabel="生成新 token"
        description={
          toRotate && (
            <Stack gap={4}>
              <Text size="sm">
                即将为站点{' '}
                <Text span fw={600} ff="monospace">
                  {toRotate.siteId}
                </Text>{' '}
                生成全新的 agent token。
              </Text>
              <Text size="xs" c="orange">
                旧 token 将立即失效，使用旧 token 的 agent 会断开连接。
              </Text>
            </Stack>
          )
        }
      />

      {/* One-time token result */}
      <Modal
        opened={rotatedToken !== null}
        onClose={() => setRotatedToken(null)}
        title="新 agent token"
        centered
        size="lg"
        closeOnClickOutside={false}
      >
        <Stack>
          <Alert color="orange" title="此 token 仅显示一次">
            请立即复制并妥善保存。关闭此对话框后将无法再次查看。
          </Alert>
          <Code
            block
            style={{ wordBreak: 'break-all', whiteSpace: 'pre-wrap', userSelect: 'all' }}
          >
            {rotatedToken}
          </Code>
          <Group justify="flex-end">
            <CopyButton value={rotatedToken ?? ''} timeout={2000}>
              {({ copied, copy }) => (
                <Button
                  leftSection={
                    copied ? <IconCheck size={16} /> : <IconCopy size={16} />
                  }
                  color={copied ? 'green' : 'blue'}
                  variant={copied ? 'light' : 'filled'}
                  onClick={copy}
                >
                  {copied ? '已复制' : '复制 token'}
                </Button>
              )}
            </CopyButton>
            <Button variant="default" onClick={() => setRotatedToken(null)}>
              我已保存
            </Button>
          </Group>
        </Stack>
      </Modal>
    </Stack>
  );
}
