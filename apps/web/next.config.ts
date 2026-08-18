import type { NextConfig } from "next";
import path from "path";

// Server-side base URL of the Go API, used as the rewrite destination
// below.
//
// Next.js resolves rewrites at BUILD time and serialises them into
// routes-manifest.json, so this value is baked into the image and setting
// API_INTERNAL_URL on a running container does not change it. That is
// tolerable precisely because it is an *internal* address: with the
// bundled compose.yaml it is the service name `api`, which is identical
// for every self-hoster, so one published image works for all of them.
// (Contrast NEXT_PUBLIC_API_BASE_URL, which would be each deployment's own
// public hostname — that is why the browser is kept same-origin instead.)
//
// A deployment that reaches the API at some other address has to rebuild
// the web image with API_INTERNAL_URL set. `make dev` and the e2e suite set
// it before building/starting, so they get their own ports.
//
// Server Components do not go through here: lib/api.ts reads
// API_INTERNAL_URL itself, at request time.
const apiInternalUrl = process.env.API_INTERNAL_URL ?? "http://api:8080";

const nextConfig: NextConfig = {
  // Standalone output keeps the production image small.
  output: "standalone",
  reactStrictMode: true,
  // Pin the file-tracing root to this app so unrelated parent lockfiles
  // are not mistaken for the workspace root.
  outputFileTracingRoot: path.join(__dirname),

  // Proxy the API through this server so the browser only ever talks to
  // one origin. That is what makes a prebuilt image redistributable (see
  // lib/config.ts), and it also removes the need for CORS and for the
  // API port to be published at all — the bundled compose file exposes
  // only the web service. There are no route handlers under app/, so
  // these paths do not shadow anything.
  //
  // /healthz, /version and /metrics are deliberately not proxied: they are
  // operational endpoints, and leaving them off the public origin is the
  // default protection described in docs/self-hosting.md.
  async rewrites() {
    return [
      { source: "/api/:path*", destination: `${apiInternalUrl}/api/:path*` },
      { source: "/auth/:path*", destination: `${apiInternalUrl}/auth/:path*` },
      // GitLab delivers webhooks to APP_PUBLIC_URL, which is this origin —
      // the API port is not published, so the receiver has to be reachable
      // through here or inbound sync never arrives.
      {
        source: "/webhooks/:path*",
        destination: `${apiInternalUrl}/webhooks/:path*`,
      },
    ];
  },
};

export default nextConfig;
