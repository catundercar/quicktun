// ChangePasswordForm — reusable password-change form.
//
// Two flavors:
//   * Admin reset for another operator: pass `operator` and omit
//     `requireOldPassword`. The form posts a new password without verifying
//     anything (the backend trusts admin callers).
//   * Self-service change on /profile: pass `requireOldPassword` and the form
//     verifies the current password by calling `POST /v1/auth:login` first,
//     then PATCHes the operator. We keep this entirely on the client because
//     the backend Operator.UpdateOperator does not currently demand the old
//     password — the verification step is purely a UX/safety layer.
import { useState } from 'react';
import { Button, Group, PasswordInput, Stack, Text } from '@mantine/core';
import { useForm } from '@mantine/form';
import { useMutation } from '@tanstack/react-query';
import { notifications } from '@mantine/notifications';
import { api, ApiError } from '../api/client';
import type { LoginResponse, Operator } from '../api/types';
import { useAuthStore } from '../auth/store';

type Props = {
  /** Operator whose password is being changed. */
  operator: Operator | null;
  /** When true, the form requires the operator's current password to match
   *  before the PATCH is issued. Use this on the self-service profile page. */
  requireOldPassword?: boolean;
  /** Optional callback fired after a successful change. */
  onSuccess?: () => void;
  /** Optional cancel handler — when omitted, the cancel button is hidden. */
  onCancel?: () => void;
  submitLabel?: string;
};

type FormValues = {
  oldPassword: string;
  password: string;
  confirm: string;
};

export function ChangePasswordForm({
  operator,
  requireOldPassword = false,
  onSuccess,
  onCancel,
  submitLabel = '更新',
}: Props) {
  const operatorEmail = useAuthStore((s) => s.operatorEmail);
  const [verifying, setVerifying] = useState(false);

  const form = useForm<FormValues>({
    initialValues: { oldPassword: '', password: '', confirm: '' },
    validate: {
      oldPassword: (v) =>
        requireOldPassword && v.length === 0 ? '请输入当前密码' : null,
      password: (v) => (v.length >= 8 ? null : '密码至少 8 位'),
      confirm: (v, vals) => (v === vals.password ? null : '两次输入的密码不一致'),
    },
  });

  const mu = useMutation({
    mutationFn: async (vals: FormValues) => {
      if (!operator) return;
      if (requireOldPassword) {
        // Verify the old password by attempting a login. We do not store the
        // returned token because the user's existing session is still valid.
        if (!operatorEmail) throw new Error('当前未登录');
        setVerifying(true);
        try {
          await api.post<LoginResponse>('/v1/auth:login', {
            email: operatorEmail,
            password: vals.oldPassword,
          });
        } catch (e) {
          // Surface a friendly error rather than leaking the raw 401 message.
          if (e instanceof ApiError && e.status === 401) {
            throw new Error('当前密码不正确');
          }
          throw e;
        } finally {
          setVerifying(false);
        }
      }
      await api.patch(`/v1/${operator.name}`, {
        operator: { name: operator.name },
        updateMask: 'password',
        password: vals.password,
      });
    },
    onSuccess: () => {
      notifications.show({
        color: 'green',
        title: '密码已更新',
        message: '请下次登录使用新密码',
      });
      form.reset();
      onSuccess?.();
    },
    onError: (e: unknown) => {
      const msg = e instanceof ApiError || e instanceof Error ? e.message : '请稍后重试';
      notifications.show({ color: 'red', title: '更新失败', message: msg });
    },
  });

  const busy = mu.isPending || verifying;

  return (
    <form onSubmit={form.onSubmit((vals) => mu.mutate(vals))}>
      <Stack>
        {requireOldPassword && (
          <PasswordInput
            required
            label="当前密码"
            description="为确认是本人操作，请先输入当前密码。"
            {...form.getInputProps('oldPassword')}
          />
        )}
        <PasswordInput
          required
          label="新密码"
          description="密码至少 8 位。"
          {...form.getInputProps('password')}
        />
        <PasswordInput
          required
          label="重复新密码"
          {...form.getInputProps('confirm')}
        />
        {!operator && (
          <Text size="xs" c="dimmed">
            正在加载当前操作员信息……
          </Text>
        )}
        <Group justify="flex-end" mt="sm">
          {onCancel && (
            <Button variant="default" onClick={onCancel} disabled={busy}>
              取消
            </Button>
          )}
          <Button type="submit" loading={busy} disabled={!operator}>
            {submitLabel}
          </Button>
        </Group>
      </Stack>
    </form>
  );
}
