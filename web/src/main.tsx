import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HashRouter, Route, Routes } from "react-router-dom";
import App from "./App";
import Login from "./pages/Login";
import "./index.css";

const qc = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
});

// ponytail: plain stubs for pages landing in Tasks 14-16
const LoginLazy = Login;
const TablesLazy = () => <p>Not built yet</p>;
const DataLazy = () => <p>Not built yet</p>;
const DatasourcesLazy = () => <p>Not built yet</p>;
const AuditLazy = () => <p>Not built yet</p>;

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
