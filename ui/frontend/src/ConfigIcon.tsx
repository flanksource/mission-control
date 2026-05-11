import {
  findByName,
  ResourceIcon,
  type IconType,
  type ResourceIconProps,
} from "@flanksource/icons/icon";
import { IconMap } from "@flanksource/icons/mi";

export type ConfigIconProps = Omit<ResourceIconProps, "primary" | "secondary"> & {
  primary?: string | null;
  secondary?: string | null;
  tertiary?: string | null;
  type?: string | null;
};

export function configIconCandidates(
  primary?: string | null,
  secondary?: string | null,
  tertiary?: string | null,
): string[] {
  const seen = new Set<string>();
  const candidates: string[] = [];

  for (const value of [primary, secondary, tertiary]) {
    for (const candidate of hierarchyCandidates(value)) {
      if (seen.has(candidate)) continue;
      seen.add(candidate);
      candidates.push(candidate);
    }
  }

  return candidates;
}

export function resolveConfigIconNames({
  primary,
  secondary,
  tertiary,
  type,
  iconMap,
}: Pick<ConfigIconProps, "primary" | "secondary" | "tertiary" | "type" | "iconMap">) {
  const candidates = configIconCandidates(primary ?? type, secondary, tertiary);
  const map = iconMap ?? IconMap;
  const bundled = firstBundledIcon(candidates, map);

  return {
    primary: bundled ?? candidates[0],
    secondary: bundled ? undefined : candidates[1],
  };
}

export function ConfigIcon({
  primary,
  secondary,
  tertiary,
  type,
  iconMap,
  ...props
}: ConfigIconProps) {
  const resolved = resolveConfigIconNames({ primary, secondary, tertiary, type, iconMap });

  return (
    <ResourceIcon
      primary={resolved.primary}
      secondary={resolved.secondary}
      iconMap={iconMap}
      {...props}
    />
  );
}

function hierarchyCandidates(value?: string | null): string[] {
  const trimmed = value?.trim();
  if (!trimmed) return [];

  const parts = trimmed.split("::").map((part) => part.trim()).filter(Boolean);
  if (parts.length <= 1) return [trimmed];

  const candidates: string[] = [];
  for (let index = parts.length; index > 0; index--) {
    candidates.push(parts.slice(0, index).join("::"));
  }
  return candidates;
}

function firstBundledIcon(
  candidates: string[],
  iconMap: Record<string, IconType>,
): string | undefined {
  return candidates.find((candidate) => findByName(candidate, iconMap));
}
