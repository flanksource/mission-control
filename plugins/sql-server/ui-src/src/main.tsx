import { render } from "preact";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ThemeProvider, DensityProvider } from "@flanksource/clicky-ui";
import { App } from "./App";
import { logBanner } from "./version";
import "./styles.css";

logBanner();

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
