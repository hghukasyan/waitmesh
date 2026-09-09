"use client";

import Link from "next/link";
import { useState } from "react";
import { AeyeSection, Reveal } from "./AeyeUI";
import { Hero } from "./Hero";
import { site } from "@/lib/site";

const marqueeItems = [
  "15k+ EVT/S",
  "p50 83µs",
  "p99 <1ms",
  "5k PENDING RESTART",
  "0 LOST TIMERS",
  "WAL + FULL",
  "1 WRITER",
  "3 PRIMITIVES",
  "12,847 ARMED",
  "Apache 2.0",
];

const stackMarks = ["Go 1.26", "SQLite WAL", "REST", "CLI", "Docker", "Outbox", "M4 bench"];

const valueCards = [
  {
    id: "01",
    title: "Absence",
    body: "Start a clock on one event. Cancel if another arrives in time. Fire when the window closes empty.",
    tag: "didn't happen",
    metrics: [
      ["15m", "typical window"],
      ["1:1", "start → cancel"],
      ["0ms", "late cancel race"],
    ],
  },
  {
    id: "02",
    title: "Sequence",
    body: "Require ordered steps within a deadline. Claim, advance, or expire — durable across restarts.",
    metrics: [
      ["3–12", "steps common"],
      ["6.6k", "events/s"],
      ["292", "allocs/op"],
    ],
    tag: "ordered steps",
  },
  {
    id: "03",
    title: "Count",
    body: "Accumulate matching events until a threshold. Reset windows, fire once, or keep counting.",
    metrics: [
      ["N≥50", "storm threshold"],
      ["4.6k", "events/s"],
      ["−55%", "vs baseline"],
    ],
    tag: "N of kind",
  },
];

const capabilities = [
  {
    id: "01",
    label: "Absence rules",
    body: "payment.initiated without payment.completed in 15m. Heartbeat missed after 90s. Approval never signed in 48h.",
  },
  {
    id: "02",
    label: "Sequence rules",
    body: "A → B → C with per-step deadlines. Claim ownership so 8 concurrent workers don't double-fire.",
  },
  {
    id: "03",
    label: "Count rules",
    body: "N matching events in a window. 50 failures / 5m = storm. Quorum of 3 confirms before advance.",
  },
  {
    id: "04",
    label: "Durable outbox",
    body: "Fires land in a crash-safe outbox. At-least-once delivery; ACK latency measured separately from ingest.",
  },
];

const steps = [
  ["01", "Install", "Binary or Docker. One SQLite path. One API token. Listen in seconds."],
  ["02", "Define", "Absence / sequence / count via CLI or REST. Rules cached, invalidated on mutate."],
  ["03", "Ingest", "type + key. p50 ~83µs with durable Commit on Apple M4."],
  ["04", "Fire", "Timers expire, conditions match, outbox #N queued for delivery."],
];

const installTabs = [
  {
    label: "Binary",
    code: `$ go install github.com/waitmesh/waitmesh/cmd/waitmesh@latest
$ waitmesh serve --db ./waitmesh.db --token $WAITMESH_TOKEN
# listening :8080 · WAL=on · synchronous=FULL`,
  },
  {
    label: "Docker",
    code: `$ docker run -p 8080:8080 \\
    -e WAITMESH_TOKEN=secret \\
    -v $(pwd)/data:/data \\
    waitmesh/waitmesh:0.1.0 \\
    serve --db /data/waitmesh.db
# health 200 · auth 401 without token`,
  },
  {
    label: "CLI rule",
    code: `$ waitmesh rules create \\
    --type absence \\
    --name payment.timeout \\
    --start payment.initiated \\
    --cancel payment.completed \\
    --within 15m
# rule_id=7 · armed=0 · window=900000ms`,
  },
];

