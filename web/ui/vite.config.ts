import { mkdirSync, readdirSync, rmSync, statSync } from "node:fs"
import { join, resolve } from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig, type Plugin } from "vite"

// The Go server embeds ../dist (web/embed.go). Its .gitkeep stays, so the
// embed compiles before the first build.
const OUT = resolve(import.meta.dirname, "../dist")
const KEEP = ".gitkeep"

function files(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name)
    return statSync(path).isDirectory() ? files(path) : [path]
  })
}

// Empties ../dist but keeps .gitkeep.
function cleanOut(): Plugin {
  return {
    name: "paylist-clean",
    apply: "build",
    buildStart() {
      mkdirSync(OUT, { recursive: true })
      for (const path of files(OUT)) if (!path.endsWith(KEEP)) rmSync(path)
    },
  }
}

// `npm run dev` serves the app with hot reload; the API comes from a Paylist
// server on :8080 (`make demo`).
const API = process.env.PAYLIST_API || "http://127.0.0.1:8080"

export default defineConfig({
  base: "./",
  plugins: [react(), tailwindcss(), cleanOut()],
  resolve: {
    alias: {
      "@": resolve(import.meta.dirname, "./src"),
    },
  },
  build: {
    outDir: OUT,
    emptyOutDir: false, // done by the plugin, keeping .gitkeep
    assetsDir: "assets",
    chunkSizeWarningLimit: 900,
  },
  server: {
    proxy: {
      "/api": API,
      "/mcp": API,
    },
  },
})
