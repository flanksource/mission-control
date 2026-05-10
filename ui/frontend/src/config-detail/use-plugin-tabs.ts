import { useEffect, useState } from "react";

export type PluginTabSpec = {
  name: string;
  icon?: string;
  path: string;
  scope?: string;
};

export type PluginListing = {
  name: string;
  description?: string;
  version?: string;
  tabs?: PluginTabSpec[];
  operations?: unknown[];
};

/**
 * usePluginTabs fetches the list of plugins applicable to a given config
 * item from the host's GET /api/plugins endpoint. Each plugin contributes
 * zero or more tabs that we render as iframes pointing at the host's
 * plugin UI proxy.
 *
 * Returns the listings, loading state, and error so the caller can render
 * skeletons / fallbacks. The hook is intentionally tiny — keeping it
 * separate from ConfigItemDetail.tsx avoids inflating that already-large
 * component.
 */
export function usePluginTabs(configId: string): {
  plugins: PluginListing[];
  loading: boolean;
  error: unknown;
} {
  const [plugins, setPlugins] = useState<PluginListing[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    fetch(`/api/plugins?config_id=${encodeURIComponent(configId)}`, {
      credentials: "same-origin",
    })
      .then((res) => {
        if (!res.ok) {
          throw new Error(`/api/plugins ${res.status}`);
        }
        return res.json();
      })
      .then((data) => {
        if (!cancelled) {
          setPlugins(Array.isArray(data) ? data : []);
        }
      })
      .catch((err) => {
        if (!cancelled) setError(err);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [configId]);

  return { plugins, loading, error };
}

/**
 * pluginTabKey returns the synthetic TabKey we use to identify a specific
 * plugin's tab in the parent component's activeTab state.
 */
export function pluginTabKey(pluginName: string, tabName: string): string {
  return `plugin:${pluginName}:${tabName}`;
}

/**
 * pluginIframeSrc constructs the iframe src for a plugin tab. The plugin's
 * Go backend serves both the static UI and any /api/* routes on a single
 * port; the host reverse-proxies under /api/plugins/<name>/ui. config_id
 * is forwarded as a query param so the plugin can pick it up without
 * cross-origin postMessage handshakes.
 */
export function pluginIframeSrc(pluginName: string, path: string, configId: string): string {
  const sep = path.startsWith("/") ? "" : "/";
  return `/api/plugins/${encodeURIComponent(pluginName)}/ui${sep}${path}?config_id=${encodeURIComponent(configId)}`;
}
