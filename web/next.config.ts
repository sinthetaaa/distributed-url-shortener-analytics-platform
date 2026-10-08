import type { NextConfig } from "next";

const apiOrigin =
  process.env.SHORTSCALE_API_ORIGIN ??
  "http://localhost:18080";

const nextConfig: NextConfig = {
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
