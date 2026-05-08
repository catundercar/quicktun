import { create } from 'zustand';
import { persist } from 'zustand/middleware';

type AuthState = {
  token: string | null;
  operatorEmail: string | null;
  operatorId: string | null;
  isAdmin: boolean;
  setSession: (s: {
    token: string;
    email: string;
    isAdmin: boolean;
    operatorId?: string;
  }) => void;
  clearSession: () => void;
};

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      token: null,
      operatorEmail: null,
      operatorId: null,
      isAdmin: false,
      setSession: ({ token, email, isAdmin, operatorId }) =>
        set({
          token,
          operatorEmail: email,
          isAdmin,
          operatorId: operatorId ?? null,
        }),
      clearSession: () =>
        set({ token: null, operatorEmail: null, operatorId: null, isAdmin: false }),
    }),
    { name: 'quicktun-auth' },
  ),
);
