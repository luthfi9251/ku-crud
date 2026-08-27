import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { api } from "@/lib/api";
import { useT } from "@/lib/i18n";
import type { Me, StatCard } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { CardFormDialog } from "@/components/CardFormDialog";
import { StatCardView } from "@/components/StatCardView";

export default function Dashboard() {
  const t = useT();
  const qc = useQueryClient();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api<Me>("/auth/me") });
  const cards = useQuery({ queryKey: ["cards"], queryFn: () => api<StatCard[]>("/cards") });
  const [editing, setEditing] = useState<StatCard | null>(null);
  const [open, setOpen] = useState(false);

  const invalidate = () => qc.invalidateQueries({ queryKey: ["cards"] });
  const del = useMutation({
    mutationFn: (id: string) => api(`/cards/${id}`, { method: "DELETE" }),
    onSuccess: invalidate,
  });
  const move = useMutation({
    mutationFn: ({ id, dir }: { id: string; dir: "up" | "down" }) =>
      api(`/cards/${id}/move`, { method: "POST", body: JSON.stringify({ dir }) }),
    onSuccess: invalidate,
  });

  const admin = !!me.data?.manageTables;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between border-b pb-4">
        <h1 className="text-xl font-semibold">{t("dash.title")}</h1>
        {admin && (
          <Button size="sm" className="gap-1" onClick={() => { setEditing(null); setOpen(true); }}>
            <Plus className="h-4 w-4" /> {t("dash.addCard")}
          </Button>
        )}
      </div>
      {(cards.data ?? []).length === 0 && !cards.isLoading ? (
        <p className="text-sm text-muted-foreground">{admin ? t("dash.emptyAdmin") : t("dash.empty")}</p>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {(cards.data ?? []).map((c) => (
            <StatCardView
              key={c.id}
              card={c}
              {...(admin ? {
                onEdit: () => { setEditing(c); setOpen(true); },
                onDelete: () => { if (confirm(t("card.delete"))) del.mutate(c.id); },
                onMove: (up: boolean) => move.mutate({ id: c.id, dir: up ? "up" : "down" }),
              } : {})}
            />
          ))}
        </div>
      )}
      <CardFormDialog open={open} onClose={() => setOpen(false)} card={editing} />
    </div>
  );
}
