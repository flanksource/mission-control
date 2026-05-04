import { render } from "preact";
import { ThemeProvider, DensityProvider } from "@flanksource/clicky-ui";
import { LogsApp } from "./LogsApp";
import { logBanner } from "./version";
import "./styles.css";

logBanner();

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");

render(
  <ThemeProvider>
    <DensityProvider>
      <LogsApp />
    </DensityProvider>
  </ThemeProvider>,
  root,
);
