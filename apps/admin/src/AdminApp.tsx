import { Navigate, Route, Routes } from "react-router-dom";
import { AdminLayout } from "./components/AdminLayout";
import { useAdminAuth } from "./lib/auth";
import { CategoriesPage } from "./pages/CategoriesPage";
import { DashboardPage } from "./pages/DashboardPage";
import { GamesPage } from "./pages/GamesPage";
import { LoginPage } from "./pages/LoginPage";
import { SystemPage } from "./pages/SystemPage";
import { UsersPage } from "./pages/UsersPage";

function ProtectedAdmin() {
  const { user } = useAdminAuth();
  return user?.role === "admin" ? <AdminLayout /> : <Navigate to="/login" replace />;
}

export function AdminApp() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<ProtectedAdmin />}>
        <Route index element={<DashboardPage />} />
        <Route path="games" element={<GamesPage />} />
        <Route path="categories" element={<CategoriesPage />} />
        <Route path="users" element={<UsersPage />} />
        <Route path="system" element={<SystemPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
