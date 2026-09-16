"use client";
import { authClient } from "@/lib/auth-client";
export function OrgSwitcher() {
  const { data: org } = authClient.useActiveOrganization();
  return (
    <div className="px-3 py-2">
      <p className="text-sm font-semibold truncate">
        {org?.name ?? "Local workspace"}
      </p>
      <p className="text-[10px] text-muted-foreground mt-1">
        SELF-HOSTED · EXPERIMENTAL
      </p>
    </div>
  );
}
