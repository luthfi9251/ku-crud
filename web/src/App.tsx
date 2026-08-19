import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Outlet, useNavigate } from "react-router-dom";
import { api } from "./lib/api";
import { Button } from "@/components/ui/button";

export default function App() {
  const qc = useQueryClient();
  const nav = useNavigate();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api<{ username: string }>("/auth/me") });
  return (
    <div className="min-h-screen">
      <header className="flex items-center gap-4 border-b px-6 py-3">
        <span className="font-bold">Ku-CRUD</span>
        <nav className="flex gap-4 text-sm">
          <Link to="/">Tables</Link>
          <Link to="/datasources">Datasources</Link>
          <Link to="/audit">Audit</Link>
        </nav>
        <div className="ml-auto flex items-center gap-2 text-sm text-muted-foreground">
          {me.data && <span>{me.data.username}</span>}
          <Button variant="outline" size="sm" onClick={async () => {
            await api("/auth/logout", { method: "POST" });
            qc.clear();
            nav("/login");
          }}>Logout</Button>
        </div>
      </header>
      <main className="p-6"><Outlet /></main>
    </div>
  );
}
