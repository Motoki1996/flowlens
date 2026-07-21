import type { NextConfig } from "next";
import path from "path";

const nextConfig: NextConfig = {
  // Standalone output keeps the production image small (used in later
  // Azure Container Apps deployment).
  output: "standalone",
  reactStrictMode: true,
  // Pin the file-tracing root to this app so unrelated parent lockfiles
  // are not mistaken for the workspace root.
  outputFileTracingRoot: path.join(__dirname),
};

export default nextConfig;
