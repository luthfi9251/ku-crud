import { Outlet, useLocation } from "react-router-dom";
import { Sidebar } from "./components/Sidebar";
import { Table2, Server, ShieldCheck, Database, Layers } from "lucide-react";

export default function App() {
  const location = useLocation();

  // Helper for Breadcrumb header info
  const getHeaderInfo = () => {
    const path = location.pathname;
    if (path === "/") {
      return { title: "Tables & Schema", subtitle: "Configure dynamic database definitions", icon: Table2 };
    } else if (path === "/datasources") {
      return { title: "Datasources", subtitle: "Manage database connection pools", icon: Server };
    } else if (path === "/audit") {
      return { title: "Audit Trail", subtitle: "Track database mutation logs and diffs", icon: ShieldCheck };
    } else if (path.startsWith("/data/")) {
      return { title: "Data Explorer", subtitle: "Live table CRUD administration", icon: Layers };
    }
    return { title: "Ku-CRUD", subtitle: "Database Administration", icon: Database };
  };

  const headerInfo = getHeaderInfo();
  const Icon = headerInfo.icon;

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-background">
      {/* Sidebar Navigation */}
      <Sidebar />

      {/* Main Content Area */}
      <div className="flex flex-1 flex-col overflow-hidden">
        {/* Top Header Bar */}
        <header className="flex h-16 shrink-0 items-center justify-between border-b bg-card/60 px-6 backdrop-blur-md">
          <div className="flex items-center gap-3">
            <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <Icon className="h-5 w-5" />
            </div>
            <div>
              <h1 className="text-sm font-semibold text-foreground tracking-tight">{headerInfo.title}</h1>
              <p className="text-xs text-muted-foreground">{headerInfo.subtitle}</p>
            </div>
          </div>
        </header>

        {/* Dynamic Page Outlet */}
        <main className="flex-1 overflow-y-auto p-6 md:p-8">
          <div className="mx-auto w-full max-w-[1800px]">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  );
}
