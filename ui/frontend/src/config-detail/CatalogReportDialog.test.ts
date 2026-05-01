import { describe, expect, it } from "vitest";
import {
  buildReportRequest,
  catalogReportRoot,
  collectNodeIDs,
  flattenSelectedIDs,
  progressLabel,
  toReportDialogError,
  type ReportOptions,
} from "./CatalogReportDialog";
import { CatalogReportError } from "../api/configs";
import type { ConfigRelationshipTreeNode } from "../api/types";

describe("CatalogReportDialog helpers", () => {
  it("adds report roots without children unless explicitly expanded", () => {
    expect(catalogReportRoot("root")).toEqual({ id: "root", includeChildren: false });
    expect(catalogReportRoot("root", true)).toEqual({ id: "root", includeChildren: true });
  });

  it("maps all report options into the API payload", () => {
    const options: ReportOptions = {
      format: "facet-html",
      title: "Billing",
      since: "7d",
      recursive: true,
      groupBy: "config",
      changeArtifacts: true,
      audit: true,
      expandGroups: true,
      limit: 25,
      maxItems: 10,
      maxChanges: 20,
      maxItemArtifacts: 2,
      staleDays: 90,
      reviewOverdueDays: 30,
      filters: [],
      filtersText: "type!=Kubernetes::Secret\nhealth=warning\n\n",
      changes: true,
      insights: false,
      relationships: true,
      access: false,
      accessLogs: true,
      configJSON: true,
    };

    expect(buildReportRequest(options, ["root", "child"])).toEqual({
      format: "facet-html",
      selectedIds: ["root", "child"],
      title: "Billing",
      since: "7d",
      recursive: true,
      groupBy: "config",
      changeArtifacts: true,
      audit: true,
      expandGroups: true,
      limit: 25,
      maxItems: 10,
      maxChanges: 20,
      maxItemArtifacts: 2,
      staleDays: 90,
      reviewOverdueDays: 30,
      filters: ["type!=Kubernetes::Secret", "health=warning"],
      changes: true,
      insights: false,
      relationships: true,
      access: false,
      accessLogs: true,
      configJSON: true,
    });
  });

  it("collects and flattens selected tree ids in display order", () => {
    const tree: ConfigRelationshipTreeNode[] = [
      node("root", [node("child-a", [node("grandchild")]), node("child-b")]),
      node("other"),
    ];

    expect(collectNodeIDs(tree[0])).toEqual(["root", "child-a", "grandchild", "child-b"]);
    expect(flattenSelectedIDs(tree, new Set(["root", "grandchild", "other"]))).toEqual([
      "root",
      "grandchild",
      "other",
    ]);
  });

  it("converts structured report failures for the error dialog", () => {
    const err = new CatalogReportError(
      500,
      "Internal Server Error",
      '{"error":"failed to render catalog report","trace":"trace-id"}',
      {
        error: "failed to render catalog report",
        trace: "trace-id",
        time: "2026-04-27T09:38:10.308785Z",
        context: { user: "Admin" },
        stacktrace: "Oops: failed",
      },
    );

    expect(toReportDialogError(err)).toEqual({
      message: "failed to render catalog report",
      status: 500,
      statusText: "Internal Server Error",
      rawBody: '{"error":"failed to render catalog report","trace":"trace-id"}',
      trace: "trace-id",
      time: "2026-04-27T09:38:10.308785Z",
      context: { user: "Admin" },
      stacktrace: "Oops: failed",
    });
  });

  it("describes report generation progress", () => {
    expect(progressLabel(null)).toBe("Rendering report");
    expect(progressLabel({ stage: "rendering" })).toBe("Rendering report");
    expect(progressLabel({ stage: "downloading" })).toBe("Downloading report");
    expect(progressLabel({ stage: "downloading", loaded: 512, total: 1024 })).toBe(
      "Downloading report (512 B of 1 KB)",
    );
  });
});

function node(id: string, children: ConfigRelationshipTreeNode[] = []): ConfigRelationshipTreeNode {
  return {
    id,
    name: id,
    children,
  };
}
