import type { NextConfig } from "next"

const API_URL = process.env.API_INTERNAL_URL ?? "http://localhost:8080"

const nextConfig: NextConfig = {
  output: "standalone",
  poweredByHeader: false,
  turbopack: {
    rules: {
      "*.css": { loaders: ["@tailwindcss/turbopack"], as: "*.css" },
    },
  },
  // In Docker, Caddy routes /api to the Go API before requests reach Next.js.
  // This rewrite only matters for `npm run dev` without the proxy.
  async rewrites() {
    return [{ source: "/api/:path*", destination: `${API_URL}/api/:path*` }]
  },
}

export default nextConfig
