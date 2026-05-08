import { useState } from 'react';
import { useInfiniteQuery } from '@tanstack/react-query';
import {
  Alert,
  Badge,
  Button,
  Card,
  Code,
  Collapse,
  Group,
  Input,
  Loader,
  Modal,
  Stack,
  Text,
  TextInput,
  Title,
  Tooltip,
} from '@mantine/core';
import {
  IconAdjustmentsHorizontal,
  IconLock,
  IconRefresh,
  IconSearch,
} from '@tabler/icons-react';
import { api } from '../api/client';
import type { AuditLogEntry, ListAuditLogsResponse } from '../api/types';
import { useAuthStore } from '../auth/store';
import { ResourceTable, EmptyState } from '../components/ResourceTable';
import { formatDateTime } from '../utils/format';

type Filters = {
  operatorEmail: string;
  projectSlug: string;
  actionPrefix: string;
  /** datetime-local value (e.g. "2026-04-01T08:00") */
  since: string;
  /** datetime-local value */
  until: string;
};

const EMPTY_FILTERS: Filters = {
  operatorEmail: '',
  projectSlug: '',
  actionPrefix: '',
  since: '',
  until: '',
};

const PAGE_SIZE = 50;

function toIsoOrEmpty(localValue: string): string {
  // Convert "<input type=datetime-local>" value (local time, no zone) into
  // ISO-8601 with timezone, since the backend expects google.protobuf.Timestamp.
  if (!localValue) return '';
  const t = Date.parse(localValue);
  if (Number.isNaN(t)) return '';
  return new Date(t).toISOString();
}

function buildQuery(applied: Filters, pageToken: string): string {
  const params = new URLSearchParams();
  params.set('page_size', String(PAGE_SIZE));
  if (pageToken) params.set('page_token', pageToken);
  if (applied.operatorEmail.trim()) params.set('operator_email', applied.operatorEmail.trim());
  if (applied.projectSlug.trim()) params.set('project_slug', applied.projectSlug.trim());
  if (applied.actionPrefix.trim()) params.set('action_prefix', applied.actionPrefix.trim());
  const sinceIso = toIsoOrEmpty(applied.since);
  const untilIso = toIsoOrEmpty(applied.until);
  if (sinceIso) params.set('since', sinceIso);
  if (untilIso) params.set('until', untilIso);
  return params.toString();
}

function actionBadge(action: string) {
  const verb = action.split('.').pop() || action;
  let color = 'gray';
  if (verb === 'create' || verb === 'grant') color = 'green';
  else if (verb === 'delete' || verb === 'revoke') color = 'red';
  else if (verb === 'update' || verb === 'rotate') color = 'blue';
  else if (verb === 'login' || verb.startsWith('login')) color = 'cyan';
  return (
    <Badge color={color} variant="light">
      {action}
    </Badge>
  );
}

function formatExtra(extraJson?: string): string {
  if (!extraJson) return '';
  try {
    const parsed = JSON.parse(extraJson);
    return JSON.stringify(parsed, null, 2);
  } catch {
    return extraJson;
  }
}

