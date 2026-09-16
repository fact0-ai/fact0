import { BrandLogo } from "@/components/brand/brand-logo";
export default function AuthLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className="min-h-screen bg-background">
      <div className="p-8">
        <BrandLogo />
      </div>
      <main className="mx-auto max-w-md px-6 py-16">{children}</main>
    </div>
  );
}
