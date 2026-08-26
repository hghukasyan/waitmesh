import Image from "next/image";
import Link from "next/link";

type BrandLogoProps = {
  href?: string;
  className?: string;
  markClassName?: string;
  priority?: boolean;
  showWordmark?: boolean;
  /** Prefer solid PNG for dark hero backgrounds */
  variant?: "auto" | "on-dark" | "on-light";
};

export function BrandLogo({
  href = "/",
  className = "",
  markClassName = "h-7 w-7",
  priority = false,
  showWordmark = true,
  variant = "auto",
}: BrandLogoProps) {
  const mark =
    variant === "on-dark" ? (
      <Image
        src="/brand/mark.svg"
        alt=""
        fill
        className="object-contain"
        priority={priority}
        unoptimized
      />
    ) : variant === "on-light" ? (
      <Image
        src="/brand/mark-dark.svg"
        alt=""
        fill
        className="object-contain"
        priority={priority}
        unoptimized
      />
    ) : (
      <>
        <Image
          src="/brand/mark.svg"
          alt=""
          fill
          className="object-contain dark:hidden"
          priority={priority}
          unoptimized
        />
        <Image
          src="/brand/mark-dark.svg"
          alt=""
          fill
          className="hidden object-contain dark:block"
          priority={priority}
          unoptimized
        />
      </>
    );

  const wordColor =
    variant === "on-dark"
      ? "text-white"
      : variant === "on-light"
        ? "text-[#0a0a0a]"
        : "text-foreground";

  const content = (
    <span className={`inline-flex items-center gap-2.5 ${className}`}>
      <span className={`relative inline-block shrink-0 ${markClassName}`}>{mark}</span>
      {showWordmark ? (
        <span className={`text-[15px] font-semibold tracking-[-0.03em] ${wordColor}`}>
          Waitmesh
        </span>
      ) : null}
    </span>
  );

  if (!href) return content;
  return (
    <Link href={href} className="inline-flex items-center" aria-label="Waitmesh home">
      {content}
    </Link>
  );
}
