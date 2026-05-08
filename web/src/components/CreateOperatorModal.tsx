import { Modal, Stack, TextInput, PasswordInput, Checkbox, Group, Button } from '@mantine/core';
import { useForm } from '@mantine/form';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { notifications } from '@mantine/notifications';
import { api, ApiError } from '../api/client';
import type { Operator } from '../api/types';

type Props = {
  opened: boolean;
  onClose: () => void;
};

type FormValues = {
  email: string;
  password: string;
  confirmPassword: string;
  isAdmin: boolean;
};

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function CreateOperatorModal({ opened, onClose }: Props) {
  const qc = useQueryClient();

  const form = useForm<FormValues>({
    initialValues: {
      email: '',
      password: '',
      confirmPassword: '',
      isAdmin: false,
    },
    validate: {
      email: (v) => (EMAIL_RE.test(v.trim()) ? null : '请输入合法邮箱'),
      password: (v) => (v.length >= 8 ? null : '密码至少 8 位'),
      confirmPassword: (v, vals) => (v === vals.password ? null : '两次输入的密码不一致'),
    },
  });

  const mu = useMutation({
    mutationFn: async (vals: FormValues) =>
      api.post<Operator>('/v1/operators', {
        operator: {
          email: vals.email.trim(),
          isAdmin: vals.isAdmin,
        },
        password: vals.password,
      }),
    onSuccess: () => {
      notifications.show({ color: 'green', title: '创建成功', message: '操作员已创建' });
      qc.invalidateQueries({ queryKey: ['operators'] });
      form.reset();
      onClose();
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError || e instanceof Error ? e.message : '请稍后重试';
      notifications.show({ color: 'red', title: '创建失败', message: msg });
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
      title="创建操作员"
      centered
    >
      <form onSubmit={form.onSubmit((vals) => mu.mutate(vals))}>
        <Stack>
          <TextInput
            required
            type="email"
            label="邮箱"
            placeholder="alice@example.com"
            {...form.getInputProps('email')}
          />
          <PasswordInput
            required
            label="初始密码"
            description="密码至少 8 位，将由服务端 bcrypt 哈希后保存。"
            {...form.getInputProps('password')}
          />
          <PasswordInput
            required
            label="重复密码"
            {...form.getInputProps('confirmPassword')}
          />
          <Checkbox
            label="授予管理员权限"
            description="管理员可管理操作员、查看审计日志，并拥有所有项目的访问权限。"
            {...form.getInputProps('isAdmin', { type: 'checkbox' })}
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
              创建
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
