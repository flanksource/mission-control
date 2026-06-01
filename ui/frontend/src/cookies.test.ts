// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { deleteCookie, getCookie } from "./cookies";

describe("cookies", () => {
  afterEach(() => {
    deleteCookie("flanksource_use_new_ui");
  });

  it("reads a cookie value by name", () => {
    document.cookie = "flanksource_use_new_ui=true; path=/";
    expect(getCookie("flanksource_use_new_ui")).toBe("true");
  });

  it("returns undefined when the cookie is absent", () => {
    expect(getCookie("missing")).toBeUndefined();
  });

  it("deletes a cookie that is set", () => {
    document.cookie = "flanksource_use_new_ui=true; path=/";
    deleteCookie("flanksource_use_new_ui");
    expect(getCookie("flanksource_use_new_ui")).toBeUndefined();
  });
});
