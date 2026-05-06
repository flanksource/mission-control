import { render } from "preact";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { DensityProvider, ThemeProvider } from "@flanksource/clicky-ui";
import { App } from "./App";
import "./styles.css";

declare const __PLUGIN_VERSION__: string;
declare const __PLUGIN_BUILD_DATE__: string;

console.info(`golang plugin ui version=${__PLUGIN_VERSION__} built=${__PLUGIN_BUILD_DATE__}`);

const qc = new QueryClient({
  defaultOptions: { queries: { retry: 1, staleTime: 5_000 } },
});

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");

render(
  <ThemeProvider>
    <DensityProvider>
    <QueryClientProvider client={qc}>
      <App />
    </QueryClientProvider>
    </DensityProvider>
  </ThemeProvider>,
  root,
);
