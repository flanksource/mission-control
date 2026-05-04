import { useEffect, useRef, useState } from "react";

import { pluginIframeSrc } from "./use-plugin-tabs";

export type PluginTabProps = {
  pluginName: string;
  tabPath: string;
  configId: string;
};

/**
 * PluginTab renders one plugin's iframe inside the host's tab container.
 *
 * The host expects the plugin's UI to send {type: "mc.tab.ready"} via
 * postMessage when it's done loading; until then a skeleton overlay is
 * shown. Plugins that don't send the message still render fine — the
 * skeleton just dissolves on iframe.onload.
 */
export function PluginTab({ pluginName, tabPath, configId }: PluginTabProps) {
  const [ready, setReady] = useState(false);
  const ref = useRef<HTMLIFrameElement | null>(null);

  useEffect(() => {
    function handler(e: MessageEvent) {
      if (e.source !== ref.current?.contentWindow) return;
      const msg = e.data as { type?: string };
      if (msg && msg.type === "mc.tab.ready") {
        setReady(true);
      }
    }
    window.addEventListener("message", handler);
    return () => window.removeEventListener("message", handler);
  }, []);

  return (
    <div className="relative h-[calc(100vh-220px)] min-h-[480px] w-full overflow-hidden rounded-md border border-border bg-background">
      {!ready && (
        <div className="absolute inset-0 z-10 flex items-center justify-center bg-background/70 text-sm text-muted-foreground">
          Loading {pluginName}…
        </div>
      )}
      <iframe
        ref={ref}
        src={pluginIframeSrc(pluginName, tabPath, configId)}
        title={`${pluginName} – ${tabPath}`}
        className="h-full w-full border-0"
        sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
        onLoad={() => setReady(true)}
      />
    </div>
  );
}
