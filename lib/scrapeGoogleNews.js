import Parser from "rss-parser";
import * as cheerio from "cheerio";
import he from "he";
import { htmlToText } from "html-to-text";
import pLimit from "p-limit";

const parser = new Parser({
  headers: {
    "User-Agent":
      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Safari/604.1",
  },
});

const REQUEST_HEADERS = {
  "User-Agent":
    "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Safari/604.1",
  Accept:
    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
  "Accept-Language": "am,en;q=0.9",
};

function cleanText(value = "") {
  return he
    .decode(value)
    .replace(/\u00a0/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

async function fetchHtml(url) {
  const response = await fetch(url, {
    headers: REQUEST_HEADERS,
    redirect: "follow",
  });

  if (!response.ok) {
    throw new Error(`Fetch failed ${response.status}: ${url}`);
  }

  const html = await response.text();

  return {
    html,
    finalUrl: response.url || url,
  };
}

function extractCanonicalUrl(html, fallbackUrl) {
  const $ = cheerio.load(html);

  return (
    $('link[rel="canonical"]').attr("href") ||
    $('meta[property="og:url"]').attr("content") ||
    fallbackUrl
  );
}

async function resolveGoogleNewsUrl(googleNewsUrl) {
  const { html, finalUrl } = await fetchHtml(googleNewsUrl);

  const canonical = extractCanonicalUrl(html, finalUrl);

  /**
   * Sometimes Google News redirects directly to the publisher.
   * Sometimes it stays on news.google.com.
   */
  if (!canonical.includes("news.google.com")) {
    return canonical;
  }

  if (!finalUrl.includes("news.google.com")) {
    return finalUrl;
  }

  /**
   * Fallback:
   * Google News pages may contain publisher links inside the HTML.
   */
  const $ = cheerio.load(html);

  const possibleLink = $("a")
    .map((_, el) => $(el).attr("href"))
    .get()
    .find((href) => {
      if (!href) return false;
      return (
        href.startsWith("http") &&
        !href.includes("google.com") &&
        !href.includes("gstatic.com")
      );
    });

  if (possibleLink) {
    return possibleLink;
  }

  return googleNewsUrl;
}

function extractArticleFromHtml(html, url) {
  const $ = cheerio.load(html);

  $("script, style, nav, footer, header, aside, form, iframe, noscript").remove();

  const title =
    $('meta[property="og:title"]').attr("content") ||
    $("h1").first().text() ||
    $("title").text();

  const description =
    $('meta[property="og:description"]').attr("content") ||
    $('meta[name="description"]').attr("content") ||
    "";

  const image =
    $('meta[property="og:image"]').attr("content") ||
    $('meta[name="twitter:image"]').attr("content") ||
    "";

  const publishedAt =
    $('meta[property="article:published_time"]').attr("content") ||
    $('meta[name="pubdate"]').attr("content") ||
    $("time").first().attr("datetime") ||
    "";

  const author =
    $('meta[name="author"]').attr("content") ||
    $('meta[property="article:author"]').attr("content") ||
    "";

  let articleHtml = "";

  const articleSelectors = [
    "article",
    '[role="main"]',
    ".article",
    ".article-body",
    ".story-body",
    ".entry-content",
    ".post-content",
    ".content",
    "main",
  ];

  for (const selector of articleSelectors) {
    const found = $(selector).first();
    const textLength = cleanText(found.text()).length;

    if (textLength > 400) {
      articleHtml = found.html() || "";
      break;
    }
  }

  if (!articleHtml) {
    articleHtml = $("body").html() || "";
  }

  const content = htmlToText(articleHtml, {
    wordwrap: false,
    selectors: [
      { selector: "a", options: { ignoreHref: true } },
      { selector: "img", format: "skip" },
    ],
  });

  return {
    url,
    title: cleanText(title),
    description: cleanText(description),
    image,
    author: cleanText(author),
    publishedAt,
    content: cleanText(content),
  };
}

async function scrapeOneRssItem(item) {
  const googleUrl = item.link;

  let originalUrl = googleUrl;

  try {
    originalUrl = await resolveGoogleNewsUrl(googleUrl);
  } catch (error) {
    console.warn("Could not resolve Google URL:", googleUrl, error.message);
  }

  try {
    const { html, finalUrl } = await fetchHtml(originalUrl);
    const canonicalUrl = extractCanonicalUrl(html, finalUrl);

    const article = extractArticleFromHtml(html, canonicalUrl);

    return {
      rssTitle: cleanText(item.title || ""),
      rssSnippet: cleanText(item.contentSnippet || item.content || ""),
      rssPublishedAt: item.isoDate || item.pubDate || "",
      sourceName: item.source?.title || "",
      googleNewsUrl: googleUrl,
      originalUrl: canonicalUrl,
      ...article,
    };
  } catch (error) {
    return {
      rssTitle: cleanText(item.title || ""),
      rssSnippet: cleanText(item.contentSnippet || item.content || ""),
      rssPublishedAt: item.isoDate || item.pubDate || "",
      sourceName: item.source?.title || "",
      googleNewsUrl: googleUrl,
      originalUrl,
      title: cleanText(item.title || ""),
      description: cleanText(item.contentSnippet || ""),
      image: "",
      author: "",
      publishedAt: item.isoDate || item.pubDate || "",
      content: "",
      scrapeError: error.message,
    };
  }
}

export async function scrapeGoogleNewsRss(rssUrl, options = {}) {
  const limit = options.limit || 20;
  const concurrency = options.concurrency || 3;

  const feed = await parser.parseURL(rssUrl);

  const items = feed.items.slice(0, limit);

  const limiter = pLimit(concurrency);

  const articles = await Promise.all(
    items.map((item) => limiter(() => scrapeOneRssItem(item)))
  );

  return articles;
}
