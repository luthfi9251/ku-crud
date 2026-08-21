import type { ColumnDef } from "./types";

export const ENUM_COLOR_CLASSES: Record<string, string> = {
  gray: "bg-slate-500/10 text-slate-600 dark:text-slate-300 border-slate-500/30",
  blue: "bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/30",
  green: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/30",
  amber: "bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/30",
  red: "bg-red-500/10 text-red-600 dark:text-red-400 border-red-500/30",
  purple: "bg-purple-500/10 text-purple-600 dark:text-purple-400 border-purple-500/30",
  cyan: "bg-cyan-500/10 text-cyan-600 dark:text-cyan-400 border-cyan-500/30",
  orange: "bg-orange-500/10 text-orange-600 dark:text-orange-400 border-orange-500/30",
};

// formatCell renders one raw cell value per the column's formatting config.
// Presentation only — never written back and never used for CSV export.
export function formatCell(c: ColumnDef, raw: unknown, lang: string): string {
  if (raw === null || raw === undefined || raw === "") return "";
  if (c.fieldType === "number" && typeof raw === "number") {
    const nf = c.formatting?.number;
    if (!nf) return String(raw);
    const opts: Intl.NumberFormatOptions = { maximumFractionDigits: 6 };
    if (nf.decimals != null) {
      opts.minimumFractionDigits = nf.decimals;
      opts.maximumFractionDigits = nf.decimals;
    }
    if (nf.thousands) opts.useGrouping = true;
    let out = new Intl.NumberFormat(lang === "id" ? "id-ID" : "en-US", opts).format(raw);
    if (nf.prefix) out = nf.prefix + out;
    return out;
  }
  if (c.fieldType === "enum" && c.formatting?.enumColors) {
    return String(raw); // color is a rendering decision — see enumColorClass()
  }
  if (c.fieldType === "datetime" && typeof raw === "string") {
    const d = new Date(raw);
    if (!isNaN(d.getTime())) {
      return new Intl.DateTimeFormat(lang === "id" ? "id-ID" : "en-US", {
        dateStyle: "medium", timeStyle: "short",
      }).format(d);
    }
  }
  return String(raw);
}

export function enumColorClass(c: ColumnDef, value: string): string {
  const name = c.formatting?.enumColors?.[value];
  return ENUM_COLOR_CLASSES[name ?? "gray"];
}
