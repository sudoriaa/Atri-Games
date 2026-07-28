import { ApiClient, type AuthResponse, type User } from "@atri/shared";
import { createContext, type ReactNode, useContext, useMemo, useState } from "react";

const storageKey = "atri_admin_session";

interface AdminAuthValue {
  user: User | null;
  api: ApiClient;
  login: (email: string, password: string) => Promise<void>;
  logout: () => void;
}

const AdminAuthContext = createContext<AdminAuthValue | null>(null);

function readSession() {
  try {
    const raw = sessionStorage.getItem(storageKey);
    return raw ? (JSON.parse(raw) as AuthResponse) : null;
  } catch {
    sessionStorage.removeItem(storageKey);
    return null;
  }
}

export function AdminAuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<AuthResponse | null>(() => readSession());
  const api = useMemo(() => new ApiClient(import.meta.env.VITE_API_URL ?? "/api/v1", () => session?.token ?? null), [session?.token]);
  const save = (next: AuthResponse | null) => {
    setSession(next);
    if (next) sessionStorage.setItem(storageKey, JSON.stringify(next));
    else sessionStorage.removeItem(storageKey);
  };

  return <AdminAuthContext.Provider value={{
    user: session?.user ?? null, api,
    login: async (email, password) => {
      const response = await api.login({ email, password });
      if (response.user.role !== "admin") throw new Error("此账户没有管理权限");
      save(response);
    },
    logout: () => save(null),
  }}>{children}</AdminAuthContext.Provider>;
}

export function useAdminAuth() {
  const value = useContext(AdminAuthContext);
  if (!value) throw new Error("useAdminAuth must be used inside provider");
  return value;
}
