import Link from "next/link";
import { BrandLogo } from "./BrandLogo";
import { site } from "@/lib/site";

const pages = [
  { href: "/", label: "Home" },
  { href: "/#quickstart", label: "Quickstart" },
  { href: "/#docs", label: "Docs" },
  { href: "/#use-cases", label: "Use cases" },
  { href: "/#how", label: "How it works" },
  { href: "/#faq", label: "FAQ" },
];

export function Footer() {
  return (
    <footer className="border-t border-border bg-white text-[#0a0a0a]">
      <div className="site-shell grid gap-10 py-14 md:grid-cols-[1.2fr_1fr_1fr]">
        <div>
          <Link href="/" className="inline-flex items-center" aria-label="Waitmesh home">
            <BrandLogo href="" variant="on-light" markClassName="h-10 w-10" />
          </Link>
          <p className="mt-4 max-w-sm text-sm leading-relaxed text-muted">
            {site.tagline} {site.secondaryTagline}
          </p>
          <p className="mt-6 max-w-sm text-xs leading-relaxed text-muted">
            Single binary · SQLite WAL · Apache 2.0.
          </p>
        </div>
        <div>
          <p className="mono text-[11px] uppercase tracking-[0.16em] text-muted">Pages</p>
          <ul className="mt-4 grid grid-cols-2 gap-2 text-sm text-muted">
            {pages.map((page) => (
              <li key={page.href}>
                <Link href={page.href} className="hover:text-accent">
                  {page.label}
                </Link>
              </li>
            ))}
          </ul>
        </div>
        <div>
          <p className="mono text-[11px] uppercase tracking-[0.16em] text-muted">Get in touch</p>
          <ul className="mt-4 space-y-2 text-sm text-muted">
            <li>
              <a href="mailto:hello@waitmesh.com" className="hover:text-accent">
                General contact
              </a>
            </li>
            <li>
              <a href="mailto:security@waitmesh.com" className="hover:text-accent">
                Security
              </a>
            </li>
            <li>
              <a
                href={site.github}
                target="_blank"
                rel="noopener noreferrer"
                className="hover:text-accent"
              >
                Star on GitHub ↗
              </a>
            </li>
            <li>
              <a
                href={site.docs}
                target="_blank"
                rel="noopener noreferrer"
                className="hover:text-accent"
              >
                Documentation ↗
              </a>
            </li>
          </ul>
        </div>
      </div>
      <div className="border-t border-border">
        <div className="site-shell flex flex-col gap-3 py-4 mono text-[11px] text-muted sm:flex-row sm:items-center sm:justify-between">
          <p>© {new Date().getFullYear()} Waitmesh · Apache-2.0</p>
          <div className="flex flex-wrap gap-x-4 gap-y-1">
            <span>Local by default · No telemetry by default</span>
          </div>
        </div>
      </div>
    </footer>
  );
}
