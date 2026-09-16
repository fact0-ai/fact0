"use client";

import { useCallback, useMemo } from "react";
import useSWR from "swr";
import useSWRMutation from "swr/mutation";

import { authClient } from "./auth-client";
import { useDemoMode } from "./demo/demo-context";
import { DEMO_BOOTSTRAP, DEMO_KEYS, DEMO_STATUS } from "./demo/fixtures";
import {
  meClient,
  type BootstrapResponse,
  type KeyRow,
  type MeStatus,
} from "./me-api";

import { fetchToken } from "./auth-token";
export { fetchToken, invalidateTokenCache } from "./auth-token";

function useMeClient() {
  const { data: session, isPending: sessionPending } = authClient.useSession();
  const { data: activeOrg, isPending: activeOrgPending } =
    authClient.useActiveOrganization();

  const client = useMemo(() => meClient(fetchToken), []);

  // Wait for BOTH session + active organization atoms to hydrate. On hard
  // refresh, session often resolves before the active-org query replays -
  // `orgId` is briefly null while `bootstrap`/`status` SWR aren't running at
  // all, yet our old `loading` heuristic read "not loading" and flashed Setup.
  const authReady = !sessionPending && !!session?.user;
  const orgHydrated = !activeOrgPending;
  const orgId = activeOrg?.id ?? null;
  const ready = authReady && orgHydrated && orgId !== null && orgId.length > 0;

  return { client, ready, orgId };
}

/**
 * useBootstrap performs an idempotent POST /v1/me/bootstrap on first
 * dashboard load. The endpoint is race-safe (per-external-id advisory
 * lock) so calling it from multiple tabs is fine.
 *
 * IMPORTANT - the raw `key` returned on first provision is intentionally
 * NOT persisted on the client. The dashboard authenticates every audit
 * call with the user's Better Auth JWT (via DualAuth on the backend)
 * and never holds a long-lived `alk_live_*` secret in the browser.
 * Storing it in localStorage would re-introduce the XSS-exfiltration
 * risk this design was meant to remove.
 *
 * The raw key surfaces in the bootstrap response only so the keys-admin
 * UI can show it once at create time with a "copy now, we won't show
 * it again" warning. After that it lives in Postgres as a SHA-256 hash
 * and is never recoverable.
 */
export function useBootstrap() {
  const demo = useDemoMode();
  const { client, ready, orgId } = useMeClient();
  const live = useSWR<BootstrapResponse, Error>(
    ready && orgId ? `bootstrap:${orgId}` : null,
    () => client.bootstrap(),
    {
      revalidateOnFocus: false,
      revalidateIfStale: false,
      shouldRetryOnError: false,
    },
  );
  if (demo) {
    return {
      data: DEMO_BOOTSTRAP,
      error: undefined,
      isLoading: false,
      mutate: async () => DEMO_BOOTSTRAP,
    };
  }
  return live;
}

/**
 * useMeStatus polls /v1/me/status - drives the onboarding checklist
 * and the "live" indicator. Polls fast (4s) while events are zero,
 * then backs off once the chain is alive.
 *
 * Bootstrap and status now fire in parallel. `isLoading` stays true
 * until both have an initial response, which prevents the "Setup Mode"
 * flash that occurred when status resolved before bootstrap on cold loads.
 *
 * Returned `isLoading` merges bootstrap + status so dashboards can gate
 * on a single flag.
 */
export function useMeStatus() {
  const demo = useDemoMode();
  const bootstrap = useBootstrap();
  const { client, ready, orgId } = useMeClient();

  const statusKey = ready && orgId ? `status:${orgId}` : null;

  const swr = useSWR<MeStatus, Error>(
    demo ? null : statusKey,
    () => client.status(),
    {
      refreshInterval: (data) => (data?.has_activity ? 30_000 : 15_000),
      revalidateOnFocus: false,
      keepPreviousData: true,
    },
  );

  if (demo) {
    return {
      data: DEMO_STATUS,
      error: undefined,
      isLoading: false,
      mutate: async () => DEMO_STATUS,
    };
  }

  const awaitingFirstStatus =
    !!statusKey && swr.data === undefined && swr.error === undefined;

  const isLoading =
    !ready ||
    (ready && !!orgId && (bootstrap.isLoading || awaitingFirstStatus));

  return { ...swr, isLoading };
}

export function useKeys() {
  const demo = useDemoMode();
  const { client, ready, orgId } = useMeClient();
  const live = useSWR<{ keys: KeyRow[] }, Error>(
    ready && orgId ? `keys:${orgId}` : null,
    () => client.listKeys(),
    { revalidateOnFocus: false, keepPreviousData: true },
  );
  if (demo) {
    return {
      data: { keys: DEMO_KEYS },
      error: undefined,
      isLoading: false,
      mutate: async () => ({ keys: DEMO_KEYS }),
    };
  }
  return live;
}

export function useCreateKey() {
  const demo = useDemoMode();
  const { client } = useMeClient();
  return useCallback(
    async (scope: "read" | "write", label?: string) => {
      if (demo) throw new Error("API key changes are unavailable in demo mode");
      return client.createKey(scope, label);
    },
    [client, demo],
  );
}

export function useRevokeKey() {
  const demo = useDemoMode();
  const { client, orgId } = useMeClient();
  const { trigger } = useSWRMutation(
    orgId ? `keys:${orgId}` : null,
    async (_key: string, { arg }: { arg: string }) => {
      if (demo) throw new Error("API key changes are unavailable in demo mode");
      await client.revokeKey(arg);
    },
  );
  return trigger;
}
