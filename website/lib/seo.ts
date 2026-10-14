import type { Metadata } from "next";
import { site } from "@/lib/site";

export const defaultOgImage = {
  url: "/og/og.png",
  width: 1200,
  height: 630,
  alt: "Waitmesh — know when something didn't happen",
} as const;

type PageMetaInput = {
  title: string;
  description: string;
  path: string;
  absoluteTitle?: boolean;
  image?: string;
  type?: "website" | "article";
  publishedTime?: string;
  noIndex?: boolean;
};

function absoluteUrl(path: string): string {
  if (path === "/" || path === "") return site.url;
  return `${site.url}${path.startsWith("/") ? path : `/${path}`}`;
}

export function pageMetadata({
  title,
  description,
  path,
  absoluteTitle = false,
  image,
  type = "website",
  publishedTime,
  noIndex = false,
}: PageMetaInput): Metadata {
  const url = absoluteUrl(path);
  const ogTitle = absoluteTitle ? title : `${title} | Waitmesh`;
  const ogImage = image
    ? { url: image, width: 1200, height: 630, alt: ogTitle }
    : { ...defaultOgImage };

  return {
    title: absoluteTitle ? { absolute: title } : title,
    description,
    keywords: [...site.keywords],
    alternates: {
      canonical: path === "/" || path === "" ? site.url : path,
    },
    openGraph: {
      title: ogTitle,
      description,
      url,
      siteName: site.name,
      locale: "en_US",
      type,
      images: [ogImage],
      ...(publishedTime ? { publishedTime } : {}),
    },
    twitter: {
      card: "summary_large_image",
      title: ogTitle,
      description,
      images: [ogImage.url],
    },
    ...(noIndex
      ? { robots: { index: false, follow: false } }
      : {}),
  };
}
