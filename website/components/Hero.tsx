"use client";

import Link from "next/link";
import { useEffect, useState, useSyncExternalStore } from "react";
import { BrandLogo } from "./BrandLogo";
import { site } from "@/lib/site";
import { TerminalDemo } from "./TerminalDemo";

function subscribeReducedMotion(onStoreChange: () => void) {
  const media = window.matchMedia("(prefers-reduced-motion: reduce)");
  media.addEventListener("change", onStoreChange);
  return () => media.removeEventListener("change", onStoreChange);
}

function TypeBracket({ text }: { text: string }) {
  const reducedMotion = useSyncExternalStore(
    subscribeReducedMotion,
    () => window.matchMedia("(prefers-reduced-motion: reduce)").matches,
    () => true,
  );
  const [count, setCount] = useState(0);

  useEffect(() => {
    if (reducedMotion) {
      return;
    }
    let i = 0;
    const timer = window.setInterval(() => {
      i += 1;
      setCount(i);
      if (i >= text.length) window.clearInterval(timer);
    }, 70);
    return () => window.clearInterval(timer);
  }, [text, reducedMotion]);

  const visible = reducedMotion ? text.length : count;

  return (
    <span className="pixel-bracket mono inline-flex items-end tracking-normal">
      [{" "}
      {text.slice(0, visible)}
      <span className="caret" aria-hidden /> ]
    </span>
  );
}

const heroStats = [
  ["83µs", "p50 ingest · durable"],
  ["15k+", "events/s concurrent"],
  ["0", "lost timers on crash"],
  ["FULL", "SQLite synchronous"],
];

export function Hero() {
  return (
    <section>
      <div className="hero-grid border-b border-white/10 text-white">
        <div className="site-shell flex items-center justify-between py-9">
          <div className="flex items-center gap-3">
            <Link href="/" className="inline-flex items-center" aria-label="Waitmesh home">
              <BrandLogo
                href=""
                variant="on-dark"
                markClassName="h-[52px] w-[52px]"
                priority
              />
            </Link>
          </div>
          <p className="scroll-hint mono hidden text-[11px] uppercase tracking-[0.16em] text-white/45 sm:block">
            Scroll for more ↓
          </p>
        </div>
      </div>

      <div className="border-b border-border bg-background">
        <div className="site-shell grid gap-10 py-10 sm:py-14 lg:grid-cols-[1.15fr_0.85fr] lg:items-start lg:gap-16 lg:py-16">
          <h1 className="reveal max-w-3xl text-[clamp(2.5rem,5.6vw,4.2rem)] font-normal leading-[1.08] tracking-[-0.065em]">
            Know when something
            <br />
            <span className="whitespace-nowrap">
              <TypeBracket text="didn't" /> happen.
            </span>
          </h1>

          <div className="reveal reveal-delay-hero">
            <p className="text-[15px] leading-relaxed text-[#555]">
              Event conditions for absence, sequence, and count. Durable timers survive crashes.
              Single-writer SQLite with WAL and full sync. REST, CLI, and Go SDK — open source under
              Apache 2.0.
            </p>
            <div className="mt-8 flex flex-wrap items-center gap-6">
              <Link href="/#quickstart" className="btn-primary min-w-[180px]">
                <span className="inline-block h-2.5 w-2.5 bg-white" aria-hidden />
                Quickstart
              </Link>
              <a
                href={site.github}
                target="_blank"
                rel="noopener noreferrer"
                className="btn-ghost"
              >
                ▷ Star on GitHub
              </a>
            </div>
            <p className="mono mt-10 text-[11px] text-[#777]">
              <span className="text-accent">&gt;</span> event-condition engine{" "}
              <span className="uppercase">for what never arrived</span>
            </p>
          </div>
        </div>

        <div className="site-shell pb-8">
          <div className="grid grid-cols-2 border border-border sm:grid-cols-4">
            {heroStats.map(([value, label]) => (
              <div
                key={label}
                className="border-b border-border px-4 py-5 sm:border-b-0 sm:border-r sm:last:border-r-0 odd:border-r sm:odd:border-r"
              >
                <p className="text-2xl tracking-tight text-accent sm:text-3xl">{value}</p>
                <p className="mono mt-2 text-[10px] uppercase tracking-[0.14em] text-muted">{label}</p>
              </div>
            ))}
          </div>
        </div>

        <div className="site-shell pb-12">
          <TerminalDemo />
        </div>
      </div>
    </section>
  );
}