export function AuditPage() {
  const isAdmin = useAuthStore((s) => s.isAdmin);

  // Two-tier state: `draft` is what's in inputs, `applied` is what's actually
  // queried. Pressing "应用筛选" or Enter promotes draft → applied.
  const [draft, setDraft] = useState<Filters>(EMPTY_FILTERS);
  const [applied, setApplied] = useState<Filters>(EMPTY_FILTERS);
  const [filtersOpen, setFiltersOpen] = useState(true);
  const [detail, setDetail] = useState<AuditLogEntry | null>(null);

  const { data, isLoading, isFetchingNextPage, error, fetchNextPage, hasNextPage, refetch } =
    useInfiniteQuery({
      queryKey: ['audit-logs', applied],
      enabled: isAdmin,
      initialPageParam: '',
      queryFn: ({ pageParam }) =>
        api.get<ListAuditLogsResponse>(`/v1/audit-logs?${buildQuery(applied, pageParam as string)}`),
      getNextPageParam: (last) => (last.nextPageToken ? last.nextPageToken : undefined),
    });

  if (!isAdmin) {
    return (
      <Stack>
        <Title order={3}>审计日志</Title>
        <Alert color="yellow" icon={<IconLock size={16} />} title="无权访问">
          您没有权限查看审计日志。
        </Alert>
      </Stack>
    );
  }

  const apply = () => setApplied(draft);
  const reset = () => {
    setDraft(EMPTY_FILTERS);
    setApplied(EMPTY_FILTERS);
  };
  const handleEnter = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') apply();
  };

  const entries: AuditLogEntry[] = data?.pages.flatMap((p) => p.entries ?? []) ?? [];
  const totalSize = data?.pages[0]?.totalSize;

  return (
    <Stack>
      <Group justify="space-between">
        <Title order={3}>审计日志</Title>
        <Group gap="xs">
          <Tooltip label="刷新">
            <Button
              variant="default"
              leftSection={<IconRefresh size={16} />}
              onClick={() => refetch()}
              loading={isLoading}
            >
              刷新
            </Button>
          </Tooltip>
          <Button
            variant="default"
            leftSection={<IconAdjustmentsHorizontal size={16} />}
            onClick={() => setFiltersOpen((v) => !v)}
          >
            {filtersOpen ? '收起筛选' : '展开筛选'}
          </Button>
        </Group>
      </Group>

      <Collapse expanded={filtersOpen}>
        <Card withBorder padding="md" radius="md">
          <Stack gap="sm">
            <Group grow align="flex-end">
              <TextInput
                label="操作员邮箱"
                placeholder="alice@example.com"
                value={draft.operatorEmail}
                onChange={(e) => setDraft({ ...draft, operatorEmail: e.currentTarget.value })}
                onKeyDown={handleEnter}
              />
              <TextInput
                label="项目 slug"
                placeholder="prod-web"
                value={draft.projectSlug}
                onChange={(e) => setDraft({ ...draft, projectSlug: e.currentTarget.value })}
                onKeyDown={handleEnter}
              />
              <TextInput
                label="动作前缀"
                placeholder="project.create"
                value={draft.actionPrefix}
                onChange={(e) => setDraft({ ...draft, actionPrefix: e.currentTarget.value })}
                onKeyDown={handleEnter}
              />
            </Group>
            <Group grow align="flex-end">
              <Input.Wrapper label="起始时间">
                <Input
                  component="input"
                  type="datetime-local"
                  value={draft.since}
                  onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                    setDraft({ ...draft, since: e.currentTarget.value })
                  }
                  onKeyDown={handleEnter}
                />
              </Input.Wrapper>
              <Input.Wrapper label="结束时间">
                <Input
                  component="input"
                  type="datetime-local"
                  value={draft.until}
                  onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                    setDraft({ ...draft, until: e.currentTarget.value })
                  }
                  onKeyDown={handleEnter}
                />
              </Input.Wrapper>
            </Group>
            <Group justify="flex-end" gap="xs">
              <Button variant="default" onClick={reset}>
                重置
              </Button>
              <Button leftSection={<IconSearch size={16} />} onClick={apply}>
                应用筛选
              </Button>
            </Group>
          </Stack>
        </Card>
      </Collapse>

      {error && <Alert color="red">{(error as Error).message}</Alert>}

      <Card withBorder padding={0} radius="md">
        {isLoading ? (
          <Group justify="center" p="xl">
            <Loader />
          </Group>
        ) : entries.length === 0 ? (
          <EmptyState title="暂无日志" hint="调整筛选条件或等待新事件产生。" />
        ) : (
          <ResourceTable
            data={entries}
            rowKey={(e) => e.id}
            onRowClick={(e) => setDetail(e)}
            columns={[
              {
                key: 'time',
                header: '时间',
                width: 170,
                render: (e) => (
                  <Text size="sm" ff="monospace">
                    {formatDateTime(e.time)}
                  </Text>
                ),
              },
              {
                key: 'operator',
                header: '操作员',
                render: (e) => <Text size="sm">{e.operatorEmail || '-'}</Text>,
              },
              {
                key: 'sourceIp',
                header: '来源 IP',
                width: 130,
                render: (e) => (
                  <Text size="sm" ff="monospace" c="dimmed">
                    {e.sourceIp || '-'}
                  </Text>
                ),
              },
              {
                key: 'action',
                header: '动作',
                render: (e) => actionBadge(e.action),
              },
              {
                key: 'target',
                header: '目标',
                render: (e) => (
                  <Text size="sm" ff="monospace">
                    {e.target || '-'}
                  </Text>
                ),
              },
              {
                key: 'project',
                header: '项目',
                render: (e) =>
                  e.projectSlug ? (
                    <Text size="sm" ff="monospace">
                      {e.projectSlug}
                    </Text>
                  ) : (
                    <Text size="xs" c="dimmed">
                      —
                    </Text>
                  ),
              },
              {
                key: 'extra',
                header: '附加',
                render: (e) =>
                  e.extraJson ? (
                    <Text size="xs" c="dimmed" lineClamp={1} style={{ maxWidth: 240 }}>
                      {e.extraJson}
                    </Text>
                  ) : (
                    <Text size="xs" c="dimmed">
                      —
                    </Text>
                  ),
              },
            ]}
          />
        )}
      </Card>

      <Group justify="space-between" align="center">
        <Text size="xs" c="dimmed">
          已加载 {entries.length} 条
          {typeof totalSize === 'number' && totalSize > 0 ? ` / 共 ${totalSize} 条` : ''}
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

      <Modal
        opened={detail !== null}
        onClose={() => setDetail(null)}
        title="审计日志详情"
        size="lg"
        centered
      >
        {detail && (
          <Stack gap="xs">
            <Group gap="xs">
              <Text size="sm" fw={600}>
                时间
              </Text>
              <Text size="sm" ff="monospace">
                {formatDateTime(detail.time)}
              </Text>
            </Group>
            <Group gap="xs">
              <Text size="sm" fw={600}>
                操作员
              </Text>
              <Text size="sm">{detail.operatorEmail}</Text>
            </Group>
            <Group gap="xs">
              <Text size="sm" fw={600}>
                来源 IP
              </Text>
              <Text size="sm" ff="monospace">
                {detail.sourceIp || '-'}
              </Text>
            </Group>
            <Group gap="xs">
              <Text size="sm" fw={600}>
                动作
              </Text>
              {actionBadge(detail.action)}
            </Group>
            <Group gap="xs">
              <Text size="sm" fw={600}>
                目标
              </Text>
              <Text size="sm" ff="monospace">
                {detail.target || '-'}
              </Text>
            </Group>
            <Group gap="xs">
              <Text size="sm" fw={600}>
                项目
              </Text>
              <Text size="sm" ff="monospace">
                {detail.projectSlug || '-'}
              </Text>
            </Group>
            <Text size="sm" fw={600} mt="xs">
              附加信息 (extra_json)
            </Text>
            <Code block>{formatExtra(detail.extraJson) || '(空)'}</Code>
          </Stack>
        )}
      </Modal>
    </Stack>
  );
}
