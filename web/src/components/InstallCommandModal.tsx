import {
  Modal,
  Tabs,
  Code,
  Alert,
  Button,
  CopyButton,
  Stack,
  Loader,
  Text,
} from '@mantine/core';
import {
  IconAlertTriangle,
  IconCheck,
  IconCopy,
} from '@tabler/icons-react';
import { useQuery } from '@tanstack/react-query';
import { api } from '../api/client';
import type { InstallCommandResponse } from '../api/types';

type Props = {
  /** Resource name like `projects/p/sites/s`, or null to keep modal closed. */
  siteName: string | null;
  opened: boolean;
  onClose: () => void;
};

/**
 * Best-effort: extract the QT_ENDPOINT value from the server-issued linux
 * install command. The backend builds it as
 *   curl ... | QT_TOKEN=... QT_ENDPOINT=<host:port> bash
 * If parsing fails, fall back to a placeholder.
 */
function parseEndpoint(linuxCmd: string): string {
  const m = linuxCmd.match(/QT_ENDPOINT=([^\s]+)/);
  if (m && m[1]) return m[1];
  return 'CONTROL_DOMAIN:443';
}

export function InstallCommandModal({ siteName, opened, onClose }: Props) {
  // Always refetch on open: a fresh token is issued each time, the previous one
  // becomes invalid. We disable cache and only run while the modal is open.
  const { data, isLoading, error } = useQuery({
    queryKey: ['install-command', siteName, opened],
    queryFn: () =>
      api.get<InstallCommandResponse>(
        `/v1/${siteName}:installCommand?os=linux`,
      ),
    enabled: opened && !!siteName,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    gcTime: 0,
    staleTime: 0,
    retry: false,
  });

  const token = data?.token ?? '';
  const linuxCmd = data?.command ?? '';
  const endpoint = linuxCmd ? parseEndpoint(linuxCmd) : 'CONTROL_DOMAIN:443';

  // macOS uses the same install-agent.sh.
  const macosCmd = linuxCmd;

  const winNssm = `# 1) Download install-agent.ps1, nssm.exe and quicktun-agent.exe to the same folder.
# 2) In an elevated PowerShell, run:
$env:QT_TOKEN="${token}"
$env:QT_ENDPOINT="${endpoint}"
iwr -useb https://${endpoint}/install/agent.ps1 | iex`;

  const winMsi = `# 1) Install the MSI:
msiexec /i quicktun-agent.msi /qn

# 2) Edit C:\\ProgramData\\quicktun\\agent.yaml and fill in:
#    token: ${token}
#    control_endpoint: ${endpoint}

# 3) Start the service:
Start-Service quicktun-agent`;

  return (
    <Modal
      opened={opened}
      onClose={onClose}
      size="lg"
      centered
      title="站点安装命令"
      closeOnClickOutside={false}
    >
      {isLoading ? (
        <Loader />
      ) : error ? (
        <Alert color="red">{(error as Error).message}</Alert>
      ) : (
        <Stack>
          <Alert
            color="orange"
            title="此 token 仅显示一次"
            icon={<IconAlertTriangle size={16} />}
          >
            <Text size="sm">
              关闭此弹窗后将无法再次查看；如需重新查看会生成新 token，旧 token
              立即失效。请立即复制保存对应平台的命令。
            </Text>
          </Alert>
          <Tabs defaultValue="linux" keepMounted={false}>
            <Tabs.List>
              <Tabs.Tab value="linux">Linux</Tabs.Tab>
              <Tabs.Tab value="macos">macOS</Tabs.Tab>
              <Tabs.Tab value="windows-nssm">Windows (NSSM)</Tabs.Tab>
              <Tabs.Tab value="windows-msi">Windows (MSI)</Tabs.Tab>
            </Tabs.List>
            <Tabs.Panel value="linux" pt="sm">
              <CodeWithCopy code={linuxCmd} />
            </Tabs.Panel>
            <Tabs.Panel value="macos" pt="sm">
              <CodeWithCopy code={macosCmd} />
            </Tabs.Panel>
            <Tabs.Panel value="windows-nssm" pt="sm">
              <CodeWithCopy code={winNssm} />
            </Tabs.Panel>
            <Tabs.Panel value="windows-msi" pt="sm">
              <CodeWithCopy code={winMsi} />
            </Tabs.Panel>
          </Tabs>
        </Stack>
      )}
    </Modal>
  );
}

function CodeWithCopy({ code }: { code: string }) {
  return (
    <div style={{ position: 'relative' }}>
      <Code
        block
        style={{
          paddingRight: 90,
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-all',
          userSelect: 'all',
        }}
      >
        {code}
      </Code>
      <CopyButton value={code} timeout={2000}>
        {({ copied, copy }) => (
          <Button
            size="xs"
            color={copied ? 'teal' : 'blue'}
            onClick={copy}
            leftSection={
              copied ? <IconCheck size={14} /> : <IconCopy size={14} />
            }
            style={{ position: 'absolute', top: 8, right: 8 }}
          >
            {copied ? '已复制' : '复制'}
          </Button>
        )}
      </CopyButton>
    </div>
  );
}
