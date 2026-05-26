"use client";

import { useTranslation } from "react-i18next";
import { useEffect, useState } from "react";
import Link from "next/link";

export default function News() {
  const { t } = useTranslation();
  const [news, setNews] = useState([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetch(`${process.env.NEXT_PUBLIC_API_URL || 'http://localhost:10000'}/api/news`)
      .then((res) => res.json())
      .then((data) => {
        setNews(data);
        setLoading(false);
      })
      .catch((err) => {
        console.error("Failed to fetch news:", err);
        setLoading(false);
      });
  }, []);

  return (
    <div className="pb-20 px-4">
      <h1 className="text-2xl font-bold mb-4">{t("news")}</h1>
      {loading ? (
        <div className="flex justify-center py-10">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-green-600"></div>
        </div>
      ) : (
        <div className="grid gap-4">
          {news && news.length > 0 ? (
            news.map((item) => (
              <Link href={`/news/${item.id}`} key={item.id}>
                <div className="bg-white p-4 rounded-lg shadow flex gap-4 cursor-pointer hover:shadow-md transition-shadow">
                  {item.image_url && (
                    <img
                      src={item.image_url}
                      alt={item.title}
                      className="w-24 h-24 object-cover rounded flex-shrink-0"
                      onError={(e) => e.target.style.display = 'none'}
                    />
                  )}
                  <div className="flex-1">
                    <h2 className="font-bold text-lg line-clamp-2">{item.title}</h2>
                    <p className="text-gray-500 text-sm mt-1">
                      {new Date(item.created_at).toLocaleDateString()}
                    </p>
                  </div>
                </div>
              </Link>
            ))
          ) : (
            <p className="text-center text-gray-500 py-10">No news available at the moment.</p>
          )}
        </div>
      )}
    </div>
  );
}
