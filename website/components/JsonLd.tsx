import { site } from "@/lib/site";

function JsonLdScript({ data }: { data: Record<string, unknown> }) {
  return (
    <script
      type="application/ld+json"
      dangerouslySetInnerHTML={{ __html: JSON.stringify(data) }}
    />
  );
}

export function JsonLd() {
  const organization = {
    "@context": "https://schema.org",
    "@type": "Organization",
    name: site.name,
    url: site.url,
    logo: `${site.url}/brand/mark-solid.png`,
    description: site.seoDescription,
    sameAs: [site.github],
    contactPoint: [
      {
        "@type": "ContactPoint",
        email: "hello@waitmesh.com",
        contactType: "customer support",
      },
      {
        "@type": "ContactPoint",
        email: "security@waitmesh.com",
        contactType: "security",
      },
    ],
  };

  const software = {
    "@context": "https://schema.org",
    "@type": "SoftwareApplication",
    name: site.name,
    alternateName: ["waitmesh", "Wait Mesh"],
    applicationCategory: "DeveloperApplication",
    operatingSystem: "Linux, macOS, Windows, Docker",
    url: site.url,
    description: site.seoDescription,
    keywords: site.keywords.join(", "),
    featureList: [
      "Absence event conditions",
      "Sequence event conditions",
      "Count / threshold conditions",
      "Durable crash-safe timers",
      "SQLite WAL + synchronous FULL",
      "REST API and CLI",
      "Durable outbox delivery",
    ],
    offers: {
      "@type": "Offer",
      price: "0",
      priceCurrency: "USD",
      description: "Apache 2.0 open-source engine",
    },
    license: "https://www.apache.org/licenses/LICENSE-2.0",
    codeRepository: site.github,
    softwareVersion: site.version,
    downloadUrl: site.github,
  };

  const website = {
    "@context": "https://schema.org",
    "@type": "WebSite",
    name: site.name,
    url: site.url,
    description: site.seoDescription,
    keywords: site.keywords.join(", "),
    inLanguage: "en-US",
    publisher: { "@type": "Organization", name: site.name, url: site.url },
  };

  return (
    <>
      <JsonLdScript data={organization} />
      <JsonLdScript data={software} />
      <JsonLdScript data={website} />
    </>
  );
}

export function FaqJsonLd({
  faqs,
}: {
  faqs: readonly { q: string; a: string }[];
}) {
  const data = {
    "@context": "https://schema.org",
    "@type": "FAQPage",
    mainEntity: faqs.map((item) => ({
      "@type": "Question",
      name: item.q,
      acceptedAnswer: {
        "@type": "Answer",
        text: item.a,
      },
    })),
  };
  return <JsonLdScript data={data} />;
}
