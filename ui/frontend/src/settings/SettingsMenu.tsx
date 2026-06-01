// ABOUTME: Sidebar settings menu with theme, density and old-UI switch actions.
// ABOUTME: Switching to old UI deletes the new-UI opt-in cookie and reloads root.
import { useState } from "react";
import { Icon, IconMenuPicker, useDensity, useTheme, type IconMenuOption } from "@flanksource/clicky-ui";
import {
  UiDesktop,
  UiListDashes,
  UiListFlat,
  UiMoon,
  UiRows,
  UiSun,
} from "@flanksource/clicky-ui/icons";
import type { Density, Theme } from "@flanksource/clicky-ui/hooks";
import { deleteCookie, getCookie } from "../cookies";

const NEW_UI_COOKIE = "flanksource_use_new_ui";

const THEME_OPTIONS: IconMenuOption<Theme>[] = [
  { value: "light", icon: UiSun, label: "Light" },
  { value: "dark", icon: UiMoon, label: "Dark" },
  { value: "system", icon: UiDesktop, label: "System" },
];

const DENSITY_OPTIONS: IconMenuOption<Density>[] = [
  { value: "compact", icon: UiRows, label: "Compact" },
  { value: "comfortable", icon: UiListFlat, label: "Comfortable" },
  { value: "spacious", icon: UiListDashes, label: "Spacious" },
];

const menuClassName = "top-auto bottom-[calc(100%+0.375rem)]";

export function SettingsMenu() {
  const [open, setOpen] = useState(false);
  const { theme, setTheme } = useTheme();
  const { density, setDensity } = useDensity();

  const switchToOldUI = () => {
    if (getCookie(NEW_UI_COOKIE)) {
      deleteCookie(NEW_UI_COOKIE);
    }
    window.location.href = "/";
  };

  return (
    <div className="relative">
      {open && (
        <>
          <div className="fixed inset-0 z-10" onClick={() => setOpen(false)} />
          <div className="absolute bottom-full left-0 z-20 mb-1 flex w-full flex-col rounded-md border border-border bg-popover p-1 shadow-md">
            <IconMenuPicker<Theme>
              value={theme}
              onChange={setTheme}
              options={THEME_OPTIONS}
              ariaLabel="Theme"
              showLabel
              menuClassName={menuClassName}
            />
            <IconMenuPicker<Density>
              value={density}
              onChange={setDensity}
              options={DENSITY_OPTIONS}
              ariaLabel="Density"
              showLabel
              menuClassName={menuClassName}
            />
            <button
              type="button"
              onClick={switchToOldUI}
              className="flex w-full items-center gap-2 rounded px-control-px py-2 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
            >
              <Icon name="lucide:arrow-left-right" className="h-4 w-4 shrink-0 text-foreground" />
              <span className="min-w-0 flex-1 truncate text-left">Switch to old UI</span>
            </button>
          </div>
        </>
      )}
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-sm font-medium text-foreground/80 transition-colors hover:bg-accent hover:text-accent-foreground"
      >
        <Icon name="lucide:settings" className="h-4 w-4 shrink-0 text-muted-foreground" />
        <span>Settings</span>
      </button>
    </div>
  );
}
