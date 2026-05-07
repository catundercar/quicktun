// Lightweight relative-time formatter for ISO 8601 timestamps.
// No external dependency — keep bundle small.

export function formatTime(iso?: string): string {
  if (!iso) return '-';
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  const now = Date.now();
  const diff = Math.round((now - t) / 1000); // seconds
  if (diff < 0) {
    // Future timestamp — fall back to absolute.
    return formatAbsolute(t);
  }
  if (diff < 5) return '刚刚';
  if (diff < 60) return `${diff} 秒前`;
  const m = Math.round(diff / 60);
  if (m < 60) return `${m} 分钟前`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h} 小时前`;
  const d = Math.round(h / 24);
  if (d < 7) return `${d} 天前`;
  return formatAbsolute(t);
}

function formatAbsolute(t: number): string {
  const d = new Date(t);
  const y = d.getFullYear();
  const mo = String(d.getMonth() + 1).padStart(2, '0');
  const da = String(d.getDate()).padStart(2, '0');
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  return `${y}-${mo}-${da} ${hh}:${mm}`;
}

// Renders absolute date+time always (no relative bias).
export function formatDateTime(iso?: string): string {
  if (!iso) return '-';
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  return formatAbsolute(t);
}
