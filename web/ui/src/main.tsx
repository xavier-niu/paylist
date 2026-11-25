import { StrictMode } from "react"
import { createRoot } from "react-dom/client"

import "./index.css"
import { App } from "./App"

// iPhones and iPads zoom into a field smaller than 16px when it's tapped, and
// stay zoomed. A maximum scale of 1 stops that; Safari still lets people
// pinch to zoom. Elsewhere it would stop pinching too, and nothing zooms on
// a tap there, so it's set only on these.
const iOS = /iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1)
if (iOS) {
  const meta = document.querySelector<HTMLMetaElement>('meta[name="viewport"]')
  if (meta && !meta.content.includes("maximum-scale")) meta.content += ", maximum-scale=1"
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>
)
