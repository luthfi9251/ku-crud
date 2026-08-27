// looksLikeJSON reports whether a string should be treated as a JSON
// document: it must start with { or [ and parse cleanly. The guarded
// prefix keeps "123", "true" and prose out (they are valid bare JSON
// values but never useful to pretty-print).
export function looksLikeJSON(s: string): boolean {
  const t = s.trimStart();
  if (!t.startsWith("{") && !t.startsWith("[")) return false;
  try {
    JSON.parse(t);
    return true;
  } catch {
    return false;
  }
}
