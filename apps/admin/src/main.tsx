import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { AdminApp } from "./AdminApp";
import { AdminAuthProvider } from "./lib/auth";
import "./styles.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter basename="/admin">
      <AdminAuthProvider>
        <AdminApp />
      </AdminAuthProvider>
    </BrowserRouter>
  </StrictMode>,
);
