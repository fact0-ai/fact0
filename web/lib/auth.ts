// Runtime-only configuration: builds do not need database or cloud credentials.
import { betterAuth } from "better-auth";
import { APIError } from "better-auth/api";
import { nextCookies } from "better-auth/next-js";
import { jwt, organization } from "better-auth/plugins";
import { Pool } from "pg";

function createAuth() {
  const databaseUrl =
    process.env.DATABASE_URL ?? process.env.FACT0_POSTGRES_DSN;
  const secret = process.env.BETTER_AUTH_SECRET?.trim();
  const origin = process.env.BETTER_AUTH_URL?.trim();
  if (!databaseUrl || !secret || secret.length < 32 || !origin) {
    throw new Error(
      "Set DATABASE_URL, BETTER_AUTH_URL and a random BETTER_AUTH_SECRET of at least 32 characters. See the self-hosted quickstart.",
    );
  }
  const baseURL = new URL(origin).origin;
  const pool = new Pool({
    connectionString: databaseUrl,
    max: 10,
    connectionTimeoutMillis: 10000,
  });
  return betterAuth({
    database: pool,
    baseURL,
    secret,
    trustedOrigins: [baseURL],
    advanced: {
      crossSubDomainCookies: { enabled: false },
      useSecureCookies: baseURL.startsWith("https:"),
    },
    emailAndPassword: {
      enabled: true,
      disableSignUp: true,
      minPasswordLength: 12,
    },
    account: { accountLinking: { enabled: false } },
    databaseHooks: {
      session: {
        create: {
          before: async (session) => {
            const r = await pool.query<{ organizationId: string }>(
              `SELECT "organizationId" FROM member WHERE "userId"=$1 AND role='owner' LIMIT 1`,
              [session.userId],
            );
            if (!r.rows[0])
              throw new APIError("FORBIDDEN", {
                message: "The local owner has not been provisioned.",
              });
            return {
              data: {
                ...session,
                activeOrganizationId: r.rows[0].organizationId,
              },
            };
          },
        },
      },
    },
    plugins: [
      organization({ allowUserToCreateOrganization: false }),
      jwt({
        jwt: {
          expirationTime: "15m",
          issuer: baseURL,
          audience: baseURL,
          definePayload: async ({ user, session }) => {
            const orgId = (session as { activeOrganizationId?: string | null })
              .activeOrganizationId;
            const r = await pool.query<{ role: string }>(
              `SELECT role FROM member WHERE "userId"=$1 AND "organizationId"=$2`,
              [user.id, orgId],
            );
            return {
              sub: user.id,
              email: user.email,
              activeOrganizationId: orgId,
              role: r.rows[0]?.role,
            };
          },
        },
      }),
      nextCookies(),
    ],
  });
}
let instance: ReturnType<typeof createAuth> | undefined;
export function getAuth() {
  return (instance ??= createAuth());
}
export type Session = ReturnType<typeof getAuth>["$Infer"]["Session"];
