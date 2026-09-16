"use client";
import Link from "next/link";
export function SettingsNav() {
  return (
    <nav className="space-y-2 text-sm">
      <Link className="block p-2" href="/dashboard/settings">
        Installation
      </Link>
      <Link className="block p-2" href="/dashboard/settings/api-keys">
        API keys
      </Link>
    </nav>
  );
}
