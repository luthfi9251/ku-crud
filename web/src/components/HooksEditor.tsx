import type { HookEvent, HooksConfig, HookAssignment } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useT } from "@/lib/i18n";

const EVENTS: HookEvent[] = [
  "beforeCreate", "afterCreate",
  "beforeUpdate", "afterUpdate",
  "beforeDelete", "afterDelete",
];

interface Props {
  value: HooksConfig;
  names: string[];
  onChange: (v: HooksConfig) => void;
}

export default function HooksEditor({ value, names, onChange }: Props) {
  const t = useT();
  if (names.length === 0) return null;

  const setEvent = (ev: HookEvent, list: HookAssignment[]) => {
    const next = { ...value };
    if (list.length === 0) delete next[ev];
    else next[ev] = list.map((a, i) => ({ ...a, order: i + 1 }));
    onChange(next);
  };

  return (
    <div className="space-y-4">
      {EVENTS.map((ev) => {
        const list = value[ev] ?? [];
        return (
          <div key={ev} className="rounded border p-3 space-y-2">
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium">{t(`tf.hooks.event.${ev}`)}</span>
              <Button type="button" variant="link" className="h-auto p-0 text-xs"
                onClick={() => setEvent(ev, [...list, { hook: names[0], order: list.length + 1 }])}>
                {t("tf.hooks.add")}
              </Button>
            </div>
            {list.map((a, i) => (
              <div key={i} className="flex items-start gap-2">
                <Select
                  value={a.hook}
                  onValueChange={(v) => setEvent(ev, list.map((x, j) => j === i ? { ...x, hook: v } : x))}>
                  <SelectTrigger className="h-8 w-48 text-xs"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {names.map((n) => <SelectItem key={n} value={n} className="text-xs font-mono">{n}</SelectItem>)}
                  </SelectContent>
                </Select>
                <Input className="h-8 flex-1 text-xs font-mono"
                  placeholder={t("tf.hooks.config")}
                  defaultValue={a.config ? JSON.stringify(a.config) : ""}
                  onBlur={(e) => {
                    const raw = e.target.value.trim();
                    if (!raw) { setEvent(ev, list.map((x, j) => j === i ? { ...x, config: null } : x)); return; }
                    try {
                      const parsed = JSON.parse(raw);
                      setEvent(ev, list.map((x, j) => j === i ? { ...x, config: parsed } : x));
                      e.target.setCustomValidity("");
                    } catch {
                      e.target.setCustomValidity(t("tf.hooks.configInvalid"));
                      e.target.reportValidity();
                    }
                  }} />
                <Button type="button" variant="link" className="h-auto p-0 text-xs text-destructive"
                  onClick={() => setEvent(ev, list.filter((_, j) => j !== i))}>
                  {t("tf.hooks.remove")}
                </Button>
              </div>
            ))}
          </div>
        );
      })}
    </div>
  );
}
