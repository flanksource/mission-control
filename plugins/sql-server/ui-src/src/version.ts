// Build-time constants injected by vite via `define` (see vite.config.ts).
// The Taskfile passes PLUGIN_VERSION + PLUGIN_BUILD_DATE through to the
// vite build, which substitutes these at compile time.
declare const __PLUGIN_VERSION__: string;
declare const __PLUGIN_BUILD_DATE__: string;

export const PLUGIN_NAME = "sql-server";
export const PLUGIN_VERSION = __PLUGIN_VERSION__;
export const PLUGIN_BUILD_DATE = __PLUGIN_BUILD_DATE__;

export function logBanner(): void {
  const date = PLUGIN_BUILD_DATE ? ` (built ${PLUGIN_BUILD_DATE})` : "";
  // eslint-disable-next-line no-console
  console.info(`[${PLUGIN_NAME}] UI v${PLUGIN_VERSION}${date}`);
}
