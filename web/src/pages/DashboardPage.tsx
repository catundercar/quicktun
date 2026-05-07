import { useQuery } from '@tanstack/react-query';
import {
  Card,
  Grid,
  Text,
  Title,
  Group,
  Badge,
  Stack,
  Loader,
  Alert,
} from '@mantine/core';
import { api } from '../api/client';
import type { SystemStatus } from '../api/types';

function StatCard({
  label,
  value,
  hint,
}: {
  label: string;
  value: number | string;
  hint?: string;
}) {
  return (
    <Card withBorder padding="lg" radius="md">
      <Text c="dimmed" size="xs" tt="uppercase" fw={700}>
        {label}
      </Text>
      <Text size="xl" fw={700} mt={4}>
        {value}
      </Text>
      {hint && (
        <Text size="xs" c="dimmed" mt={4}>
          {hint}
        </Text>
      )}
    </Card>
  );
}

export function DashboardPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['admin-status'],
    queryFn: () => api.get<SystemStatus>('/v1/admin:status'),
    refetchInterval: 15_000,
  });

  if (isLoading) return <Loader />;
  if (error) return <Alert color="red">{(error as Error).message}</Alert>;
  if (!data) return null;

  return (
    <Stack>
      <Title order={3}>仪表盘</Title>
      <Grid>
        <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
          <StatCard label="操作员" value={data.operatorCount} />
        </Grid.Col>
        <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
          <StatCard
            label="项目"
            value={data.projectCountActive}
            hint={`+ ${data.projectCountDisabled} 已停用`}
          />
        </Grid.Col>
        <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
          <StatCard
            label="站点"
            value={data.siteCountOnline}
            hint={`${data.siteCountOffline} 离线 / ${data.siteCountPending} 待激活`}
          />
        </Grid.Col>
        <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
          <StatCard label="服务" value={data.serviceCount} />
        </Grid.Col>
        <Grid.Col span={{ base: 12, sm: 6, md: 3 }}>
          <StatCard label="rathole 监管进程" value={data.supervisorRunningCount} />
        </Grid.Col>
      </Grid>

      {data.staleSites && data.staleSites.length > 0 && (
        <Card withBorder>
          <Group mb="xs">
            <Title order={5}>近期掉线的站点</Title>
            <Badge color="orange">{data.staleSites.length}</Badge>
          </Group>
          <Stack gap={4}>
            {data.staleSites.map((s) => (
              <Group key={s.name} justify="space-between">
                <Text size="sm">{s.name}</Text>
                <Text size="xs" c="dimmed">
                  {s.lastSeenAt} {s.hostname}
                </Text>
              </Group>
            ))}
          </Stack>
        </Card>
      )}
    </Stack>
  );
}