const benches = [
  { name: "Sequence", rate: "6,661", unit: "evt/s", delta: "+14%", note: "match path" },
  { name: "Count", rate: "4,635", unit: "evt/s", delta: "+55%", note: "threshold path" },
  { name: "Mixed", rate: "8,793", unit: "evt/s", delta: "+21%", note: "abs+seq+cnt" },
  { name: "Concurrent×8", rate: "15,289", unit: "evt/s", delta: "+10%", note: "producers" },
];

const latencies = [
  ["Sequence start", "83µs", "127µs", "504µs"],
  ["Sequence then", "154µs", "227µs", "772µs"],
  ["Count ingest", "216µs", "313µs", "876µs"],
];

const stressChecks = [
  ["16", "publishers + timer sweep"],
  ["5,000", "pending backlog · reopen · drain"],
  ["0", "lost deadlines after kill"],
  ["1", "writer · MaxOpenConns"],
];

const useCases = [
  {
    name: "Payment timeouts",
    badge: "absence",
    summary: "Fire if payment.completed never follows payment.initiated.",
    stats: [
      ["15m", "SLA window"],
      ["~2%", "typical fire rate"],
      ["<1ms", "claim → outbox"],
    ],
  },
  {
    name: "Onboarding sequences",
    badge: "sequence",
    summary: "signup → verify → fund within deadlines; expire incomplete funnels.",
    stats: [
      ["3", "required steps"],
      ["24h / 7d", "step budgets"],
      ["claim", "no double fire"],
    ],
  },
  {
    name: "Retry storms",
    badge: "count",
    summary: "Alert when N failures share a key inside a sliding window.",
    stats: [
      ["N=50", "threshold"],
      ["5m", "window"],
      ["1×", "fire then reset"],
    ],
  },
  {
    name: "Heartbeat watch",
    badge: "absence",
    summary: "Agent check-in stops arriving — page before customers notice.",
    stats: [
      ["90s", "miss window"],
      ["cancel", "on ping"],
      ["24/7", "armed"],
    ],
  },
];

const faqs = [
  {
    q: "How is this different from a cron or queue delay?",
    a: "Cron fires on a schedule. Queues delay a known job. Waitmesh watches for missing or ordered events and only fires when the condition is true — including when nothing arrives.",
  },
  {
    q: "Does it survive process crashes?",
    a: "Yes. Timers and outbox rows live in SQLite with WAL and synchronous=FULL. Stress tests reopen a 5,000-pending backlog and drain with zero lost deadlines.",
  },
  {
    q: "What do the bench numbers mean?",
    a: "Measured on Apple M4 with durable Commit (fsync). Sequence ~6.6k evt/s, concurrent×8 ~15k evt/s, sequence start ingest p50 83µs. Not marketing — see docs/BENCHMARKS.md.",
  },
  {
    q: "Is there telemetry?",
    a: "No. Local by default. Nothing phones home unless you wire your own integrations.",
  },
  {
    q: "What license?",
    a: "Apache 2.0. Use it in production, fork it, ship it.",
  },
];

function Marquee() {
  const loop = [...marqueeItems, ...marqueeItems];
  return (
    <div className="marquee">
      <div className="marquee-track">
        {loop.map((item, index) => (
          <div key={`${item}-${index}`} className="marquee-item mono">
            {item}
          </div>
        ))}
      </div>
    </div>
  );
}

function WireIcon({ variant }: { variant: 1 | 2 | 3 }) {
  if (variant === 1) {
    return (
      <svg
        viewBox="0 0 120 80"
        className="h-20 w-28 text-foreground"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
      >
        <ellipse cx="40" cy="40" rx="22" ry="28" />
        <circle cx="82" cy="40" r="16" />
      </svg>
    );
  }
  if (variant === 2) {
    return (
      <svg
        viewBox="0 0 120 80"
        className="h-20 w-28 text-foreground"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
      >
        <path d="M20 55 L50 35 L80 55 L50 75 Z" />
        <path d="M30 40 L60 20 L90 40 L60 60 Z" />
        <path d="M40 25 L70 5 L100 25 L70 45 Z" />
      </svg>
    );
  }
  return (
    <svg
      viewBox="0 0 120 80"
      className="h-20 w-28 text-foreground"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
    >
      <path d="M60 8 L68 32 L94 32 L74 48 L82 72 L60 56 L38 72 L46 48 L26 32 L52 32 Z" />
    </svg>
  );
}

