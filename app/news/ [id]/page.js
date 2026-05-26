'use client';
import { useState, useEffect } from 'react';
import Image from 'next/image';
import { useParams, useRouter } from 'next/navigation';

const API_URL =
  process.env.NEXT_PUBLIC_API_URL || 'https://fotmob-backend-go.onrender.com';

function sourceName(rawURL) {
  try {
    return new URL(rawURL).hostname.replace(/^www\./, '');
  } catch {
    return 'ዜና';
  }
}

function formatDate(dateStr) {
  return new Date(dateStr).toLocaleDateString('am-ET', {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

/**
 * Render raw-text content as paragraphs.
 * The backend joins paragraphs with "\n\n"; we split on that and also on
 * standalone "\n" so the reading experience is clean.
 */
function ArticleBody({ text }) {
  if (!text) return null;
  const paras = text
    .split(/\n{2,}/)
    .map((p) => p.replace(/\n/g, ' ').trim())
    .filter((p) => p.length > 0);

  return (
    <div className="space-y-4 text-gray-800 leading-relaxed text-[15px]">
      {paras.map((p, i) => (
        <p key={i}>{p}</p>
      ))}
    </div>
  );
}

// ── Skeleton ──────────────────────────────────────────────────────────────────
function Skeleton() {
  return (
    <div className="animate-pulse">
      <div className="h-56 bg-gray-200 w-full" />
      <div className="p-4 space-y-3">
        <div className="h-5 bg-gray-200 rounded w-4/5" />
        <div className="h-5 bg-gray-200 rounded w-3/5" />
        <div className="h-3 bg-gray-100 rounded w-1/3 mt-4" />
        <div className="space-y-2 mt-6">
          {[...Array(6)].map((_, i) => (
            <div key={i} className="h-3 bg-gray-100 rounded" />
          ))}
        </div>
      </div>
    </div>
  );
}

// ── Page ──────────────────────────────────────────────────────────────────────
export default function NewsDetailPage() {
  const { id } = useParams();
  const router = useRouter();
  const [article, setArticle] = useState(null);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);

  useEffect(() => {
    if (!id) return;
    fetch(`${API_URL}/api/news/${id}`)
      .then((r) => {
        if (!r.ok) throw new Error('not found');
        return r.json();
      })
      .then(setArticle)
      .catch(() => setNotFound(true))
      .finally(() => setLoading(false));
  }, [id]);

  return (
    <div className="min-h-screen bg-white pb-28">
      {/* ── Sticky top bar ──────────────────────────────────────────────── */}
      <div className="sticky top-0 z-20 bg-white/90 backdrop-blur border-b border-gray-100 flex items-center gap-3 px-3 py-3 shadow-sm">
        <button
          onClick={() => router.back()}
          aria-label="ተመለስ"
          className="p-2 rounded-full hover:bg-gray-100 active:bg-gray-200 transition-colors"
        >
          <svg className="w-5 h-5 text-gray-700" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
          </svg>
        </button>
        <span className="text-sm font-semibold text-gray-700 truncate">
          {article ? sourceName(article.publisher_url) : 'ዜና'}
        </span>

        {/* Share button – uses Web Share API if available */}
        {article && (
          <button
            onClick={() => {
              if (navigator.share) {
                navigator.share({ title: article.title, url: article.publisher_url });
              } else {
                navigator.clipboard?.writeText(article.publisher_url);
              }
            }}
            aria-label="አጋራ"
            className="ml-auto p-2 rounded-full hover:bg-gray-100 active:bg-gray-200 transition-colors"
          >
            <svg className="w-5 h-5 text-gray-700" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2}
                d="M8.684 13.342C8.886 12.938 9 12.482 9 12c0-.482-.114-.938-.316-1.342m0 2.684a3 3 0 110-2.684m0 2.684l6.632 3.316m-6.632-6l6.632-3.316m0 0a3 3 0 105.367-2.684 3 3 0 00-5.367 2.684zm0 9.316a3 3 0 105.368 2.684 3 3 0 00-5.368-2.684z" />
            </svg>
          </button>
        )}
      </div>

      {/* ── Loading ──────────────────────────────────────────────────────── */}
      {loading && <Skeleton />}

      {/* ── 404 ─────────────────────────────────────────────────────────── */}
      {notFound && (
        <div className="flex flex-col items-center justify-center py-24 px-6 text-center">
          <p className="text-5xl mb-4">📰</p>
          <p className="text-lg font-semibold text-gray-700 mb-1">ዜናው አልተገኘም</p>
          <p className="text-sm text-gray-400 mb-6">This article may have been removed.</p>
          <button
            onClick={() => router.push('/news')}
            className="bg-green-700 text-white px-5 py-2 rounded-full text-sm font-semibold"
          >
            ወደ ዜና ዝርዝር
          </button>
        </div>
      )}

      {/* ── Article ──────────────────────────────────────────────────────── */}
      {article && (
        <article>
          {/* Hero image */}
          {article.image_url && (
            <div className="relative w-full h-56 bg-gray-200">
              <Image
                src={article.image_url}
                alt={article.title}
                fill
                className="object-cover"
                unoptimized
                priority
              />
              {/* Dark gradient so title can overlay safely */}
              <div className="absolute inset-0 bg-gradient-to-t from-black/60 via-transparent to-transparent" />
            </div>
          )}

          <div className="px-4 pt-5 pb-6">
            {/* Source + date meta row */}
            <div className="flex items-center gap-2 mb-3">
              <span className="bg-green-100 text-green-800 text-xs font-semibold px-2.5 py-1 rounded-full">
                {sourceName(article.publisher_url)}
              </span>
              <span className="text-xs text-gray-400">{formatDate(article.created_at)}</span>
            </div>

            {/* Title */}
            <h1 className="text-xl font-extrabold text-gray-900 leading-tight mb-5">
              {article.title}
            </h1>

            {/* Divider */}
            <div className="h-0.5 w-12 bg-green-600 mb-5 rounded-full" />

            {/* Full article body */}
            {article.content ? (
              <ArticleBody text={article.content} />
            ) : (
              <p className="text-gray-400 italic text-sm">ሙሉ ይዘት አልተገኘም።</p>
            )}

            {/* ── CTA — Read original ──────────────────────────────────── */}
            <a
              href={article.publisher_url}
              target="_blank"
              rel="noopener noreferrer"
              className="mt-8 flex items-center justify-center gap-2 w-full bg-green-700
                         hover:bg-green-800 active:bg-green-900 text-white font-semibold
                         text-sm py-3.5 rounded-2xl transition-colors shadow-md shadow-green-700/20"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
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
