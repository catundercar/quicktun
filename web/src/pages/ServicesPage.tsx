import { useEffect, useMemo, useState } from 'react';
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
  CopyButton,
  Group,
  Loader,
  Modal,
  NumberInput,
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
  IconEdit,
  IconPlus,
  IconSearch,
  IconTrash,
} from '@tabler/icons-react';
import { api, ApiError } from '../api/client';
import type {
  ListProjectsResponse,
  ListServicesResponse,
  ListSitesResponse,
  Service,
} from '../api/types';
import { ResourceTable, EmptyState } from '../components/ResourceTable';
import { ConfirmModal } from '../components/ConfirmModal';
import { EditServiceModal } from '../components/EditServiceModal';

const SLUG_RE = /^[a-z0-9][a-z0-9-]*[a-z0-9]$/;
const PAGE_SIZE = 50;

function protoBadge(p: string) {
  if (p === 'PROTO_TCP') return <Badge color="blue">TCP</Badge>;
  if (p === 'PROTO_UDP') return <Badge color="violet">UDP</Badge>;
  return <Badge color="gray">{p || '-'}</Badge>;
}

// Suggest a sensible default --local-port for a forward. SSH/RDP/VNC have
// well-known shadow ports (2222, 13389, 15900) that almost never collide, and
// for everything else we offset the relay port by 10_000 to keep it out of
// privileged range. Falls back to 10000 when no relay port has been assigned
// yet so the snippet is still pasteable.
function defaultLocalPort(svc: Service): number {
  switch (svc.targetPort) {
    case 22:
      return 2222;
    case 3389:
      return 13389;
    case 5900:
      return 15900;
    default:
      return svc.relayPort ? svc.relayPort + 10000 : 10000;
  }
}

function forwardCommand(svc: Service): string {
  return `quicktun forward ${svc.name} --local-port ${defaultLocalPort(svc)}`;
}

