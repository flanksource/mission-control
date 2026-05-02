import { afterEach, describe, expect, it, vi } from "vitest";
import {
  buildPlaybookRunsByIdPath,
  buildPlaybookRunsPath,
  deletePlaybook,
  getPlaybookRunWithActions,
  getPlaybooks,
  isFinalPlaybookRunStatus,
  mergePlaybookRunSpecActions,
  updatePlaybook,
} from "./playbooks";

describe("playbook API helpers", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("loads the playbook catalogue from the backend playbook list API", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify([
      { id: "pb-1", name: "Restart pods", category: "Kubernetes" },
    ]), { status: 200, headers: { "Content-Type": "application/json" } }));

    await expect(getPlaybooks()).resolves.toEqual([
      { id: "pb-1", name: "Restart pods", category: "Kubernetes" },
    ]);
    expect(fetchMock).toHaveBeenCalledWith("/playbook/list", expect.any(Object));
  });

  it("updates playbooks through PostgREST", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify([
      { id: "pb-1", name: "Restart pods", namespace: "default" },
    ]), { status: 200, headers: { "Content-Type": "application/json" } }));

    await expect(updatePlaybook({
      id: "pb-1",
      namespace: "default",
      name: "Restart pods",
      title: "Restart pods",
      spec: { actions: [] },
    })).resolves.toEqual([
      { id: "pb-1", name: "Restart pods", namespace: "default" },
    ]);
    expect(fetchMock).toHaveBeenCalledWith("/db/playbooks?id=eq.pb-1", expect.objectContaining({
      method: "PATCH",
      body: JSON.stringify({
        namespace: "default",
        name: "Restart pods",
        title: "Restart pods",
        spec: { actions: [] },
      }),
    }));
  });

  it("soft deletes playbooks through PostgREST", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify([]), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }));

    await expect(deletePlaybook("pb-1")).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith("/db/playbooks?id=eq.pb-1", expect.objectContaining({
      method: "PATCH",
      body: expect.stringContaining("deleted_at"),
    }));
  });

  it("builds the default run list path", () => {
    expect(buildPlaybookRunsPath()).toBe(
      [
        "/db/playbook_runs",
        "?select=*,playbooks(id,name,title,icon,category),component:components(id,name,icon),check:checks(id,name,icon),config:config_items(id,name,type,config_class)",
        "&parent_id=is.null",
        "&order=created_at.desc",
        "&limit=50",
      ].join(""),
    );
  });

  it("adds run list filters", () => {
    expect(buildPlaybookRunsPath({
      configId: "cfg-1",
      playbookId: "pb-1",
      status: "pending_approval",
      limit: 25,
      offset: 50,
    })).toContain("config_id=eq.cfg-1&playbook_id=eq.pb-1&status=eq.pending_approval");
    expect(buildPlaybookRunsPath({ limit: 25, offset: 50 })).toContain("limit=25&offset=50");
  });

  it("builds a run detail path including child runs", () => {
    expect(buildPlaybookRunsByIdPath("run-1")).toContain("or=(id.eq.run-1,parent_id.eq.run-1)");
  });

  it("does not treat a child run as the requested run detail", async () => {
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(new Response(JSON.stringify([
        { id: "child-1", playbook_id: "pb-1", parent_id: "run-1", status: "completed" },
      ]), { status: 200, headers: { "Content-Type": "application/json" } }))
      .mockResolvedValueOnce(new Response(JSON.stringify([]), { status: 200, headers: { "Content-Type": "application/json" } }))
      .mockResolvedValueOnce(new Response(JSON.stringify([]), { status: 200, headers: { "Content-Type": "application/json" } }));

    await expect(getPlaybookRunWithActions("run-1")).resolves.toEqual({
      run: null,
      childRuns: [
        { id: "child-1", playbook_id: "pb-1", parent_id: "run-1", status: "completed" },
      ],
      actions: [],
    });
  });

  it("adds display actions for future spec steps without action rows", async () => {
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(new Response(JSON.stringify([
        {
          id: "run-1",
          playbook_id: "pb-1",
          status: "running",
          spec: {
            actions: [
              { name: "Create RoleBinding", exec: { script: "kubectl apply -f rolebinding.yaml" } },
              { name: "Clear RoleBinding", if: "success()", delay: "5m", exec: { script: "kubectl delete rolebinding demo" } },
            ],
          },
        },
      ]), { status: 200, headers: { "Content-Type": "application/json" } }))
      .mockResolvedValueOnce(new Response(JSON.stringify([
        {
          id: "action-1",
          name: "Create RoleBinding",
          playbook_run_id: "run-1",
          status: "completed",
          start_time: "2026-04-27T10:00:03Z",
        },
      ]), { status: 200, headers: { "Content-Type": "application/json" } }))
      .mockResolvedValueOnce(new Response(JSON.stringify([]), { status: 200, headers: { "Content-Type": "application/json" } }));

    await expect(getPlaybookRunWithActions("run-1")).resolves.toMatchObject({
      actions: [
        {
          id: "action-1",
          name: "Create RoleBinding",
          playbook_run_id: "run-1",
          status: "completed",
        },
        {
          id: "spec:run-1:1",
          name: "Clear RoleBinding",
          playbook_run_id: "run-1",
          synthetic: true,
          spec_index: 1,
        },
      ],
    });
  });

  it("merges spec actions in declaration order and keeps extra real action rows", () => {
    expect(mergePlaybookRunSpecActions([
      {
        id: "run-1",
        playbook_id: "pb-1",
        spec: {
          actions: [
            { name: "First" },
            { name: "Second" },
          ],
        },
      },
    ], [
      { id: "action-2", name: "Second", playbook_run_id: "run-1", status: "completed" },
      { id: "retry-1", name: "Second", playbook_run_id: "run-1", status: "failed", retry_count: 1 },
    ])).toEqual([
      {
        id: "spec:run-1:0",
        name: "First",
        playbook_run_id: "run-1",
        synthetic: true,
        spec_index: 0,
      },
      { id: "action-2", name: "Second", playbook_run_id: "run-1", status: "completed" },
      { id: "retry-1", name: "Second", playbook_run_id: "run-1", status: "failed", retry_count: 1 },
    ]);
  });

  it("merges direct action details into rpc action rows", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(new Response(JSON.stringify([
        { id: "run-1", playbook_id: "pb-1", status: "failed" },
      ]), { status: 200, headers: { "Content-Type": "application/json" } }))
      .mockResolvedValueOnce(new Response(JSON.stringify([
        { id: "action-1", name: "SQL", playbook_run_id: "run-1", status: "failed", result: null },
      ]), { status: 200, headers: { "Content-Type": "application/json" } }))
      .mockResolvedValueOnce(new Response(JSON.stringify([
        {
          id: "action-1",
          error: "db timeout",
          result: null,
          artifacts: [{ id: "artifact-1", path: "failure.log" }],
        },
      ]), { status: 200, headers: { "Content-Type": "application/json" } }));

    await expect(getPlaybookRunWithActions("run-1")).resolves.toEqual({
      run: { id: "run-1", playbook_id: "pb-1", status: "failed" },
      childRuns: [],
      actions: [
        {
          id: "action-1",
          name: "SQL",
          playbook_run_id: "run-1",
          status: "failed",
          result: null,
          error: "db timeout",
          artifacts: [{ id: "artifact-1", path: "failure.log" }],
        },
      ],
    });
    expect(fetchMock.mock.calls[2]?.[0]).toContain("/db/playbook_run_actions?select=id,error,result,artifacts:artifacts(*)");
  });

  it("identifies final run states", () => {
    expect(isFinalPlaybookRunStatus("completed")).toBe(true);
    expect(isFinalPlaybookRunStatus("failed")).toBe(true);
    expect(isFinalPlaybookRunStatus("running")).toBe(false);
    expect(isFinalPlaybookRunStatus(undefined)).toBe(false);
  });
});
