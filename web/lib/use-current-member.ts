"use client";

import { authClient } from "./auth-client";
import { useDemoMode } from "./demo/demo-context";

const ADMIN_ROLES = new Set(["owner", "admin"]);

export function useCurrentMember() {
  const demo = useDemoMode();
  const { data: session, isPending: sessionLoading } = authClient.useSession();
  const { data: active, isPending: orgLoading } = authClient.useActiveOrganization();

  if (demo) {
    return {
      role: "owner",
      isAdmin: true,
      isOwner: true,
      isLoading: false,
      organizationId: "demo-org",
    };
  }

  const userId = session?.user.id;
  type MemberLite = { userId: string; role: string };
  const members = (active?.members ?? []) as MemberLite[];
  const me = userId ? members.find((m) => m.userId === userId) : undefined;
  const role = me?.role ?? null;

  return {
    role,
    isAdmin: !!role && ADMIN_ROLES.has(role),
    isOwner: role === "owner",
    isLoading: sessionLoading || orgLoading,
    organizationId: active?.id ?? null,
  };
}
