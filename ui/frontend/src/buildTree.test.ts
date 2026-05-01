import { describe, expect, it } from "vitest";
import { buildTypeTree } from "./buildTree";

describe("buildTypeTree", () => {
  it("groups entries by `::` into nested nodes", () => {
    const tree = buildTypeTree([
      { type: "AWS::EC2::Instance", count: 3 },
      { type: "AWS::EC2::Volume", count: 2 },
      { type: "AWS::S3::Bucket", count: 5 },
      { type: "Kubernetes::Pod", count: 7 },
    ]);

    expect(tree.map((n) => n.label)).toEqual(["AWS", "Kubernetes / Pod"]);
    expect(tree[0].children.map((n) => n.label)).toEqual(["EC2", "S3 / Bucket"]);
    expect(tree[0].children[0].children.map((n) => n.label)).toEqual([
      "Instance",
      "Volume",
    ]);
  });

  it("aggregates subtree counts", () => {
    const tree = buildTypeTree([
      { type: "AWS::EC2::Instance", count: 3 },
      { type: "AWS::EC2::Volume", count: 2 },
      { type: "AWS::S3::Bucket", count: 5 },
    ]);

    expect(tree[0].count).toBe(10);              // AWS
    expect(tree[0].children[0].count).toBe(5);   // AWS::EC2
    expect(tree[0].children[0].children[0].count).toBe(3); // AWS::EC2::Instance
  });

  it("collapses single folder-only edges into the visible node", () => {
    const tree = buildTypeTree([{ type: "AWS::EC2::Instance", count: 1 }]);

    expect(tree).toHaveLength(1);
    expect(tree[0].label).toBe("AWS / EC2 / Instance");
    expect(tree[0].fullType).toBe("AWS::EC2::Instance");
    expect(tree[0].typePath).toBe("AWS::EC2::Instance");
    expect(tree[0].parentTypePath).toBe("AWS::EC2");
    expect(tree[0].children).toEqual([]);
  });

  it("handles flat (single-segment) types", () => {
    const tree = buildTypeTree([{ type: "Host", count: 2 }]);

    expect(tree).toHaveLength(1);
    expect(tree[0].label).toBe("Host");
    expect(tree[0].fullType).toBe("Host");
    expect(tree[0].children).toEqual([]);
  });

  it("skips empty type strings", () => {
    const tree = buildTypeTree([
      { type: "", count: 5 },
      { type: "AWS::EC2::Instance", count: 1 },
    ]);

    expect(tree).toHaveLength(1);
    expect(tree[0].label).toBe("AWS / EC2 / Instance");
  });
});
