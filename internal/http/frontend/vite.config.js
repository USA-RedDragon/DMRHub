import { fileURLToPath, URL } from "node:url";

import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import istanbul from "vite-plugin-istanbul";

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [
    vue(),
    istanbul({
      include: "src/*",
      exclude: ["node_modules"],
      requireEnv: false,
      cypress: true,
    }),
  ],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  test: {
    coverage: {
      all: true,
      enabled: true,
      provider: "c8",
      reporter: ["html", "lcov"],
      reportsDirectory: "coverage/unit",
      src: ["src"],
    },
  },
});
