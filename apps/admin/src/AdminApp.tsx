import { Navigate, Route, Routes, useLocation } from "react-router-dom";
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
  const location = useLocation();
  if (user?.role === "admin") return <AdminLayout />;
  // 未登录或会话无效时重定向到登录页，并把原目标路径带回，登录后回到该页。
  return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />;
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
