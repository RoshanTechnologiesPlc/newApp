import { scrapeGoogleNewsRss } from "@/lib/scrapeGoogleNews";

export async function POST(req) {
  try {
    const body = await req.json();

    const rssUrl = body.rssUrl;
    const limit = body.limit || 20;

    if (!rssUrl) {
      return Response.json(
        { success: false, message: "rssUrl is required" },
        { status: 400 }
      );
    }

    const articles = await scrapeGoogleNewsRss(rssUrl, {
      limit,
      concurrency: 3,
    });

    return Response.json({
      success: true,
      count: articles.length,
      articles,
    });
  } catch (error) {
    return Response.json(
      {
        success: false,
        message: error.message,
      },
      { status: 500 }
    );
  }
}
