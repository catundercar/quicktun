import { Modal, Group, Button, Text, Stack } from '@mantine/core';
import type { ReactNode } from 'react';

type Props = {
  opened: boolean;
  onClose: () => void;
  onConfirm: () => void;
  title?: string;
  description?: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  loading?: boolean;
  /** Mantine Button color for the confirm button. Default 'red' (destructive). */
  confirmColor?: string;
};

/** Reusable confirmation modal — always shown before destructive actions. */
export function ConfirmModal({
  opened,
  onClose,
  onConfirm,
  title = '请确认',
  description,
  confirmLabel = '确认',
  cancelLabel = '取消',
  loading = false,
  confirmColor = 'red',
}: Props) {
  return (
    <Modal opened={opened} onClose={onClose} title={title} centered>
      <Stack>
        {typeof description === 'string' ? (
          <Text size="sm">{description}</Text>
        ) : (
          description
        )}
        <Group justify="flex-end" mt="sm">
          <Button variant="default" onClick={onClose} disabled={loading}>
            {cancelLabel}
          </Button>
          <Button color={confirmColor} onClick={onConfirm} loading={loading}>
            {confirmLabel}
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
