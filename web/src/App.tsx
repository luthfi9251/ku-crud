import { Outlet, useLocation } from "react-router-dom";
import { Sidebar } from "./components/Sidebar";
import { Table2, Server, ShieldCheck, Database, Layers, Users as UsersIcon, KeyRound, ArrowLeftRight, BookOpen } from "lucide-react";
import { useT } from "./lib/i18n";

export default function App() {
  const location = useLocation();
  const t = useT();

  // Helper for Breadcrumb header info
  const getHeaderInfo = () => {
    const path = location.pathname;
    if (path === "/") {
      return { title: t("app.header.tables"), subtitle: t("app.header.tablesSub"), icon: Table2 };
    } else if (path === "/tables/new") {
      return { title: t("app.header.newTable"), subtitle: t("app.header.newTableSub"), icon: Table2 };
    } else if (path.includes("/tables/") && path.includes("/edit")) {
      return { title: t("app.header.editTable"), subtitle: t("app.header.editTableSub"), icon: Table2 };
    } else if (path === "/datasources") {
      return { title: t("app.header.datasources"), subtitle: t("app.header.datasourcesSub"), icon: Server };
    } else if (path === "/audit") {
      return { title: t("app.header.audit"), subtitle: t("app.header.auditSub"), icon: ShieldCheck };
    } else if (path.startsWith("/meta")) {
      return { title: t("app.header.transfer"), subtitle: t("app.header.transferSub"), icon: ArrowLeftRight };
    } else if (path === "/users") {
      return { title: t("app.header.users"), subtitle: t("app.header.usersSub"), icon: UsersIcon };
    } else if (path === "/roles") {
      return { title: t("app.header.roles"), subtitle: t("app.header.rolesSub"), icon: KeyRound };
    } else if (path.includes("/import")) {
      return { title: t("app.header.import"), subtitle: t("app.header.importSub"), icon: Layers };
    } else if (path.startsWith("/data/")) {
      return { title: t("app.header.data"), subtitle: t("app.header.dataSub"), icon: Layers };
    } else if (path === "/docs") {
      return { title: t("app.header.docs"), subtitle: t("app.header.docsSub"), icon: BookOpen };
    }
    return { title: t("app.header.default"), subtitle: t("app.header.defaultSub"), icon: Database };
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
