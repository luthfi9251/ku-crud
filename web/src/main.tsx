import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HashRouter, Route, Routes } from "react-router-dom";
import App from "./App";
import Login from "./pages/Login";
import Setup from "./pages/Setup";
import Tables from "./pages/Tables";
import TableForm from "./pages/TableForm";
import Data from "./pages/Data";
import Datasources from "./pages/Datasources";
import Audit from "./pages/Audit";
import "./index.css";

const qc = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
});

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={qc}>
      <HashRouter>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/setup" element={<Setup />} />
          <Route path="/" element={<App />}>
            <Route index element={<Tables />} />
            <Route path="tables/new" element={<TableForm />} />
            <Route path="tables/:id/edit" element={<TableForm />} />
            <Route path="data/:id" element={<Data />} />
            <Route path="datasources" element={<Datasources />} />
            <Route path="audit" element={<Audit />} />
          </Route>
        </Routes>
      </HashRouter>
    </QueryClientProvider>
  </React.StrictMode>
);
