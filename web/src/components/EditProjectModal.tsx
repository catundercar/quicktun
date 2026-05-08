import { useEffect } from 'react';
import { Modal, Stack, TextInput, Select, Button, Group } from '@mantine/core';
import { useForm } from '@mantine/form';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { notifications } from '@mantine/notifications';
import { api, ApiError } from '../api/client';
import type { Project } from '../api/types';

type Props = {
  project: Project | null;
  opened: boolean;
  onClose: () => void;
};

type FormValues = {
  displayName: string;
  relayPortRange: string;
  status: string;
};

export function EditProjectModal({ project, opened, onClose }: Props) {
  const qc = useQueryClient();

  const form = useForm<FormValues>({
    initialValues: {
      displayName: '',
      relayPortRange: '',
      status: 'PROJECT_STATUS_ACTIVE',
    },
    validate: {
      displayName: (v) => (v.trim() ? null : '显示名称必填'),
      relayPortRange: (v) =>
        /^\d+-\d+$/.test(v.trim()) ? null : '格式应为 起始端口-结束端口，如 20000-20099',
    },
  });

  // Initialize/reset form whenever the modal opens or the project changes.
  useEffect(() => {
    if (project && opened) {
      const initial: FormValues = {
        displayName: project.displayName,
        relayPortRange: project.relayPortRange,
        status: project.status || 'PROJECT_STATUS_ACTIVE',
      };
      form.setValues(initial);
      form.resetDirty(initial);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project, opened]);

  const mu = useMutation({
    mutationFn: async (vals: FormValues) => {
      if (!project) return;
      const paths: string[] = [];
      if (form.isDirty('displayName')) paths.push('display_name');
      if (form.isDirty('relayPortRange')) paths.push('relay_port_range');
      if (form.isDirty('status')) paths.push('status');
      if (paths.length === 0) return; // no-op

      await api.patch(`/v1/${project.name}`, {
        project: {
          name: project.name,
          displayName: vals.displayName,
          relayPortRange: vals.relayPortRange.trim(),
          status: vals.status,
        },
        updateMask: paths.join(','),
      });
    },
    onSuccess: () => {
      notifications.show({ color: 'green', title: '更新成功', message: '项目已更新' });
      qc.invalidateQueries({ queryKey: ['projects'] });
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
      title={`编辑项目 ${project?.projectId ?? ''}`}
      centered
    >
      <form onSubmit={form.onSubmit((vals) => mu.mutate(vals))}>
        <Stack>
          <TextInput
            required
            label="显示名称"
            {...form.getInputProps('displayName')}
          />
          <TextInput
            required
            label="中继端口段"
            description="rathole 中继侧将在该范围内分配端口；不同项目之间不可重叠。"
            placeholder="20000-20099"
            {...form.getInputProps('relayPortRange')}
          />
          <Select
            label="状态"
            data={[
              { value: 'PROJECT_STATUS_ACTIVE', label: '运行中' },
              { value: 'PROJECT_STATUS_DISABLED', label: '已停用' },
            ]}
            allowDeselect={false}
            {...form.getInputProps('status')}
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
