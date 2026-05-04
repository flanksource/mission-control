import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  ThemeProvider,
  DensityProvider,
  setFallbackIconProvider,
} from "@flanksource/clicky-ui";
// Register the <iconify-icon> custom element so clicky-ui's <Icon> renders.
import "iconify-icon";
import "@flanksource/clicky-ui/styles.css";
import "./index.css";
import { App } from "./App";
import { ConfigIcon } from "./ConfigIcon";

setFallbackIconProvider(({ name, className, size, alt }) => {
  if (!name) return null;
  if (name.includes(":") && !name.includes("::")) {
    return React.createElement("iconify-icon", {
      icon: name,
      className,
      title: alt,
    });
  }
  if (!name.includes("::")) return null;
  const iconClassName = className?.match(/\bh-\d|\bh-\[/)
    ? className
    : [className, "h-4 max-w-4"].filter(Boolean).join(" ");
  return (
    <ConfigIcon
      primary={name}
      className={iconClassName}
      size={size ?? 16}
      alt={alt}
    />
  );
});

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 0, staleTime: 30_000 } },
});

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ThemeProvider>
      <DensityProvider>
        <QueryClientProvider client={queryClient}>
          <App />
        </QueryClientProvider>
      </DensityProvider>
    </ThemeProvider>
  </React.StrictMode>,
);
