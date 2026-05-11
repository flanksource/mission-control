import { describe, expect, it } from "vitest";
import { configIconCandidates, resolveConfigIconNames } from "./ConfigIcon";

describe("ConfigIcon", () => {
  it("builds parent fallback candidates from hierarchical config types", () => {
    expect(configIconCandidates("Kubernetes::Pod::Container")).toEqual([
      "Kubernetes::Pod::Container",
      "Kubernetes::Pod",
      "Kubernetes",
    ]);
  });

  it("uses the nearest parent icon when a child type has no icon", () => {
    expect(resolveConfigIconNames({ primary: "Kubernetes::Pod::Container" })).toEqual({
      primary: "Kubernetes::Pod",
      secondary: undefined,
    });
  });

  it("falls back through explicit secondary and tertiary config types", () => {
    expect(
      resolveConfigIconNames({
        primary: "not-a-real-config-type",
        secondary: "still-not-real",
        tertiary: "Azure::VM::Disk",
      }),
    ).toEqual({
      primary: "Azure::VM",
      secondary: undefined,
    });
  });
});
