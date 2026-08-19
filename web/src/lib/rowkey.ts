// Row keys travel in URLs as base64url(JSON array of key value strings),
// mirroring the Go implementation in internal/api/rowkey.go.
// ["3"] → "WyIzIl0", ["1","a"] → "WyIxIiwiYSJd"
export function encodeRowKey(vals: (string | number | boolean | null)[]): string {
  const strings = vals.map((v) => (v === null ? null : String(v)));
  const json = JSON.stringify(strings);
  const bytes = new TextEncoder().encode(json);
  let bin = "";
  bytes.forEach((b) => (bin += String.fromCharCode(b)));
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

// Renders an audit rowPk (JSON array like ["1","5"], or a legacy plain value)
// for display.
export function displayRowPk(raw: string): string {
  try {
    const arr = JSON.parse(raw);
    if (Array.isArray(arr)) return arr.map((v) => (v === null ? "—" : String(v))).join(" / ");
  } catch {
    // legacy single-value entries
  }
  return raw || "—";
}
