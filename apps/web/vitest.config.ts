import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "url";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./vitest.setup.ts"],
    // Pinned rather than inherited: the devcontainer exports both of these,
    // and a suite that silently picks up the developer's shell would pass
    // here and fail in CI. The empty NEXT_PUBLIC_API_BASE_URL is the
    // shipped default — browser calls are same-origin (lib/config.ts).
    env: {
      NEXT_PUBLIC_API_BASE_URL: "",
      API_INTERNAL_URL: "http://localhost:8080",
    },
    include: ["**/*.test.{ts,tsx}"],
  },
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./", import.meta.url)),
    },
  },
});
