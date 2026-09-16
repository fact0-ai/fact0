"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  Activity,
  BarChart3,
  Terminal,
  Bot,
  ShieldCheck,
  History,
  Settings,
  BookOpen,
} from "lucide-react";
import { docsHref } from "@/lib/docs-origin";
import { capabilities } from "@/lib/capabilities";
import { cn } from "@/lib/utils";
const items = [
  ["Dashboard", "/dashboard", Activity],
  ["Executions", "/dashboard/executions", Terminal],
  ["Coding Agents", "/dashboard/coding-agents", Bot],
  ["Observability", "/dashboard/observability", BarChart3],
  ["Audit Logs", "/dashboard/audit", ShieldCheck],
  ["Replay", "/dashboard/replay", History],
  ["Settings", "/dashboard/settings", Settings],
] as const;
export function SidebarNav({ isCollapsed }: { isCollapsed?: boolean }) {
  const pathname = usePathname();
  return (
    <nav className="flex-1 p-3 space-y-1">
      {items
        .filter(
          ([, href]) =>
            href !== "/dashboard/replay" || capabilities.executionReplay,
        )
        .map(([label, href, Icon]) => (
          <Link
            key={href}
            href={href}
            title={label}
            className={cn(
              "flex items-center gap-3 rounded-lg p-3 text-sm text-muted-foreground hover:bg-muted",
              pathname === href && "bg-muted text-foreground",
              isCollapsed && "justify-center",
            )}
          >
            <Icon className="size-4 shrink-0" />
            {!isCollapsed && label}
          </Link>
        ))}
      <a
        href={docsHref()}
        className="flex items-center gap-3 p-3 text-sm text-muted-foreground"
        title="Docs"
      >
        <BookOpen className="size-4" />
        {!isCollapsed && "Docs"}
      </a>
    </nav>
  );
}