function CapabilitySwitcher() {
  const [active, setActive] = useState(0);
  return (
    <div className="grid gap-10 lg:grid-cols-[0.95fr_1.05fr]">
      <div>
        <p className="mono mb-6 inline-flex bg-accent px-2 py-1 text-[10px] uppercase tracking-[0.14em] text-white">
          Condition Types
        </p>
        <div className="space-y-5">
          {capabilities.map((item, index) => {
            const on = index === active;
            return (
              <button
                key={item.id}
                type="button"
                onClick={() => setActive(index)}
                className={`block w-full border-l-2 pl-4 text-left transition-colors ${
                  on ? "border-accent text-white" : "border-white/15 text-white/40 hover:text-white/70"
                }`}
              >
                <p className="text-xl tracking-tight">
                  {item.label}{" "}
                  <span className="mono text-sm text-accent">{`// ${item.id}`}</span>
                </p>
                <div className={`expand-panel${on ? " is-open" : ""}`}>
                  <p className="expand-panel-inner mt-2 text-sm leading-relaxed text-white/60">
                    {item.body}
                  </p>
                </div>
              </button>
            );
          })}
        </div>
      </div>
      <div className="dark-dot-grid relative min-h-[280px] border border-white/10 bg-black/40 p-6">
        <div className="mb-4 flex flex-wrap gap-3 mono text-[10px] uppercase tracking-[0.12em] text-white/40">
          <span>load 42%</span>
          <span>queue depth 3</span>
          <span>last fire 2.1s ago</span>
        </div>
        <div className="grid h-[calc(100%-1.5rem)] grid-cols-[1fr_auto_1fr] items-center gap-4">
          <div className="space-y-2 border border-white/15 bg-black/50 p-3 text-xs text-white/70">
            <p className="mono text-[10px] text-white/40">EVENTS IN</p>
            <p>type + key</p>
            <p>start / cancel</p>
            <p>sequence steps</p>
            <p>count matches</p>
            <p className="mono text-accent">+12,481 / min</p>
          </div>
          <div className="flex flex-col items-center gap-3">
            <div className="flex h-16 w-16 items-center justify-center bg-black text-2xl text-white">
              ✱
            </div>
            <div className="flex gap-1">
              {[0, 1, 2].map((i) => (
                <span key={i} className="eq-bar" aria-hidden />
              ))}
            </div>
            <p className="mono text-[10px] text-accent">eval 0.4ms</p>
          </div>
          <div className="space-y-2 border border-white/15 bg-black/50 p-3 text-xs text-white/70">
            <p className="mono text-[10px] text-white/40">OUTCOME</p>
            <p>
              ARMED <span className="text-white/40">12,847</span>
            </p>
            <p>
              CANCELLED <span className="text-white/40">9,102</span>
            </p>
            <p>
              FIRED <span className="text-block">391</span>
            </p>
            <p>
              OUTBOX <span className="text-accent">14</span>
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}

function InstallTabs() {
  const [active, setActive] = useState(0);
  return (
    <div className="overflow-hidden border border-border bg-dark text-terminal-fg">
      <div className="flex flex-wrap gap-0 border-b border-white/10">
        {installTabs.map((tab, index) => (
          <button
            key={tab.label}
            type="button"
            onClick={() => setActive(index)}
            className={`mono px-4 py-3 text-xs uppercase tracking-[0.1em] ${
              active === index ? "bg-accent text-white" : "text-white/50 hover:text-white"
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>
      <pre className="overflow-x-auto p-5 font-mono text-[13px] leading-relaxed">
        <code>{installTabs[active].code}</code>
      </pre>
    </div>
  );
}

function FaqItem({ q, a, index }: { q: string; a: string; index: number }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="border-b border-border">
      <button
        type="button"
        className="flex w-full items-center justify-between gap-4 py-5 text-left"
        onClick={() => setOpen((v) => !v)}
      >
        <span className="flex items-baseline gap-3">
          <span className="mono text-[11px] text-accent">{`//${String(index + 1).padStart(3, "0")}`}</span>
          <span className="font-medium tracking-tight">{q}</span>
        </span>
        <span className="mono text-accent">{open ? "[-]" : "[+]"}</span>
      </button>
      <div className={`expand-panel${open ? " is-open" : ""}`}>
        <p className="expand-panel-inner pb-5 text-sm leading-relaxed text-muted">{a}</p>
      </div>
    </div>
  );
}

export function HomePageContent() {
  return (
    <>
      <Hero />
      <Marquee />

      <div className="border-b border-border bg-surface">
        <div className="site-shell flex flex-wrap items-center gap-x-8 gap-y-3 py-5">
          <p className="mono text-[10px] uppercase tracking-[0.16em] text-muted">Built for</p>
          {stackMarks.map((mark) => (
            <span key={mark} className="mono text-xs text-foreground/70">
              {mark}
            </span>
          ))}
        </div>
      </div>

      <AeyeSection
        index="01"
        kicker="KEY VALUE"
        title={
          <>
            Three condition
            <br />
            primitives.
          </>
        }
      >
        <div className="grid gap-0 md:grid-cols-3">
          {valueCards.map((card, index) => (
            <Reveal
              key={card.id}
              delay={index * 0.08}
              className="feature-card border-l-0 first:border-l md:border-l-0 md:border-r-0 md:first:border-l md:last:border-r"
            >
              <div className="flex items-start justify-between">
                <WireIcon variant={(index + 1) as 1 | 2 | 3} />
                <p className="mono text-xs text-muted">{`//${card.id}`}</p>
              </div>
              <h3 className="mt-8 text-xl tracking-tight">{card.title}</h3>
              <p className="mt-3 text-sm leading-relaxed text-muted">{card.body}</p>
              <dl className="mono mt-6 space-y-2 border-t border-border pt-4 text-xs">
                {card.metrics.map(([v, l]) => (
                  <div key={l} className="flex justify-between gap-3">
                    <dt className="text-muted">{l}</dt>
                    <dd className="text-accent">{v}</dd>
                  </div>
                ))}
              </dl>
              <p className="mono mt-6 inline-block bg-surface px-2 py-1 text-[10px] uppercase tracking-[0.12em] text-muted">
                {card.tag}
              </p>
            </Reveal>
          ))}
        </div>
      </AeyeSection>

      <AeyeSection
        index="02"
        kicker="NUMBERS"
        title={
          <>
            Measured on durable
            <br />
            commits. Not marketing.
          </>
        }
        action={
          <a
            href={`${site.docs}/BENCHMARKS.md`}
            target="_blank"
            rel="noopener noreferrer"
            className="btn-secondary"
          >
            BENCHMARKS.md ↗
          </a>
        }
      >
        <p className="mb-8 max-w-2xl text-sm text-muted">
          Apple M4 · Go 1.26 · modernc SQLite · WAL ·{" "}
          <span className="mono text-foreground">synchronous=FULL</span> · single writer. Every ingest
          includes fsync Commit.
        </p>
        <div className="grid gap-0 border border-border sm:grid-cols-2 lg:grid-cols-4">
          {benches.map((b, index) => (
            <Reveal
              key={b.name}
              delay={index * 0.05}
              className="border-b border-border p-5 sm:border-r sm:[&:nth-child(2)]:border-r-0 lg:border-b-0 lg:[&:nth-child(2)]:border-r lg:last:border-r-0"
            >
              <p className="mono text-[11px] uppercase tracking-[0.12em] text-muted">{b.name}</p>
              <p className="mt-4 text-3xl tracking-tight text-accent">
                {b.rate}
                <span className="ml-1 text-sm text-muted">{b.unit}</span>
              </p>
              <div className="mt-3 flex items-center justify-between mono text-[11px]">
                <span className="text-muted">{b.note}</span>
                <span className="text-accent">{b.delta}</span>
              </div>
            </Reveal>
          ))}
        </div>

        <div className="mt-8 overflow-hidden border border-border">
          <div className="grid grid-cols-[1.4fr_1fr_1fr_1fr] border-b border-border bg-surface mono text-[10px] uppercase tracking-[0.12em] text-muted">
            <div className="px-4 py-3">Path</div>
            <div className="px-4 py-3">p50</div>
            <div className="px-4 py-3">p95</div>
            <div className="px-4 py-3">p99</div>
          </div>
          {latencies.map(([path, p50, p95, p99]) => (
            <div
              key={path}
              className="grid grid-cols-[1.4fr_1fr_1fr_1fr] border-b border-border last:border-b-0 mono text-sm"
            >
              <div className="px-4 py-3 text-muted">{path}</div>
              <div className="px-4 py-3 text-accent">{p50}</div>
              <div className="px-4 py-3">{p95}</div>
              <div className="px-4 py-3">{p99}</div>
            </div>
          ))}
        </div>

        <div className="mt-8 grid grid-cols-2 border border-border sm:grid-cols-4">
          {[
            ["−35%", "sequence allocs"],
            ["−32%", "count allocs"],
            ["−18%", "ingest0 allocs"],
            ["89", "allocs/op ingest0"],
          ].map(([value, label]) => (
            <div key={label} className="border-r border-border px-4 py-5 last:border-r-0">
              <p className="text-2xl tracking-tight text-accent">{value}</p>
              <p className="mono mt-2 text-[10px] uppercase tracking-[0.14em] text-muted">{label}</p>
            </div>
          ))}
        </div>
      </AeyeSection>

      <AeyeSection
        index="03"
        kicker="RELIABILITY"
        title={<>Crash-safe by design.</>}
        dotted
      >
        <div className="grid gap-4 lg:grid-cols-2">
          <Reveal className="feature-card">
            <div className="flex items-center justify-between">
              <h3 className="text-lg tracking-tight">Quiet day</h3>
              <span className="mono text-sm text-accent">ARMED</span>
            </div>
            <dl className="mono mt-6 space-y-2 text-sm">
              <div className="flex justify-between border-b border-border py-2">
                <dt className="text-muted">Timers live</dt>
                <dd>12,847</dd>
              </div>
              <div className="flex justify-between border-b border-border py-2">
                <dt className="text-muted">Fired / 24h</dt>
                <dd>391</dd>
              </div>
              <div className="flex justify-between border-b border-border py-2">
                <dt className="text-muted">Cancel rate</dt>
                <dd>97.0%</dd>
              </div>
              <div className="flex justify-between border-b border-border py-2">
                <dt className="text-muted">Writer</dt>
                <dd>single queue</dd>
              </div>
              <div className="flex justify-between py-2">
                <dt className="text-muted">Sync</dt>
                <dd>FULL · WAL</dd>
              </div>
            </dl>
          </Reveal>
          <Reveal delay={0.08} className="feature-card">
            <div className="flex items-center justify-between">
              <h3 className="text-lg tracking-tight">After crash</h3>
              <span className="mono text-sm text-accent">RESUMED</span>
            </div>
            <dl className="mono mt-6 space-y-2 text-sm">
              <div className="flex justify-between border-b border-border py-2">
                <dt className="text-muted">Pending restored</dt>
                <dd>5,000</dd>
              </div>
              <div className="flex justify-between border-b border-border py-2">
                <dt className="text-muted">Lost timers</dt>
                <dd className="text-accent">0</dd>
              </div>
              <div className="flex justify-between border-b border-border py-2">
                <dt className="text-muted">Outbox</dt>
                <dd>replay ready</dd>
              </div>
              <div className="flex justify-between border-b border-border py-2">
                <dt className="text-muted">Publishers stress</dt>
                <dd>×16</dd>
              </div>
              <div className="flex justify-between py-2">
                <dt className="text-muted">Rule cache</dt>
                <dd>invalidated</dd>
              </div>
            </dl>
          </Reveal>
        </div>
        <div className="mt-8 grid grid-cols-2 border border-border sm:grid-cols-4">
          {stressChecks.map(([value, label]) => (
            <div key={label} className="border-r border-border px-4 py-5 last:border-r-0">
              <p className="text-2xl tracking-tight text-accent">{value}</p>
              <p className="mono mt-2 text-[10px] uppercase tracking-[0.14em] text-muted">{label}</p>
            </div>
          ))}
        </div>
      </AeyeSection>

      <AeyeSection
        id="how"
        index="04"
        kicker="CORE CAPABILITIES"
        title={
          <>
            How a condition
            <br />
            gets evaluated.
          </>
        }
        dark
        dotted
      >
        <CapabilitySwitcher />
        <div className="mt-12 grid gap-6 border-t border-white/10 pt-8 md:grid-cols-3">
          {[
            ["Fits your stack", "REST ingest, CLI ops, Go SDK. No new broker required."],
            ["Quiet by default", "Local SQLite. No telemetry unless you add it."],
            ["Durable first", "Timers and outbox survive kill -9 and power loss."],
          ].map(([title, body]) => (
            <div key={title}>
              <h3 className="text-lg tracking-tight text-white">{title}</h3>
              <p className="mt-2 text-sm text-white/55">{body}</p>
            </div>
          ))}
        </div>
      </AeyeSection>

      <AeyeSection
        index="05"
        kicker="HOW IT WORKS"
        title={
          <>
            A short path
            <br />
            from install to fire.
          </>
        }
      >
        <div className="grid gap-0 border border-border md:grid-cols-4">
          {steps.map(([id, title, body], index) => (
            <Reveal
              key={id}
              delay={index * 0.06}
              className="border-b border-border p-5 md:border-b-0 md:border-r md:last:border-r-0"
            >
              <p className="mono text-xs text-accent">{`// ${id}`}</p>
              <p className="mt-6 text-sm leading-relaxed text-muted">{body}</p>
              <div className="my-6 h-0.5 bg-accent" />
              <h3 className="text-base font-medium text-accent">{title}</h3>
            </Reveal>
          ))}
        </div>
      </AeyeSection>

      <AeyeSection
        id="quickstart"
        index="06"
        kicker="INSTALLATION"
        title={
          <>
            Install it.
            <br />
            Arm a rule.
          </>
        }
        action={
          <a href={site.docs} target="_blank" rel="noopener noreferrer" className="btn-primary">
            <span className="inline-block h-2.5 w-2.5 bg-white" aria-hidden />
            Docs
          </a>
        }
      >
        <div className="grid gap-8 lg:grid-cols-[0.8fr_1.2fr]">
          <div className="space-y-4 text-sm leading-relaxed text-muted">
            <p>
              Default path: binary or Docker →{" "}
              <span className="mono text-foreground">waitmesh serve</span> → create an absence rule →
              ingest a start event → wait for fire or cancel.
            </p>
            <p>
              One process, one SQLite file. Point your workers at the REST API or shell into the CLI for
              ops.
            </p>
            <dl className="mono space-y-2 border border-border p-4 text-xs">
              <div className="flex justify-between">
                <dt className="text-muted">Cold start</dt>
                <dd className="text-accent">&lt;1s</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted">Binary size</dt>
                <dd>~13MB</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted">Deps at runtime</dt>
                <dd>0 (SQLite embed)</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted">Default port</dt>
                <dd>:8080</dd>
              </div>
            </dl>
          </div>
          <InstallTabs />
        </div>
      </AeyeSection>

      <AeyeSection
        id="use-cases"
        index="07"
        kicker="USE CASES"
        title={<>Where teams use it.</>}
      >
        <div className="divide-y divide-border border-y border-border">
          {useCases.map((item) => (
            <div key={item.name} className="grid gap-4 py-6 lg:grid-cols-[1.2fr_1fr] lg:items-start">
              <div>
                <div className="flex flex-wrap items-baseline gap-3">
                  <h3 className="text-lg tracking-tight">{item.name}</h3>
                  <span className="mono text-[11px] text-muted">{item.badge}</span>
                </div>
                <p className="mt-2 max-w-2xl text-sm text-muted">{item.summary}</p>
              </div>
              <div className="grid grid-cols-3 gap-2 border border-border">
                {item.stats.map(([v, l]) => (
                  <div key={l} className="border-r border-border px-3 py-3 last:border-r-0">
                    <p className="text-lg tracking-tight text-accent">{v}</p>
                    <p className="mono mt-1 text-[9px] uppercase tracking-[0.12em] text-muted">{l}</p>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </AeyeSection>

      <AeyeSection
        id="docs"
        index="08"
        kicker="DOCUMENT"
        title={
          <>
            Docs for operators
            <br />
            and integrators.
          </>
        }
      >
        <div className="grid gap-4 md:grid-cols-2">
          {[
            ["Reliability", "WAL · single writer · 5k restart", `${site.docs}/RELIABILITY.md`],
            ["Benchmarks", "p50/p95/p99 · evt/s · allocs", `${site.docs}/BENCHMARKS.md`],
            ["Security", "tokens · health · trust model", `${site.docs}/SECURITY.md`],
            ["Release readiness", "honest v0.1 checklist", `${site.docs}/RELEASE_READINESS.md`],
          ].map(([title, meta, href], index) => (
            <Reveal key={title} delay={index * 0.05}>
              <a
                href={href}
                target="_blank"
                rel="noopener noreferrer"
                className="feature-card group block hover:border-foreground"
              >
                <p className="mono text-xs text-muted">{meta}</p>
                <h3 className="mt-4 text-xl tracking-tight group-hover:text-accent">{title}</h3>
                <p className="mono mt-8 text-xs text-accent">View →</p>
              </a>
            </Reveal>
          ))}
        </div>
      </AeyeSection>

      <AeyeSection
        index="09"
        kicker="OPEN SOURCE"
        title={<>Free under Apache 2.0.</>}
        action={
          <a href={site.github} target="_blank" rel="noopener noreferrer" className="btn-primary">
            <span className="inline-block h-2.5 w-2.5 bg-white" aria-hidden />
            GitHub
          </a>
        }
      >
        <div className="grid gap-0 border border-border lg:grid-cols-3">
          {[
            ["Community", "Free", "Engine, CLI, REST, SQLite durability. Star and fork."],
            ["Self-host", "$0", "Your disk, your token, your delivery workers."],
            ["Contribute", "PRs", "Issues and patches welcome on GitHub."],
          ].map(([name, price, body], index) => (
            <Reveal
              key={name}
              delay={index * 0.06}
              className="flex h-full flex-col border-b border-border p-6 lg:border-b-0 lg:border-r lg:last:border-r-0"
            >
              <p className="mono text-xs text-accent">{name}</p>
              <p className="mt-4 min-h-[2.25rem] text-3xl leading-none tracking-tight">{price}</p>
              <p className="mt-3 min-h-[3.75rem] text-sm text-muted">{body}</p>
            </Reveal>
          ))}
        </div>
      </AeyeSection>

      <AeyeSection id="faq" index="10" kicker="FAQ" title={<>Questions.</>}>
        <div>
          {faqs.map((item, index) => (
            <FaqItem key={item.q} q={item.q} a={item.a} index={index} />
          ))}
        </div>
      </AeyeSection>

      <section className="border-b border-border bg-surface" aria-labelledby="seo-why">
        <div className="site-shell py-14 sm:py-16">
          <p className="mono text-[11px] uppercase tracking-[0.16em] text-muted">Why this exists</p>
          <h2 id="seo-why" className="mt-4 max-w-3xl text-3xl tracking-tight sm:text-4xl">
            When queues and cron miss the thing that never arrived
          </h2>
          <div className="mt-6 max-w-3xl space-y-4 text-base leading-relaxed text-muted">
            <p>
              Most systems react to events that show up. Waitmesh is an open-source{" "}
              <strong className="font-medium text-foreground">event-condition engine</strong> for the
              opposite case:{" "}
              <strong className="font-medium text-foreground">absence detection</strong>, ordered{" "}
              <strong className="font-medium text-foreground">sequence workflows</strong>, and{" "}
              <strong className="font-medium text-foreground">count thresholds</strong> — with{" "}
              <strong className="font-medium text-foreground">durable timers</strong> that survive
              crashes on SQLite WAL.
            </p>
            <p>
              Use it for payment timeouts, heartbeat monitors, onboarding funnels, and retry storms.
              Single binary, REST + CLI, Apache 2.0. Local by default — no telemetry.
            </p>
          </div>
          <ul className="mt-8 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {[
              ["/#use-cases", "Payment & heartbeat use cases"],
              ["/#how", "Absence, sequence, count"],
              ["/#quickstart", "Install & arm a rule"],
              [`${site.docs}/BENCHMARKS.md`, "Durable ingest benchmarks"],
              [`${site.docs}/RELIABILITY.md`, "Crash & restart model"],
              [site.github, "Source on GitHub"],
            ].map(([href, label]) => (
              <li key={label}>
                <a
                  href={href}
                  {...(href.startsWith("http")
                    ? { target: "_blank", rel: "noopener noreferrer" }
                    : {})}
                  className="font-medium text-accent hover:underline"
                >
                  {label}
                </a>
              </li>
            ))}
          </ul>
        </div>
      </section>

      <section className="border-b border-border bg-dark text-white">
        <div className="site-shell py-20">
          <p className="mono text-[11px] uppercase tracking-[0.16em] text-white/40">
            [N.11/11] &gt; NEXT
          </p>
          <h2 className="slash-title mt-4 text-white">
            <span className="text-accent">/</span> Arm your first absence rule.{" "}
            <span className="text-accent">/</span>
          </h2>
          <p className="mt-6 max-w-xl text-sm leading-relaxed text-white/55">
            Install the binary, create a rule, ingest one event. Know when the follow-up never comes —
            with timers that survive a crash.
          </p>
          <div className="mt-8 grid max-w-lg grid-cols-3 gap-0 border border-white/15">
            {[
              ["83µs", "p50"],
              ["0", "lost"],
              ["15k+", "evt/s"],
            ].map(([v, l]) => (
              <div key={l} className="border-r border-white/15 px-4 py-4 last:border-r-0">
                <p className="text-xl text-accent">{v}</p>
                <p className="mono mt-1 text-[10px] uppercase tracking-[0.12em] text-white/40">{l}</p>
              </div>
            ))}
          </div>
          <div className="mt-10 flex flex-wrap gap-6">
            <Link href="/#quickstart" className="btn-primary">
              <span className="inline-block h-2.5 w-2.5 bg-white" aria-hidden />
              Quickstart
            </Link>
            <a
              href={site.github}
              target="_blank"
              rel="noopener noreferrer"
              className="btn-ghost text-white"
            >
              ▷ Star on GitHub
            </a>
          </div>
        </div>
      </section>
    </>
  );
}
