import { create } from 'zustand';
import { persist } from 'zustand/middleware';

type AuthState = {
  token: string | null;
  operatorEmail: string | null;
  isAdmin: boolean;
  setSession: (s: { token: string; email: string; isAdmin: boolean }) => void;
  clearSession: () => void;
};

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      token: null,
      operatorEmail: null,
      isAdmin: false,
      setSession: ({ token, email, isAdmin }) =>
        set({ token, operatorEmail: email, isAdmin }),
      clearSession: () =>
        set({ token: null, operatorEmail: null, isAdmin: false }),
    }),
    { name: 'quicktun-auth' },
  ),
);
