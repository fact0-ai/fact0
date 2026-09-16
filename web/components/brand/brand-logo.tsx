import Link from "next/link";

import {
  BRAND_LOGO_ASPECT,
  BRAND_LOGO_HEIGHT_PX,
  BRAND_LOGO_PATH,
  type BrandLogoSize,
} from "@/lib/brand-assets";
import { cn } from "@/lib/utils";
import content from "../../content.json";

interface Props {
  /** Wrap the wordmark in a Link to the given href. Pass null for no link. */
  href?: string | null;
  /** Height token - width follows the SVG aspect ratio. */
  size?: BrandLogoSize;
  className?: string;
}

function LogoImage({ size, className }: Pick<Props, "size" | "className">) {
  const height = BRAND_LOGO_HEIGHT_PX[size ?? "md"];
  const width = Math.round(height * BRAND_LOGO_ASPECT);

  return (
    // eslint-disable-next-line @next/next/no-img-element -- static SVG wordmark from /public
    <img
      src={BRAND_LOGO_PATH}
      alt={content.brand.logoText}
      width={width}
      height={height}
      className={cn("block h-auto w-auto max-w-full dark:invert", className)}
      style={{ height }}
    />
  );
}

/** Canonical Fact0 wordmark (navbar, sidebar, auth, admin, share). */
export function BrandLogo({ href = "/", size = "md", className }: Props) {
  const inner = <LogoImage size={size} className={className} />;

  if (!href) return inner;
  return (
    <Link href={href} className="inline-flex shrink-0 items-center">
      {inner}
    </Link>
  );
}
