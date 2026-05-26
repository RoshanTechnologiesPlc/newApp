'use client';
import { useState, useEffect } from 'react';
import Image from 'next/image';
import { useParams, useRouter } from 'next/navigation';

const API_URL =
  process.env.NEXT_PUBLIC_API_URL || 'https://fotmob-backend-go.onrender.com';

function sourceName(rawURL) {
  try { return new URL(rawURL).hostname.replace(/^www\./, ''); }
  catch { return 'ዜና'; }
}

function formatDate(dateStr) {
  try {
    return new Date(dateStr).toLocaleDateString('am-ET', {
      year: 'numeric', month: 'long', day: 'numeric',
    });
  } catch { return ''; }
}

function ArticleBody({ text }) {
  if (!text) return null;
  const paras = text
    .split(/\n{2,}/)
    .map((p) => p.replace(/\n/g, ' ').trim())
    .filter((p) => p.length > 0);
  if (paras.length === 0) return <p className="text-gray-700 leading-relaxed text-[15px]">{text}</p>;
  return (
    <div className="space-y-4">
      {paras.map((p, i) => (
        <p key={i} className="text-gray-700 leading-relaxed text-[15px]">{p}</p>
      ))}
    </div>
  );
}

function Skeleton() {
  return (
    <div className="animate-pulse px-4 pt-4 space-y-4">
      <div className="h-52 bg-gray-200 rounded-2xl -mx-4" />
      <div className="h-5 bg-gray-200 rounded w-4/5" />
      <div className="h-5 bg-gray-200 rounded w-3/5" />
      <div className="space-y-2 pt-4">
        {[...Array(8)].map((_, i) => (
          <div key={i} className="h-3 bg-gray-100 rounded" style={{ width: `${70 + (i % 3) * 10}%` }} />
        ))}
      </div>
    </div>
  );
}

export default function NewsDetailPage() {
  const { id } = useParams();
  const router = useRouter();
  const [article, setArticle] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  useEffect(() => {
    if (!id) return;
    const tryDirect = fetch(`${API_URL}/api/news/${id}`)
      .then((r) => { if (!r.ok) throw new Error('no direct endpoint'); return r.json(); });
    const tryList = fetch(`${API_URL}/api/news`)
      .then((r) => r.json())
      .then((list) => {
        const found = list.find((n) => String(n.id) === String(id));
        if (!found) throw new Error('not found in list');
        return found;
      });
    tryDirect
      .catch(() => tryList)
      .then((data) => setArticle(data))
      .catch(() => setError(true))
      .finally(() => setLoading(false));
  }, [id]);

  return (
    <div className="min-h-screen bg-white pb-28">
      <div className="sticky top-0 z-20 bg-white/95 backdrop-blur-sm border-b border-gray-100
                      flex items-center gap-3 px-3 py-3 shadow-sm">
        <button
          onClick={() => router.back()}
          className="p-2 rounded-full hover:bg-gray-100 active:bg-gray-200 transition-colors"
        >
          <svg className="w-5 h-5 text-gray-700" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
          </svg>
        </button>
        <span className="flex-1 text-sm font-semibold text-gray-700 truncate">
          {article ? sourceName(article.publisher_url) : 'ዜና'}
        </span>
        {article && (
          <button
            onClick={() => {
              if (navigator.share) {
                navigator.share({ title: article.title, url: article.publisher_url });
              } else {
                navigator.clipboard?.writeText(article.publisher_url);
              }
            }}
            className="p-2 rounded-full hover:bg-gray-100 active:bg-gray-200 transition-colors"
          >
            <svg className="w-5 h-5 text-gray-700" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2}
                d="M8.684 13.342C8.886 12.938 9 12.482 9 12c0-.482-.114-.938-.316-1.342m0 2.684a3 3 0 110-2.684m0 2.684l6.632 3.316m-6.632-6l6.632-3.316m0 0a3 3 0 105.367-2.684 3 3 0 00-5.367 2.684zm0 9.316a3 3 0 105.368 2.684 3 3 0 00-5.368-2.684z" />
            </svg>
          </button>
        )}
      </div>

      {loading && <Skeleton />}

      {error && !loading && (
        <div className="flex flex-col items-center justify-center py-24 px-6 text-center">
          <p className="text-5xl mb-4">📰</p>
          <p className="text-lg font-semibold text-gray-700 mb-1">ዜናው አልተገኘም</p>
          <p className="text-sm text-gray-400 mb-6">The article could not be loaded.</p>
          <button
            onClick={() => router.push('/news')}
            className="bg-green-700 text-white px-5 py-2.5 rounded-full text-sm font-semibold"
          >
            ወደ ዜና ዝርዝር ተመለስ
          </button>
        </div>
      )}

      {article && (
        <article>
          {article.image_url ? (
            <div className="relative w-full h-52 bg-gray-200">
              <Image src={article.image_url} alt={article.title} fill
                className="object-cover" unoptimized priority />
              <div className="absolute inset-0 bg-gradient-to-t from-black/50 via-transparent to-transparent" />
            </div>
          ) : (
            <div className="w-full h-20 bg-gradient-to-r from-green-700 to-green-500" />
          )}
          <div className="px-4 pt-5 pb-8">
            <div className="flex items-center gap-2 mb-4 flex-wrap">
              <span className="bg-green-100 text-green-800 text-xs font-semibold px-3 py-1 rounded-full">
                {sourceName(article.publisher_url)}
              </span>
              <span className="text-xs text-gray-400">{formatDate(article.created_at)}</span>
            </div>
            <h1 className="text-xl font-extrabold text-gray-900 leading-tight mb-4">{article.title}</h1>
            <div className="h-0.5 w-10 bg-green-600 mb-5 rounded-full" />
            {article.content && article.content.trim() ? (
              <ArticleBody text={article.content} />
            ) : (
              <div className="bg-gray-50 rounded-2xl p-4 text-center">
                <p className="text-gray-400 text-sm mb-3">ሙሉ ይዘት ለዚህ ዜና አልተገኘም።</p>
                <p className="text-gray-400 text-xs">ዋናውን ምንጭ ለማንበብ ከታቸን ያለውን ቁልፍ ይጫኑ።</p>
              </div>
            )}
            <a href={article.publisher_url} target="_blank" rel="noopener noreferrer"
              className="mt-8 flex items-center justify-center gap-2 w-full bg-green-700
                         hover:bg-green-800 active:bg-green-900 text-white font-semibold
                         text-sm py-4 rounded-2xl transition-colors shadow-lg shadow-green-700/25">
              <svg className="w-4 h-4 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2}
                  d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
              </svg>
              ዋናውን ምንጭ ክፈት — {sourceName(article.publisher_url)}
            </a>
          </div>
        </article>
      )}
    </div>
  );
}
