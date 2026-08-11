import type { NextConfig } from "next";

const apiOrigin = process.env.RUNWAY_API_URL ?? "http://127.0.0.1:8080";

const nextConfig: NextConfig = {
  agentRules: false,
  turbopack: {
    root: process.cwd(),
  },
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${apiOrigin}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;
