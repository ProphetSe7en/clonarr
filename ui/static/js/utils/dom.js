// Small DOM/input helpers shared by Alpine expressions and feature modules.

// Generate UUID that works over plain HTTP (crypto.randomUUID needs secure context).
export function genUUID(noDashes) {
  if (crypto.randomUUID) {
    const id = crypto.randomUUID();
    return noDashes ? id.replace(/-/g, '') : id;
  }
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');
  if (noDashes) return hex;
  return hex.slice(0, 8) + '-' + hex.slice(8, 12) + '-' + hex.slice(12, 16) + '-' + hex.slice(16, 20) + '-' + hex.slice(20);
}

// Parse comma/space-separated Newznab category IDs into a deduped sorted int array.
export function parseCategoryList(str) {
  if (!str) return [];
  const seen = new Set();
  for (const part of String(str).split(/[,\s]+/)) {
    const n = parseInt(part.trim(), 10);
    if (!isNaN(n) && n > 0) seen.add(n);
  }
  return [...seen].sort((a, b) => a - b);
}
