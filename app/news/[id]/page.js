"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { useTranslation } from "react-i18next";
import { ArrowLeftIcon } from "@heroicons/react/24/outline";

export default function NewsDetail() {
  const { id } = useParams();
  const router = useRouter();
  const { t } = useTranslation();
  const [item, setItem] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!id) return;
    fetch(`${process.env.NEXT_PUBLIC_API_URL || 'http://localhost:10000'}/api/news/${id}`)
      .then((res) => {
        if (!res.ok) throw new Error("News not found");
        return res.json();
      })
      .then((data) => {
        setItem(data);
        setLoading(false);
      })
      .catch((err) => {
        console.error("Failed to fetch news detail:", err);
        setLoading(false);
      });
  }, [id]);

  if (loading) {
    return (
      <div className="flex justify-center py-20">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-green-600"></div>
      </div>
    );
  }

  if (!item) {
    return (
      <div className="p-8 text-center">
        <p className="text-gray-500 mb-4">News item not found.</p>
        <button
          onClick={() => router.back()}
          className="text-green-600 font-bold"
        >
          Go Back
        </button>
      </div>
    );
  }

  return (
    <div className="pb-20 px-4 max-w-2xl mx-auto">
      <button
        onClick={() => router.back()}
        className="flex items-center gap-2 text-gray-600 mb-6 hover:text-green-600"
      >
        <ArrowLeftIcon className="h-5 w-5" />
        <span>Back</span>
      </button>

      <article>
        <h1 className="text-2xl font-bold mb-4">{item.title}</h1>
        <div className="text-gray-500 text-sm mb-6">
          {new Date(item.created_at).toLocaleDateString()}
          {item.publisher_url && (
            <a
              href={item.publisher_url}
              target="_blank"
              rel="noopener noreferrer"
              className="ml-4 text-green-600 hover:underline"
            >
              Source
            </a>
          )}
        </div>

        {item.image_url && (
          <img
            src={item.image_url}
            alt={item.title}
            className="w-full h-auto rounded-lg mb-6 shadow-sm"
            onError={(e) => e.target.style.display = 'none'}
          />
        )}

        <div className="prose prose-green max-w-none whitespace-pre-wrap leading-relaxed text-gray-800">
          {item.content || "No content available."}
        </div>
      </article>
    </div>
  );
}
