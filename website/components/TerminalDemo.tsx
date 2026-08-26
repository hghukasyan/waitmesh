export function TerminalDemo() {
  return (
    <div className="reveal reveal-y-24 overflow-hidden border border-border bg-dark text-terminal-fg">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/10 px-4 py-3">
        <span className="mono text-xs text-terminal-muted">waitmesh · production · pid 18421</span>
        <span className="mono text-[11px] text-accent">absence · payment.timeout · T+14m58s</span>
      </div>
      <div className="grid border-b border-white/10 sm:grid-cols-4">
        {[
          ["armed", "12,847"],
          ["fired/24h", "391"],
          ["outbox", "14"],
          ["p50 ingest", "83µs"],
        ].map(([k, v]) => (
          <div
            key={k}
            className="border-b border-white/10 px-4 py-2.5 last:border-b-0 sm:border-b-0 sm:border-r sm:last:border-r-0"
          >
            <p className="mono text-[10px] uppercase tracking-[0.12em] text-terminal-muted">{k}</p>
            <p className="mono mt-0.5 text-sm text-white">{v}</p>
          </div>
        ))}
      </div>
      <pre className="overflow-x-auto p-5 font-mono text-[13px] leading-relaxed">
        <code>
          <span className="text-accent">$</span>
          {" waitmesh rules create \\\n"}
          {"    --type absence \\\n"}
          {"    --name payment.timeout \\\n"}
          {"    --start payment.initiated \\\n"}
          {"    --cancel payment.completed \\\n"}
          {"    --within 15m\n\n"}
          <span className="text-accent">$</span>
          {" waitmesh events ingest \\\n"}
          {"    --type payment.initiated \\\n"}
          {"    --key order-9041\n\n"}
          {"# 15m later — no payment.completed\n\n"}
          <span className="text-block">FIRED</span>
          {"  payment.timeout\n"}
          {"key:     order-9041\n"}
          {"after:   "}
          <span className="text-warn">15m00.012s</span>
          {"\n"}
          {"deadline: 2026-10-23T23:04:12.004Z\n"}
          {"outbox:  "}
          <span className="text-accent">queued #48291</span>
          {"\n"}
          {"latency: "}
          <span className="text-accent">0.4ms claim → fire</span>
        </code>
      </pre>
    </div>
  );
}
