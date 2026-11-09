import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "e2e",
  outputDir: "e2e/out",
  snapshotPathTemplate: "e2e/shots/{arg}{ext}",
  use: { baseURL: "http://127.0.0.1:4173", viewport: { width: 420, height: 720 }, deviceScaleFactor: 2, reducedMotion: "reduce" },
  webServer: {
    command: "pnpm exec vite build --mode mock && pnpm exec vite preview --outDir dist-mock --port 4173 --strictPort --host 127.0.0.1",
    url: "http://127.0.0.1:4173",
    reuseExistingServer: !process.env.CI,
  },
});
