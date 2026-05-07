import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Container,
  Paper,
  Title,
  TextInput,
  PasswordInput,
  Button,
  Stack,
  Center,
} from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import { api } from '../api/client';
import { useAuthStore } from './store';
import type { LoginResponse } from '../api/types';

export function LoginPage() {
  const nav = useNavigate();
  const setSession = useAuthStore((s) => s.setSession);
  const [loading, setLoading] = useState(false);

  const form = useForm({
    initialValues: { email: '', password: '' },
    validate: {
      email: (v) => (/^\S+@\S+$/.test(v) ? null : '邮箱格式不正确'),
      password: (v) => (v.length >= 1 ? null : '密码不能为空'),
    },
  });

  const onSubmit = form.onSubmit(async (vals) => {
    setLoading(true);
    try {
      const resp = await api.post<LoginResponse>('/v1/auth:login', vals);
      setSession({
        token: resp.accessToken,
        email: resp.operator.email,
        isAdmin: resp.operator.isAdmin,
      });
      nav('/dashboard');
    } catch (e) {
      const msg = e instanceof Error ? e.message : '请稍后重试';
      notifications.show({ color: 'red', title: '登录失败', message: msg });
    } finally {
      setLoading(false);
    }
  });

  return (
    <Center mih="100vh">
      <Container size={420} w={420}>
        <Title order={2} ta="center" mb="lg">
          quicktun 管理
        </Title>
        <Paper p="xl" withBorder radius="md">
          <form onSubmit={onSubmit}>
            <Stack>
              <TextInput
                required
                label="邮箱"
                placeholder="admin@example.com"
                {...form.getInputProps('email')}
              />
              <PasswordInput required label="密码" {...form.getInputProps('password')} />
              <Button type="submit" loading={loading}>
                登录
              </Button>
            </Stack>
          </form>
        </Paper>
      </Container>
    </Center>
  );
}
