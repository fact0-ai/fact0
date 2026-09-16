"use client";

import { jwtClient, organizationClient } from "better-auth/client/plugins";
import { createAuthClient } from "better-auth/react";

// Single shared client. Better Auth recommends one instance per app -
// the React hooks (`useSession`, `useActiveOrganization`, etc.) are
// stable across re-renders only when imported from the same module.
export const authClient = createAuthClient({
  // Browser requests always use this installation’s origin.
  plugins: [organizationClient(), jwtClient()],
});

export const {
  signIn,
  signOut,
  useSession,
  useActiveOrganization,
  token: getToken,
} = authClient;
