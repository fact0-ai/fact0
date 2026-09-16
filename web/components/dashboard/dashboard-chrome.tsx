"use client";
import { SWRConfig } from "swr";
import { useState } from "react";
import { Menu } from "lucide-react";
import { TooltipProvider } from "@/components/ui/tooltip";
import { OrgSwitcher } from "./org-switcher";
import { SidebarNav } from "./sidebar-nav";
import { SidebarFooter } from "./sidebar-footer";
export function DashboardChrome({
  children,
}: {
  children: React.ReactNode;
  demo?: boolean;
}) {
  const [collapsed, setCollapsed] = useState(false);
  return (
    <SWRConfig value={{ dedupingInterval: 5000, revalidateOnFocus: false }}>
      <TooltipProvider>
        <div className="min-h-screen bg-background">
          <aside
            className={`fixed inset-y-0 left-0 z-40 flex flex-col border-r bg-background ${collapsed ? "w-16" : "w-64"}`}
          >
            <button
              aria-label="Toggle sidebar"
              className="p-4 self-end"
              onClick={() => setCollapsed(!collapsed)}
            >
              <Menu className="size-4" />
            </button>
            {!collapsed && <OrgSwitcher />}
            <SidebarNav isCollapsed={collapsed} />
            <SidebarFooter isCollapsed={collapsed} />
          </aside>
          <main className={collapsed ? "ml-16" : "ml-64"}>{children}</main>
        </div>
      </TooltipProvider>
    </SWRConfig>
  );
}
