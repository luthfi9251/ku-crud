import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HashRouter, Route, Routes } from "react-router-dom";
import App from "./App";
import Login from "./pages/Login";
import Tables from "./pages/Tables";
import Data from "./pages/Data";
import Datasources from "./pages/Datasources";
import Audit from "./pages/Audit";
import "./index.css";

const qc = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
});

// ponytail: plain stubs for pages landing in Tasks 14-16
const LoginLazy = Login;
const TablesLazy = Tables;
// ponytail: plain table grid lives in Data.tsx
const DataLazy = Data;
const DatasourcesLazy = Datasources;
const AuditLazy = Audit;

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={qc}>
      <HashRouter>
        <Routes>
          <Route path="/login" element={<LoginLazy />} />
          <Route path="/" element={<App />}>
            <Route index element={<TablesLazy />} />
            <Route path="data/:id" element={<DataLazy />} />
            <Route path="datasources" element={<DatasourcesLazy />} />
            <Route path="audit" element={<AuditLazy />} />
          </Route>
        </Routes>
      </HashRouter>
    </QueryClientProvider>
  </React.StrictMode>
);
