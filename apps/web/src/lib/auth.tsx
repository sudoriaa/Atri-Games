import { ApiClient, type AuthResponse, type User } from "@atri/shared";
import { createContext, type ReactNode, useCallback, useContext, useMemo, useState } from "react";

const storageKey = "atri_user_token";

interface AuthContextValue {
  user: User | null;
  token: string | null;
  api: ApiClient;
  login: (input: { email: string; password: string }) => Promise<User>;
  register: (input: { email: string; password: string; displayName: string }) => Promise<User>;
  logout: () => void;
  updateUser: (user: User) => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

function readSession(): AuthResponse | null {
  try {
    const raw = localStorage.getItem(storageKey);
    return raw ? (JSON.parse(raw) as AuthResponse) : null;
  } catch {
    localStorage.removeItem(storageKey);
    return null;
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<AuthResponse | null>(() => readSession());
  const api = useMemo(
    () => new ApiClient(import.meta.env.VITE_API_URL ?? "/api/v1", () => session?.token ?? null),
    [session?.token],
  );

  const saveSession = useCallback((next: AuthResponse | null) => {
    setSession(next);
    if (next) localStorage.setItem(storageKey, JSON.stringify(next));
    else localStorage.removeItem(storageKey);
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({
      user: session?.user ?? null,
      token: session?.token ?? null,
      api,
      login: async (input) => {
        const response = await api.login(input);
        saveSession(response);
        return response.user;
      },
      register: async (input) => {
        const response = await api.register(input);
        saveSession(response);
        return response.user;
      },
      logout: () => saveSession(null),
      updateUser: (user) => {
        if (session) saveSession({ ...session, user });
      },
    }),
    [api, saveSession, session],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const value = useContext(AuthContext);
  if (!value) throw new Error("useAuth must be used inside AuthProvider");
  return value;
}
