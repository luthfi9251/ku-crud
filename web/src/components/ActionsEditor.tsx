import type { ActionsConfig, CustomAction, HiddenActionKey } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { HelpPopover } from "@/components/ColumnListEditor";
import { ArrowUp, ArrowDown } from "lucide-react";
import { useT } from "@/lib/i18n";

const BUILTINS: HiddenActionKey[] = ["newRow", "edit", "delete", "copy", "import", "export", "refresh"];
const GRANTS = ["read", "create", "update", "delete"] as const;
const STYLES = ["neutral", "primary", "danger"] as const;

interface Props {
  value: ActionsConfig;
  names: string[];
  onChange: (v: ActionsConfig) => void;
}

export default function ActionsEditor({ value, names, onChange }: Props) {
  const t = useT();
  const hidden = new Set(value.hidden ?? []);
  const custom = value.custom ?? [];

  const toggleHidden = (key: HiddenActionKey, on: boolean) => {
    const next = new Set(hidden);
    if (on) next.delete(key);
    else next.add(key);
    onChange({ ...value, hidden: [...next] });
  };

  const setCustom = (list: CustomAction[]) =>
    onChange({ ...value, custom: list.map((a, i) => ({ ...a, order: i + 1 })) });

  const addAction = () => {
    let n = custom.length + 1;
    while (custom.some((a) => a.id === `act_${n}`)) n++;
    setCustom([...custom, {
      id: `act_${n}`, label: "", grant: "update", hook: names[0] ?? "",
      config: null, order: custom.length + 1, style: "neutral",
    }]);
  };

  const patch = (i: number, p: Partial<CustomAction>) =>
    setCustom(custom.map((a, j) => (j === i ? { ...a, ...p } : a)));

  const move = (i: number, d: -1 | 1) => {
    const j = i + d;
    if (j < 0 || j >= custom.length) return;
    const list = [...custom];
    [list[i], list[j]] = [list[j], list[i]];
    setCustom(list);
  };

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <span className="text-sm font-medium">{t("tf.actions.builtin")}</span>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
          {BUILTINS.map((k) => (
            <label key={k} className="flex items-center justify-between rounded border px-3 py-2">
              <span className="text-xs">{t(`tf.actions.builtin.${k}`)}</span>
              <Switch checked={!hidden.has(k)} onCheckedChange={(v) => toggleHidden(k, v)} />
            </label>
          ))}
        </div>
        <p className="text-[11px] text-muted-foreground">{t("tf.actions.builtinHint")}</p>
      </div>

      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <span className="text-sm font-medium">{t("tf.actions.custom")}</span>
          <Button type="button" variant="link" className="h-auto p-0 text-xs"
            onClick={addAction} disabled={names.length === 0}>
            {t("tf.actions.add")}
          </Button>
        </div>
        {names.length === 0 ? (
          <p className="text-xs text-muted-foreground">{t("tf.hooks.none")}</p>
        ) : custom.length === 0 ? (
          <p className="text-xs text-muted-foreground">{t("tf.actions.customHint")}</p>
        ) : (
          custom.map((a, i) => (
            <div key={a.id} className="rounded border p-3 space-y-2">
              <div className="flex items-center gap-2">
                <Input className="h-8 flex-1 text-xs" placeholder={t("tf.actions.label")}
                  value={a.label} onChange={(e) => patch(i, { label: e.target.value })} />
                <Button type="button" variant="ghost" size="icon" className="h-7 w-7"
                  disabled={i === 0} onClick={() => move(i, -1)} title={t("tf.actions.up")}>
                  <ArrowUp className="h-3.5 w-3.5" />
                </Button>
                <Button type="button" variant="ghost" size="icon" className="h-7 w-7"
                  disabled={i === custom.length - 1} onClick={() => move(i, 1)} title={t("tf.actions.down")}>
                  <ArrowDown className="h-3.5 w-3.5" />
                </Button>
                <Button type="button" variant="link" className="h-auto p-0 text-xs text-destructive"
                  onClick={() => setCustom(custom.filter((_, j) => j !== i))}>
                  {t("tf.hooks.remove")}
                </Button>
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
                <div className="space-y-1">
                  <Label className="text-[11px] text-muted-foreground">{t("tf.actions.hook")}</Label>
                  <Select value={a.hook} onValueChange={(v) => patch(i, { hook: v })}>
                    <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {names.map((n) => <SelectItem key={n} value={n} className="text-xs font-mono">{n}</SelectItem>)}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1">
                  <Label className="text-[11px] text-muted-foreground">
                    {t("tf.actions.grant")}
                    <HelpPopover title={t("tf.actions.grant")} placement="bottom">
                      <p>{t("tf.actions.grantHelp")}</p>
                    </HelpPopover>
                  </Label>
                  <Select value={a.grant} onValueChange={(v) => patch(i, { grant: v as CustomAction["grant"] })}>
                    <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {GRANTS.map((g) => <SelectItem key={g} value={g} className="text-xs">{t(`tf.actions.grant.${g}`)}</SelectItem>)}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1">
                  <Label className="text-[11px] text-muted-foreground">{t("tf.actions.style")}</Label>
                  <Select value={a.style} onValueChange={(v) => patch(i, { style: v as CustomAction["style"] })}>
                    <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {STYLES.map((st) => <SelectItem key={st} value={st} className="text-xs">{t(`tf.actions.style.${st}`)}</SelectItem>)}
                    </SelectContent>
                  </Select>
                </div>
              </div>
              <div className="space-y-1">
                <Label className="text-[11px] text-muted-foreground">{t("tf.actions.confirm")}</Label>
                <Input className="h-8 text-xs" placeholder={t("tf.actions.confirmPh")}
                  value={a.confirm ?? ""} onChange={(e) => patch(i, { confirm: e.target.value })} />
              </div>
              <div className="space-y-1">
                <Label className="text-[11px] text-muted-foreground">{t("tf.hooks.config")}</Label>
                <Input className="h-8 text-xs font-mono" placeholder={t("tf.hooks.config")}
                  defaultValue={a.config ? JSON.stringify(a.config) : ""}
                  onBlur={(e) => {
                    const raw = e.target.value.trim();
                    if (!raw) { patch(i, { config: null }); return; }
                    try {
                      patch(i, { config: JSON.parse(raw) });
                      e.target.setCustomValidity("");
                    } catch {
                      e.target.setCustomValidity(t("tf.hooks.configInvalid"));
                      e.target.reportValidity();
                    }
                  }} />
              </div>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
