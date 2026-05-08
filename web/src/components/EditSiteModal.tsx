import { useEffect } from 'react';
import { Modal, Stack, TextInput, Select, Button, Group } from '@mantine/core';
import { useForm } from '@mantine/form';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { notifications } from '@mantine/notifications';
import { api, ApiError } from '../api/client';
import type { Site } from '../api/types';

type Props = {
  site: Site | null;
  opened: boolean;
  onClose: () => void;
  /** projectSlug used for invalidating the sites list query key */
  projectSlug: string;
};

type FormValues = {
  displayName: string;
  mode: string;
};

export function EditSiteModal({ site, opened, onClose, projectSlug }: Props) {
  const qc = useQueryClient();

  const form = useForm<FormValues>({
    initialValues: {
      displayName: '',
      mode: 'SITE_MODE_ENDPOINT',
    },
    validate: {
      displayName: (v) => (v.trim() ? null : '显示名称必填'),
    },
  });

  useEffect(() => {
    if (site && opened) {
      const initial: FormValues = {
        displayName: site.displayName,
        mode: site.mode || 'SITE_MODE_ENDPOINT',
      };
      form.setValues(initial);
      form.resetDirty(initial);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [site, opened]);

  const mu = useMutation({
    mutationFn: async (vals: FormValues) => {
      if (!site) return;
      const paths: string[] = [];
      if (form.isDirty('displayName')) paths.push('display_name');
      if (form.isDirty('mode')) paths.push('mode');
      if (paths.length === 0) return; // no-op

      await api.patch(`/v1/${site.name}`, {
        site: {
          name: site.name,
          displayName: vals.displayName,
          mode: vals.mode,
        },
        updateMask: paths.join(','),
      });
    },
    onSuccess: () => {
      notifications.show({ color: 'green', title: '更新成功', message: '站点已更新' });
      qc.invalidateQueries({ queryKey: ['sites', projectSlug] });
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
      title={`编辑站点 ${site?.siteId ?? ''}`}
      centered
    >
      <form onSubmit={form.onSubmit((vals) => mu.mutate(vals))}>
        <Stack>
          <TextInput
            required
            label="显示名称"
            {...form.getInputProps('displayName')}
          />
          <Select
            label="模式"
            description="ENDPOINT：单机转发；SUBNET：将整个子网通过该站点接入。"
            data={[
              { value: 'SITE_MODE_ENDPOINT', label: 'ENDPOINT — 单机转发' },
              { value: 'SITE_MODE_SUBNET', label: 'SUBNET — 子网' },
            ]}
            allowDeselect={false}
            {...form.getInputProps('mode')}
          />
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
