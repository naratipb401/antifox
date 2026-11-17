/** @type {import('next').NextConfig} */

// Static export: `NEXT_EXPORT=1 next build` emits a fully static tree into web/out that can be
// dropped straight into an nginx web root. Dev (`next dev`) leaves the variable unset, keeping the
// /api reverse proxy and hot reload.
const isExport = process.env.NEXT_EXPORT === "1";
// Vercel demo: whole site runs on mock data, no backend, no /api reverse proxy.
const isMock = process.env.NEXT_PUBLIC_MOCK === "1";

const nextConfig = {
  reactCompiler: true,
  // Allow dev assets (HMR) to be loaded from LAN IPs; add/remove entries as needed.
  // During dev, allow any IPv4 origin for /_next/* and HMR (immune to LAN IP changes).
  // Note: Next forbids a bare "*" for security reasons; use segment wildcards; "*.*.*.*" matches any IPv4.
  allowedDevOrigins: ["*.*.*.*"],
  compiler: {
    removeConsole: process.env.NODE_ENV === "production",
  },
  ...(isExport
    ? {
        // Pure static export: no Node runtime; images unoptimized; each route emits <route>/index.html.
        output: "export",
        images: { unoptimized: true },
        trailingSlash: true,
      }
    : isMock
      ? {
          // Vercel mock demo: no backend, no /api reverse proxy.
          images: { unoptimized: true },
        }
      : {
          // Dev: reverse-proxy /api/* to the Go backend (default :8787, override with AUTOPENTEST_API).
          async rewrites() {
            const backend = process.env.AUTOPENTEST_API ?? "http://localhost:8787";
            return [{ source: "/api/:path*", destination: `${backend}/api/:path*` }];
          },
        }),
};

export default nextConfig;
