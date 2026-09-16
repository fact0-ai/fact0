#!/usr/bin/env node
import { randomUUID } from "node:crypto";
import { createInterface } from "node:readline/promises";
import { Pool } from "pg";
import { hashPassword } from "better-auth/crypto";

const args = process.argv.slice(2);
const command = args[0];
const option = (name) => {
  const i = args.indexOf(name);
  return i < 0 ? undefined : args[i + 1];
};
if (!["create", "reset-password"].includes(command)) {
  console.error(
    "Usage: node scripts/owner.mjs create|reset-password [--email EMAIL] [--name NAME] [--password-stdin]",
  );
  process.exit(1);
}
async function prompt(label) {
  if (!process.stdin.isTTY)
    throw new Error(
      "Pass --email when using --password-stdin or non-interactive input.",
    );
  const rl = createInterface({ input: process.stdin, output: process.stderr });
  try {
    return await rl.question(label);
  } finally {
    rl.close();
  }
}
const email = (option("--email") ?? (await prompt("Owner email: ")))
  .trim()
  .toLowerCase();
const ownerName =
  option("--name") ??
  (command === "create" &&
  process.stdin.isTTY &&
  !args.includes("--password-stdin")
    ? (await prompt("Owner name [Local owner]: ")).trim() || "Local owner"
    : "Local owner");
if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email))
  throw new Error("A valid owner email is required.");
async function readPassword() {
  if (args.includes("--password-stdin") || !process.stdin.isTTY) {
    let input = "";
    for await (const chunk of process.stdin) input += chunk;
    return input.replace(/\r?\n$/, "");
  }
  process.stderr.write("Owner password (12+ characters): ");
  process.stdin.setRawMode(true);
  process.stdin.resume();
  process.stdin.setEncoding("utf8");
  return new Promise((resolve, reject) => {
    let value = "";
    function finish() {
      process.stdin.setRawMode(false);
      process.stdin.pause();
      process.stdin.removeListener("data", onData);
      process.stderr.write("\n");
    }
    function onData(chunk) {
      for (const ch of chunk) {
        if (ch === "\u0003") {
          finish();
          reject(new Error("Cancelled"));
          return;
        }
        if (ch === "\r" || ch === "\n") {
          finish();
          resolve(value);
          return;
        }
        if (ch === "\u007f" || ch === "\b") value = value.slice(0, -1);
        else value += ch;
      }
    }
    process.stdin.on("data", onData);
  });
}
const password = await readPassword();
if (password.length < 12 || password.length > 128)
  throw new Error("Use a password between 12 and 128 characters.");
const dsn = process.env.DATABASE_URL ?? process.env.FACT0_POSTGRES_DSN;
if (!dsn)
  throw new Error(
    "DATABASE_URL is required. Apply the database migrations first.",
  );
const pool = new Pool({ connectionString: dsn });
const conn = await pool.connect();
try {
  const hashed = await hashPassword(password);
  await conn.query("BEGIN");
  await conn.query(
    "SELECT pg_advisory_xact_lock(hashtext('fact0-local-owner-setup'))",
  );
  const users = await conn.query(
    'SELECT id, email FROM "user" ORDER BY "createdAt" LIMIT 2',
  );
  if (command === "create") {
    if (
      users.rows.length &&
      (users.rows.length !== 1 || users.rows[0].email.toLowerCase() !== email)
    ) {
      throw new Error(
        "This installation already has an owner. Use reset-password for the existing owner.",
      );
    }
    if (!users.rows.length) {
      const userId = randomUUID(),
        orgId = randomUUID();
      await conn.query(
        'INSERT INTO "user" (id,name,email,"emailVerified","createdAt","updatedAt") VALUES ($1,$2,$3,true,NOW(),NOW())',
        [userId, ownerName, email],
      );
      await conn.query(
        'INSERT INTO account (id,"accountId","providerId","userId",password,"createdAt","updatedAt") VALUES ($1,$2,\'credential\',$2,$3,NOW(),NOW())',
        [randomUUID(), userId, hashed],
      );
      await conn.query(
        "INSERT INTO organization (id,name,slug,\"createdAt\") VALUES ($1,$2,'local',NOW())",
        [orgId, "Local workspace"],
      );
      await conn.query(
        'INSERT INTO member (id,"organizationId","userId",role,"createdAt") VALUES ($1,$2,$3,\'owner\',NOW())',
        [randomUUID(), orgId, userId],
      );
      console.log("Local owner and workspace created.");
    } else
      console.log(
        "Owner already exists; checking local workspace provisioning.",
      );
  } else {
    const user = users.rows.find((u) => u.email.toLowerCase() === email);
    if (!user || users.rows.length !== 1)
      throw new Error("No matching single owner.");
    const account = await conn.query(
      'UPDATE account SET password=$1,"updatedAt"=NOW() WHERE "userId"=$2 AND "providerId"=\'credential\' RETURNING id',
      [hashed, user.id],
    );
    if (!account.rowCount)
      throw new Error("Owner password account is missing.");
    await conn.query('DELETE FROM session WHERE "userId"=$1', [user.id]);
    console.log(
      "Owner password reset; browser sessions revoked. Existing JWTs expire within 15 minutes.",
    );
  }
  await conn.query("COMMIT");
} catch (error) {
  await conn.query("ROLLBACK");
  throw error;
} finally {
  conn.release();
  await pool.end();
}

if (command === "create") {
  const origin = process.env.BETTER_AUTH_URL ?? "http://localhost:3000";
  const localWeb = process.env.FACT0_OWNER_WEB_URL ?? "http://127.0.0.1:3000";
  const backend = process.env.FACT0_BACKEND_URL ?? "http://localhost:8000";
  let cookie = "";
  try {
    const login = await fetch(`${localWeb}/api/auth/sign-in/email`, {
      method: "POST",
      headers: { "Content-Type": "application/json", Origin: origin },
      body: JSON.stringify({ email, password }),
    });
    if (!login.ok)
      throw new Error(
        `Local sign-in failed (${login.status}); check the supplied password and running web service.`,
      );
    cookie = login.headers
      .getSetCookie()
      .map((c) => c.split(";")[0])
      .join("; ");
    const tokenResponse = await fetch(`${localWeb}/api/auth/token`, {
      headers: { Cookie: cookie },
    });
    const { token } = await tokenResponse.json();
    if (!token) throw new Error("Could not issue the owner JWT.");
    const bootstrap = await fetch(`${backend}/v1/me/bootstrap`, {
      method: "POST",
      headers: { Authorization: `Bearer ${token}` },
    });
    if (!bootstrap.ok)
      throw new Error(
        `API bootstrap failed (${bootstrap.status}). Check the API/JWKS configuration.`,
      );
    const provisioned = await bootstrap.json();
    console.log(`Tenant: ${provisioned.tenant?.id}`);
    if (provisioned.key)
      console.log(
        `Initial API key (shown once; save securely): ${provisioned.key}`,
      );
    else
      console.log(
        "Workspace is provisioned. Existing key secrets are not recoverable; create a key in Settings if needed.",
      );
    console.log(`Sign in at ${origin}/sign-in`);
  } catch (error) {
    console.error(
      `Owner is saved, but API provisioning is incomplete: ${error.message}`,
    );
    console.error(
      "Start the web/API services, then rerun the same create command with the owner password.",
    );
    process.exitCode = 1;
  } finally {
    if (cookie)
      await fetch(`${localWeb}/api/auth/sign-out`, {
        method: "POST",
        headers: {
          Cookie: cookie,
          Origin: origin,
          "Content-Type": "application/json",
        },
        body: "{}",
      }).catch(() => {});
  }
}
