import { Table, Text, Stack, Center } from '@mantine/core';
import type { ReactNode } from 'react';

type Column<T> = {
  key: string;
  header: ReactNode;
  render: (row: T) => ReactNode;
  width?: number | string;
};

type Props<T> = {
  columns: Column<T>[];
  data: T[];
  rowKey: (row: T) => string;
  empty?: ReactNode;
  onRowClick?: (row: T) => void;
};

/**
 * Thin wrapper around Mantine `Table` to keep header style consistent
 * across the Projects/Sites/Services/Operators pages.
 */
export function ResourceTable<T>({
  columns,
  data,
  rowKey,
  empty,
  onRowClick,
}: Props<T>) {
  if (data.length === 0) {
    return (
      <Center p="xl">
        {typeof empty === 'string' ? (
          <Text c="dimmed">{empty}</Text>
        ) : empty ? (
          empty
        ) : (
          <Text c="dimmed">暂无数据</Text>
        )}
      </Center>
    );
  }
  return (
    <Table.ScrollContainer minWidth={600}>
      <Table verticalSpacing="sm" highlightOnHover striped="even">
        <Table.Thead>
          <Table.Tr>
            {columns.map((c) => (
              <Table.Th key={c.key} style={c.width ? { width: c.width } : undefined}>
                {c.header}
              </Table.Th>
            ))}
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {data.map((row) => (
            <Table.Tr
              key={rowKey(row)}
              onClick={onRowClick ? () => onRowClick(row) : undefined}
              style={onRowClick ? { cursor: 'pointer' } : undefined}
            >
              {columns.map((c) => (
                <Table.Td key={c.key}>{c.render(row)}</Table.Td>
              ))}
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  );
}

/** Simple, consistent empty-state block. */
export function EmptyState({ title, hint }: { title: string; hint?: ReactNode }) {
  return (
    <Center p="xl">
      <Stack gap={4} align="center">
        <Text fw={600}>{title}</Text>
        {hint && (
          <Text size="sm" c="dimmed">
            {hint}
          </Text>
        )}
      </Stack>
    </Center>
  );
}
