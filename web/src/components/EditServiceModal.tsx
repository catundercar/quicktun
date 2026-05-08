import { useEffect } from 'react';
import { Modal, Stack, TextInput, NumberInput, Button, Group } from '@mantine/core';
import { useForm } from '@mantine/form';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { notifications } from '@mantine/notifications';
import { api, ApiError } from '../api/client';
import type { Service } from '../api/types';

type Props = {
  service: Service | null;
  opened: boolean;
  onClose: () => void;
  /** Used for query-cache invalidation. */
  projectSlug: string;
  siteSlug: string;
};

type FormValues = {
  displayName: string;
  targetAddr: string;
  targetPort: number;
};

export function EditServiceModal({
  service,
  opened,
  onClose,
  projectSlug,
  siteSlug,
}: Props) {
  const qc = useQueryClient();

  const form = useForm<FormValues>({
    initialValues: { displayName: '', targetAddr: '', targetPort: 0 },
    validate: {
      displayName: (v) => (v.trim() ? null : '显示名称必填'),
      targetAddr: (v) => (v.trim() ? null : '目标地址必填'),
      targetPort: (v) => (v >= 1 && v <= 65535 ? null : '端口需在 1-65535'),
    },
  });

  useEffect(() => {
    if (service && opened) {
      const initial: FormValues = {
        displayName: service.displayName,
        targetAddr: service.targetAddr,
        targetPort: service.targetPort,
      };
      form.setValues(initial);
      form.resetDirty(initial);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [service, opened]);

  const mu = useMutation({
    mutationFn: async (vals: FormValues) => {
      if (!service) return;
      const paths: string[] = [];
      if (form.isDirty('displayName')) paths.push('display_name');
      if (form.isDirty('targetAddr')) paths.push('target_addr');
      if (form.isDirty('targetPort')) paths.push('target_port');
      if (paths.length === 0) return; // no-op

      await api.patch(`/v1/${service.name}`, {
        service: {
          name: service.name,
          displayName: vals.displayName,
          targetAddr: vals.targetAddr.trim(),
          targetPort: vals.targetPort,
        },
        updateMask: paths.join(','),
      });
    },
    onSuccess: () => {
      notifications.show({ color: 'green', title: '更新成功', message: '服务已更新' });
      qc.invalidateQueries({ queryKey: ['services', projectSlug, siteSlug] });
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
        if (!mu.isPending) onClose();
      }}
      title={`编辑服务 ${service?.serviceId ?? ''}`}
      centered
    >
      <form onSubmit={form.onSubmit((vals) => mu.mutate(vals))}>
        <Stack>
          <TextInput
            required
            label="显示名称"
            {...form.getInputProps('displayName')}
          />
          <Group grow>
            <TextInput
              required
              label="目标地址"
              description="站点可达的 IP 或主机名。"
              placeholder="127.0.0.1"
              {...form.getInputProps('targetAddr')}
            />
            <NumberInput
              required
              label="目标端口"
              min={1}
              max={65535}
              {...form.getInputProps('targetPort')}
            />
          </Group>
          <Group justify="flex-end" mt="sm">
            <Button variant="default" onClick={onClose} disabled={mu.isPending}>
              取消
            </Button>
            <Button type="submit" loading={mu.isPending}>
              保存
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
