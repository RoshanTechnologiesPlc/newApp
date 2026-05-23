/** @type {import('next').NextConfig} */
const nextConfig = {
  // Allow all hosts to support the secure proxy
  experimental: {
    // In newer Next.js versions, this might be needed or sufficient
    // but typically next dev/start handles host headers unless specifically restricted.
  },
};

module.exports = nextConfig;
