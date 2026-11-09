import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// `--mode mock` builds the same UI against lib/mock.ts so Playwright can drive
// every state without a helper. Production builds never include the mock.
export default defineConfig(({ mode }) => ({
  plugins: [svelte()],
  build: {
    outDir: mode === "mock" ? "dist-mock" : "dist",
    target: "esnext",
    assetsInlineLimit: 0,
  },
  test: { include: ["src/**/*.test.ts"] },
}));
