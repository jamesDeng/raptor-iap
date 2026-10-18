import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import vueJsx from "@vitejs/plugin-vue-jsx";
import tailwindcss from "@tailwindcss/vite";
export default defineConfig({
  plugins: [vue(), vueJsx(), tailwindcss()],
  resolve: { conditions: ["source", "development", "browser"] },
  build: { outDir: "dist", emptyOutDir: true },
  server: { proxy: { "/api": "http://127.0.0.1:8873" } },
});
