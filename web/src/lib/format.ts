export function shortId(id: string, n = 8): string {
  return id ? id.slice(0, n) : "";
}

export function basename(p: string): string {
  const i = Math.max(p.lastIndexOf("/"), p.lastIndexOf("\\"));
  return i >= 0 ? p.slice(i + 1) : p;
}

export function lastSegments(p: string, n = 2): string {
  const parts = p.replace(/\\/g, "/").split("/");
  return parts.slice(-n).join("/");
}

export function tryPrettyJson(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}

export function pluralize(n: number, one: string, many = one + "s"): string {
  return `${n.toLocaleString()} ${n === 1 ? one : many}`;
}
