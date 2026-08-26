import type { ReactNode } from "react";

export function SectionIndex({ current, total = 11 }: { current: string; total?: number }) {
  return (
    <p className="section-index">
      [N.{current}/{String(total).padStart(2, "0")}]{" "}
      <span className="text-foreground/40">&gt;</span>{" "}
    </p>
  );
}

export function SlashTitle({ children, light = false }: { children: ReactNode; light?: boolean }) {
  return (
    <h2 className={`slash-title ${light ? "text-white" : "text-foreground"}`}>
      <span className="text-accent">/</span> {children} <span className="text-accent">/</span>
    </h2>
  );
}

export function Reveal({
  children,
  className = "",
  delay = 0,
}: {
  children: ReactNode;
  className?: string;
  delay?: number;
}) {
  const delayStep = Math.min(12, Math.max(0, Math.round(delay / 0.05)));

  return (
    <div
      className={`reveal reveal-y-20${className ? ` ${className}` : ""}`}
      {...(delayStep > 0 ? { "data-reveal-delay": String(delayStep) } : {})}
    >
      {children}
    </div>
  );
}

export function AeyeSection({
  id,
  index,
  kicker,
  title,
  children,
  action,
  dark = false,
  dotted = false,
}: {
  id?: string;
  index: string;
  kicker: string;
  title: ReactNode;
  children: ReactNode;
  action?: ReactNode;
  dark?: boolean;
  dotted?: boolean;
}) {
  return (
    <section
      id={id}
      className={`border-b border-border py-16 sm:py-20 ${
        dark ? "bg-dark text-white" : "bg-white text-foreground"
      } ${dotted ? (dark ? "dark-dot-grid" : "dot-grid") : ""}`}
    >
      <div className="site-shell">
        <div className="flex flex-col gap-6 border-b border-border/70 pb-8 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <div className="flex flex-wrap items-center gap-2">
              <SectionIndex current={index} />
              <span
                className={`mono text-[11px] uppercase tracking-[0.12em] ${
                  dark ? "text-white/50" : "text-muted"
                }`}
              >
                {kicker}
              </span>
            </div>
            <Reveal>
              <SlashTitle light={dark}>{title}</SlashTitle>
            </Reveal>
          </div>
          {action ? <div className="shrink-0">{action}</div> : null}
        </div>
        <div className="mt-10">{children}</div>
      </div>
    </section>
  );
}
