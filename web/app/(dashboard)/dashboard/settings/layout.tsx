import { PageToolbar } from "@/components/dashboard/page-toolbar";
import { SettingsLayoutBody } from "@/components/dashboard/settings-layout-body";

export default function SettingsLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <>
      <PageToolbar title="Settings" showFilters={false} />
      <div className="p-8 lg:p-10">
        <SettingsLayoutBody>{children}</SettingsLayoutBody>
      </div>
    </>
  );
}
