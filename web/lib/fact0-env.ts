/** Resolve Fact0 environment variables. */

export function fact0BackendUrl(fallback = "http://localhost:8000"): string {
  return (process.env.FACT0_BACKEND_URL?.trim() || fallback).replace(/\/+$/, "");
}

export function fact0PostgresDsn(): string | undefined {
  return process.env.DATABASE_URL ?? process.env.FACT0_POSTGRES_DSN;
}

export function fact0Env(name: string): string | undefined {
  return process.env[`FACT0_${name}`];
}
