"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import { site } from "@/lib/site";
import { BrandLogo } from "./BrandLogo";
import { ThemeToggle } from "./ThemeToggle";

const desktopLinks = [
  { href: "/", label: "HOME" },
  { href: "/#quickstart", label: "QUICKSTART" },
  { href: "/#docs", label: "DOCS" },
  { href: "/#use-cases", label: "USE CASES" },
  { href: "/#how", label: "HOW IT WORKS" },
  { href: "/#faq", label: "FAQ" },
];

const mobileLinks = [
  ...desktopLinks,
  { href: site.github, label: "GITHUB" },
  { href: "mailto:hello@waitmesh.com", label: "CONTACT" },
];

function isPathMatch(pathname: string, href: string) {
  if (href === "/") return pathname === "/";
  if (href.startsWith("/#")) return pathname === "/";
  return pathname === href || pathname.startsWith(`${href}/`);
}

export function Header() {
  const pathname = usePathname();
  const [open, setOpen] = useState(false);
  const activeHref = desktopLinks
    .filter((link) => isPathMatch(pathname, link.href))
    .sort((a, b) => b.href.length - a.href.length)[0]?.href;

  return (
    <header className="sticky top-0 z-50 border-b border-[#ddd] bg-white/95 backdrop-blur-sm dark:border-white/10 dark:bg-[#0a0a0a]/95">
      <div className="flex min-h-12 items-stretch justify-between">
        <nav className="hidden items-stretch lg:flex" aria-label="Primary">
          {desktopLinks.map((link) => {
            const active = link.href === activeHref;
            return (
              <Link
                key={link.href}
                href={link.href}
                className={`mono flex items-center border-r border-[#ddd] px-3 text-[11px] uppercase leading-none tracking-[0.1em] dark:border-white/10 ${
                  active
                    ? "relative z-[1] -mb-px bg-accent text-white"
                    : "text-[#1a1a1a] hover:bg-[#f5f5f5] dark:text-white dark:hover:bg-white/5"
                }`}
              >
                {active ? (
                  <>
                    &lt;&nbsp;{link.label}&nbsp;&gt;
                  </>
                ) : (
                  link.label
                )}
              </Link>
            );
          })}
        </nav>

        <Link
          href="/"
          className="flex items-center px-4 py-2 lg:hidden"
          onClick={() => setOpen(false)}
        >
          <BrandLogo href="" markClassName="h-7 w-7" />
        </Link>

        <div className="ml-auto flex items-stretch">
          <div className="flex items-center border-l border-[#ddd] px-2 dark:border-white/10">
            <ThemeToggle />
          </div>
          <a
            href={site.github}
            target="_blank"
            rel="noopener noreferrer"
            className="hidden items-center border-l border-[#ddd] px-3 text-[11px] font-medium uppercase tracking-[0.1em] hover:bg-[#f5f5f5] dark:border-white/10 dark:hover:bg-white/5 sm:inline-flex"
          >
            GitHub ☆
          </a>
          <a
            href="mailto:hello@waitmesh.com"
            className="relative hidden items-center gap-2 border-l border-[#ddd] px-4 text-[11px] font-medium uppercase tracking-[0.1em] hover:bg-[#f5f5f5] dark:border-white/10 dark:hover:bg-white/5 md:inline-flex"
          >
            <span className="absolute left-3 top-2 text-[10px]" aria-hidden>
              ↗
            </span>
            <span className="pl-3">Contact</span>
          </a>
          <button
            type="button"
            className="inline-flex items-center border-l border-[#ddd] px-4 text-xs uppercase tracking-[0.1em] dark:border-white/10 lg:hidden"
            aria-expanded={open}
            aria-controls="mobile-nav"
            onClick={() => setOpen((v) => !v)}
          >
            Menu
          </button>
        </div>
      </div>

      {open ? (
        <nav
          id="mobile-nav"
          className="border-t border-[#ddd] dark:border-white/10 lg:hidden"
          aria-label="Mobile"
        >
          {mobileLinks.map((link) => (
            <Link
              key={link.href}
              href={link.href}
              onClick={() => setOpen(false)}
              className="mono block border-b border-[#ddd] px-4 py-3 text-[11px] uppercase tracking-[0.1em] dark:border-white/10"
            >
              {link.label}
            </Link>
          ))}
        </nav>
      ) : null}
    </header>
  );
}
