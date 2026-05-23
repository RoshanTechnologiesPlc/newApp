'use client';
import { useState, useEffect } from 'react';
import Image from 'next/image';
import { useTranslation } from 'react-i18next';
import '../../lib/i18n';

export default function NewsPage() {
  const { t } = useTranslation();
  const [news, setNews] = useState([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function fetchNews() {
      try {
        // Replace with your Go API URL after deployment
        const API_URL = process.env.NEXT_PUBLIC_API_URL || 'https://fotmob-backend-go.onrender.com';
        const res = await fetch(`${API_URL}/api/news`);
        const data = await res.json();
        setNews(data);
      } catch (err) {
        console.error('Failed to fetch news:', err);
      } finally {
        setLoading(false);
      }
    }
    fetchNews();
  }, []);

  if (loading) return <div className="p-4 text-center">በመጫን ላይ...</div>;

  return (
    <div className="p-4 pb-20">
      <h2 className="text-xl font-bold mb-6 text-green-800 border-b-2 border-green-800 pb-2">
        {t('common.news')}
      </h2>

      {news.length === 0 ? (
        <div className="text-center text-gray-500 py-10">ምንም ዜና አልተገኘም</div>
      ) : (
        <div className="space-y-8">
          {news.map((item) => (
            <div key={item.id} className="bg-white rounded-xl overflow-hidden shadow-sm border border-gray-100 transition hover:shadow-md">
              {item.image_url && (
                <div className="relative w-full h-48">
                  <Image
                    src={item.image_url}
                    alt={item.title}
                    fill
                    className="object-cover"
                    unoptimized={true}
                  />
                </div>
              )}
              <div className="p-4">
                <h3 className="font-bold text-lg text-gray-900 mb-2 leading-tight">
                  {item.title}
                </h3>
                <p className="text-sm text-gray-600 line-clamp-3 mb-4">
                  {item.content}
                </p>
                <div className="flex justify-between items-center pt-3 border-t border-gray-50">
                  <span className="text-xs text-gray-400">
                    {new Date(item.created_at).toLocaleDateString('am-ET')}
                  </span>
                  <a
                    href={item.publisher_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-xs font-semibold text-green-700 hover:underline"
                  >
                    ሙሉውን አንብብ →
                  </a>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
