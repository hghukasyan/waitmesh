import type { Metadata } from "next";
import { HomePageContent } from "@/components/HomePageContent";
import { FaqJsonLd } from "@/components/JsonLd";
import { pageMetadata } from "@/lib/seo";
import { site } from "@/lib/site";

export const metadata: Metadata = pageMetadata({
  title: site.seoTitle,
  description: site.seoDescription,
  path: "/",
  absoluteTitle: true,
});

const homeFaqs = [
  {
    q: "How is Waitmesh different from a cron or queue delay?",
    a: "Cron fires on a schedule. Queues delay a known job. Waitmesh watches for missing or ordered events and only fires when the condition is true — including when nothing arrives.",
  },
  {
    q: "Does Waitmesh survive process crashes?",
    a: "Yes. Timers and outbox rows live in SQLite with WAL and synchronous=FULL. Stress tests reopen a 5,000-pending backlog and drain with zero lost deadlines.",
  },
  {
    q: "What are Waitmesh event conditions?",
    a: "Three primitives: absence (fire when a follow-up never comes), sequence (ordered steps with deadlines), and count (threshold of matching events in a window).",
  },
  {
    q: "Is Waitmesh open source?",
    a: "Yes. Apache 2.0. Single binary, REST, CLI, and Go SDK. Local by default with no telemetry.",
  },
] as const;

export default function HomePage() {
  return (
    <>
      <FaqJsonLd faqs={homeFaqs} />
      <HomePageContent />
    </>
  );
}
