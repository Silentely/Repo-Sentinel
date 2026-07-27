import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { AppProviders } from "./app/providers";
import "./styles/tokens.css";
import "./styles/globals.css";

const root = document.getElementById("root");
if (!root) {
  throw new Error("RepoSentinel root element is missing");
}

createRoot(root).render(
  <StrictMode>
    <AppProviders />
  </StrictMode>,
);