export function ServicesPage() {
  const qc = useQueryClient();
  const [projectSlug, setProjectSlug] = useState<string>('');
  const [siteSlug, setSiteSlug] = useState<string>('');
  const [createOpen, setCreateOpen] = useState(false);
  const [toDelete, setToDelete] = useState<Service | null>(null);
  const [toEdit, setToEdit] = useState<Service | null>(null);
  const [search, setSearch] = useState('');

  const projectsQ = useQuery({
    queryKey: ['projects'],
    queryFn: () => api.get<ListProjectsResponse>('/v1/projects'),
  });
  const projects = projectsQ.data?.projects ?? [];

  // Auto-pick first project once data loads.
  useEffect(() => {
    if (!projectSlug && projects.length > 0) {
      setProjectSlug(projects[0].projectId);
    }
  }, [projects, projectSlug]);

  const sitesQ = useQuery({
    queryKey: ['sites', projectSlug],
    queryFn: () =>
      api.get<ListSitesResponse>(`/v1/projects/${projectSlug}/sites`),
    enabled: !!projectSlug,
  });
  const sites = sitesQ.data?.sites ?? [];

  // Auto-pick first site whenever the project changes.
  useEffect(() => {
    if (!projectSlug) {
      setSiteSlug('');
      return;
    }
    if (sites.length === 0) {
      setSiteSlug('');
      return;
    }
    if (!sites.some((s) => s.siteId === siteSlug)) {
      setSiteSlug(sites[0].siteId);
    }
  }, [sites, projectSlug, siteSlug]);

  const servicesQ = useInfiniteQuery({
    queryKey: ['services', projectSlug, siteSlug],
    enabled: !!projectSlug && !!siteSlug,
    initialPageParam: '',
    queryFn: ({ pageParam }) => {
      const params = new URLSearchParams({ 'page.pageSize': String(PAGE_SIZE) });
      if (pageParam) params.set('page.pageToken', pageParam as string);
      return api.get<ListServicesResponse>(
        `/v1/projects/${projectSlug}/sites/${siteSlug}/services?${params.toString()}`,
      );
    },
    getNextPageParam: (last) => last.page?.nextPageToken || undefined,
  });

  const services: Service[] = useMemo(
    () => servicesQ.data?.pages.flatMap((p) => p.services ?? []) ?? [],
    [servicesQ.data],
  );

  const filteredServices = useMemo(() => {
    if (!search.trim()) return services;
    const q = search.trim().toLowerCase();
    return services.filter(
      (s) =>
        s.displayName.toLowerCase().includes(q) ||
        s.serviceId.toLowerCase().includes(q) ||
        s.targetAddr.toLowerCase().includes(q),
    );
  }, [services, search]);

  const projectOptions = useMemo(
    () =>
      projects.map((p) => ({
        value: p.projectId,
        label: `${p.displayName} (${p.projectId})`,
      })),
    [projects],
  );

  const siteOptions = useMemo(
    () =>
      sites.map((s) => ({
        value: s.siteId,
        label: `${s.displayName} (${s.siteId})`,
      })),
    [sites],
  );

  const createForm = useForm({
    initialValues: {
      serviceId: '',
      displayName: '',
      targetAddr: '127.0.0.1',
      targetPort: 22,
      proto: 'PROTO_TCP',
    },
    validate: {
      serviceId: (v) => {
        if (!v) return '服务 ID 必填';
        if (v.length < 1 || v.length > 64) return '长度需在 1-64 之间';
        if (!SLUG_RE.test(v)) return '只允许小写字母、数字与中划线，且首尾必须为字母或数字';
        return null;
      },
      displayName: (v) => (v.trim() ? null : '显示名称必填'),
      targetAddr: (v) => (v.trim() ? null : '目标地址必填'),
      targetPort: (v) => (v >= 1 && v <= 65535 ? null : '端口需在 1-65535'),
    },
  });

  const createMu = useMutation({
    mutationFn: async (vals: typeof createForm.values) => {
      const qs = new URLSearchParams({ service_id: vals.serviceId }).toString();
      return api.post<Service>(
        `/v1/projects/${projectSlug}/sites/${siteSlug}/services?${qs}`,
        {
          displayName: vals.displayName,
          targetAddr: vals.targetAddr.trim(),
          targetPort: vals.targetPort,
          proto: vals.proto,
        },
      );
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['services', projectSlug, siteSlug] });
      notifications.show({ color: 'green', title: '创建成功', message: '服务已创建' });
      setCreateOpen(false);
      createForm.reset();
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError || e instanceof Error ? e.message : '请稍后重试';
      notifications.show({ color: 'red', title: '创建失败', message: msg });
    },
  });

  const deleteMu = useMutation({
    mutationFn: (s: Service) => api.delete<void>(`/v1/${s.name}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['services', projectSlug, siteSlug] });
      notifications.show({ color: 'green', title: '删除成功', message: '服务已删除' });
      setToDelete(null);
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError || e instanceof Error ? e.message : '请稍后重试';
      notifications.show({ color: 'red', title: '删除失败', message: msg });
    },
  });

  if (projectsQ.isLoading) return <Loader />;
  if (projectsQ.error)
    return <Alert color="red">{(projectsQ.error as Error).message}</Alert>;

  if (projects.length === 0) {
    return (
      <Stack>
        <Title order={3}>服务</Title>
        <Card withBorder>
          <EmptyState
            title="尚未创建任何项目"
            hint="请先创建项目和站点，再添加服务转发规则。"
          />
        </Card>
      </Stack>
    );
  }

  return (
    <Stack>
      <Group justify="space-between" wrap="wrap">
        <Group>
          <Title order={3}>服务</Title>
          <Select
            data={projectOptions}
            value={projectSlug}
            onChange={(v) => v && setProjectSlug(v)}
            placeholder="选择项目"
            searchable
            allowDeselect={false}
            w={220}
          />
          <Select
            data={siteOptions}
            value={siteSlug}
            onChange={(v) => v && setSiteSlug(v)}
            placeholder={sites.length === 0 ? '该项目无站点' : '选择站点'}
            searchable
            allowDeselect={false}
            disabled={sites.length === 0}
            w={220}
          />
        </Group>
        <Button
          leftSection={<IconPlus size={16} />}
          onClick={() => setCreateOpen(true)}
          disabled={!projectSlug || !siteSlug}
        >
          创建服务
        </Button>
      </Group>

      <TextInput
        placeholder="按名称、ID 或目标地址搜索"
        leftSection={<IconSearch size={14} />}
        value={search}
        onChange={(e) => setSearch(e.currentTarget.value)}
      />

      <Card withBorder padding={0} radius="md">
        {sitesQ.isLoading ? (
          <Group p="md">
            <Loader size="sm" />
          </Group>
        ) : sites.length === 0 ? (
          <EmptyState
            title="该项目下暂无站点"
            hint="请先在「站点」页面为该项目创建站点。"
          />
        ) : servicesQ.isLoading ? (
          <Group p="md">
            <Loader size="sm" />
          </Group>
        ) : servicesQ.error ? (
          <Alert color="red" m="md">
            {(servicesQ.error as Error).message}
          </Alert>
        ) : filteredServices.length === 0 ? (
          <EmptyState
            title={search ? '未找到匹配的服务' : '该站点下暂无服务'}
            hint={
              search
                ? '尝试调整搜索关键字。'
                : '点击右上角创建服务，将在中继侧自动分配端口。'
            }
          />
        ) : (
          <ResourceTable
            data={filteredServices}
            rowKey={(s) => s.name}
            columns={[
              {
                key: 'serviceId',
                header: '名称',
                render: (s) => <Text ff="monospace">{s.serviceId}</Text>,
              },
              { key: 'displayName', header: '显示名称', render: (s) => s.displayName },
              {
                key: 'target',
                header: '目标',
                render: (s) => (
                  <Text ff="monospace" size="sm">
                    {s.targetAddr}:{s.targetPort}
                  </Text>
                ),
              },
              { key: 'proto', header: '协议', render: (s) => protoBadge(s.proto) },
              {
                key: 'relayPort',
                header: '中继端口',
                render: (s) => (
                  <Text ff="monospace" size="sm">
                    {s.relayPort ?? '-'}
                  </Text>
                ),
              },
              {
                key: 'actions',
                header: '操作',
                width: 180,
                render: (s) => (
                  <Group gap="xs" wrap="nowrap">
                    <CopyButton value={forwardCommand(s)} timeout={1500}>
                      {({ copied, copy }) => (
                        <Tooltip
                          label={
                            copied
                              ? '已复制'
                              : `复制 forward 命令（本地端口 ${defaultLocalPort(s)}）`
                          }
                        >
                          <ActionIcon
                            variant="subtle"
                            color={copied ? 'teal' : 'gray'}
                            onClick={copy}
                          >
                            {copied ? <IconCheck size={16} /> : <IconCopy size={16} />}
                          </ActionIcon>
                        </Tooltip>
                      )}
                    </CopyButton>
                    <Tooltip label="编辑服务">
                      <ActionIcon
                        variant="subtle"
                        onClick={() => setToEdit(s)}
                      >
                        <IconEdit size={16} />
                      </ActionIcon>
                    </Tooltip>
                    <Tooltip label="删除服务">
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
          共 {services.length} 个服务
          {search ? ` · 匹配 ${filteredServices.length} 个` : ''}
        </Text>
        <Button
          variant="default"
          onClick={() => servicesQ.fetchNextPage()}
          disabled={!servicesQ.hasNextPage || servicesQ.isFetchingNextPage}
          loading={servicesQ.isFetchingNextPage}
        >
          {servicesQ.hasNextPage ? '加载更多' : '没有更多了'}
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
        title="创建服务"
        centered
      >
        <form onSubmit={createForm.onSubmit((vals) => createMu.mutate(vals))}>
          <Stack>
            <Group grow>
              <TextInput label="所属项目" value={projectSlug} readOnly />
              <TextInput label="所属站点" value={siteSlug} readOnly />
            </Group>
            <TextInput
              required
              label="服务 ID"
              description="例如 ssh、postgres。1-64 个字符，仅小写字母、数字、中划线。"
              placeholder="ssh"
              {...createForm.getInputProps('serviceId')}
            />
            <TextInput
              required
              label="显示名称"
              placeholder="SSH 转发"
              {...createForm.getInputProps('displayName')}
            />
            <Group grow>
              <TextInput
                required
                label="目标地址"
                description="站点可达的 IP 或主机名。"
                placeholder="127.0.0.1"
                {...createForm.getInputProps('targetAddr')}
              />
              <NumberInput
                required
                label="目标端口"
                min={1}
                max={65535}
                {...createForm.getInputProps('targetPort')}
              />
            </Group>
            <Select
              label="协议"
              data={[
                { value: 'PROTO_TCP', label: 'TCP' },
                { value: 'PROTO_UDP', label: 'UDP' },
              ]}
              {...createForm.getInputProps('proto')}
              allowDeselect={false}
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
      <EditServiceModal
        service={toEdit}
        opened={toEdit !== null}
        onClose={() => setToEdit(null)}
        projectSlug={projectSlug}
        siteSlug={siteSlug}
      />

      {/* Delete confirm */}
      <ConfirmModal
        opened={toDelete !== null}
        onClose={() => {
          if (!deleteMu.isPending) setToDelete(null);
        }}
        onConfirm={() => toDelete && deleteMu.mutate(toDelete)}
        loading={deleteMu.isPending}
        title="删除服务"
        description={
          toDelete && (
            <Text size="sm">
              确定要删除服务{' '}
              <Text span fw={600} ff="monospace">
                {toDelete.serviceId}
              </Text>{' '}
              吗？此操作不可恢复，对应的 rathole 转发规则将立即停止。
            </Text>
          )
        }
        confirmLabel="删除"
      />
    </Stack>
  );
}
