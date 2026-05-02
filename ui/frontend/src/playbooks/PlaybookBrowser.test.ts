import { describe, expect, it } from "vitest";
import {
  actionDisplayStatus,
  ansiSegments,
  buildSubmitPayload,
  errorDiagnosticsFromAction,
  errorDiagnosticsFromRun,
  normalizePlaybookParams,
  parseDiagnosticsStackTrace,
  outputTextMode,
  playbookRunDuration,
  playbookStatusTone,
  primaryActionOutput,
  runElapsed,
  runTimelineEvents,
  shortRunId,
  stepOutputMaxHeight,
  stepParameterGroups,
} from "./PlaybookBrowser";
import {
  buildPlaybookSections,
  recentTargetsForPlaybook,
  statusVisual,
  targetSummaryFromRun,
} from "./playbook-ui-helpers";
import type { Playbook, PlaybookParameter, PlaybookRun, PlaybookRunAction } from "../api/types";

describe("playbook UI helpers", () => {
  it("normalizes parameter defaults to backend string params", () => {
    const parameters: PlaybookParameter[] = [
      { name: "message", type: "text", default: "hello" },
      { name: "enabled", type: "checkbox", default: true },
      { name: "empty", type: "text" },
      { name: "json", type: "code", default: { a: 1 } },
    ];

    expect(normalizePlaybookParams(parameters)).toEqual({
      message: "hello",
      enabled: "true",
      empty: "",
      json: "{\"a\":1}",
    });
  });

  it("builds a submit payload without empty params", () => {
    expect(buildSubmitPayload("pb-1", { config_id: "cfg-1" }, { a: "1", b: "" })).toEqual({
      id: "pb-1",
      config_id: "cfg-1",
      params: { a: "1" },
    });
  });

  it("formats completed run duration", () => {
    const run: PlaybookRun = {
      id: "run-1",
      playbook_id: "pb-1",
      status: "completed",
      start_time: "2026-04-27T10:00:00Z",
      end_time: "2026-04-27T10:02:05Z",
    };

    expect(playbookRunDuration(run)).toBe("2m 5s");
  });

  it("formats run detail header ids and elapsed time", () => {
    expect(shortRunId("07ffd27a-b33f-4ee6-80d6-b83430a4a16e")).toBe("07ffd27a");
    expect(shortRunId("run_8821")).toBe("run_8821");
    expect(runElapsed({
      id: "run-1",
      playbook_id: "pb-1",
      status: "completed",
      start_time: "2026-04-27T10:00:00Z",
      end_time: "2026-04-27T10:02:05Z",
    })).toBe("00:02:05");
  });

  it("infers running only after a step starts without a terminal date", () => {
    expect(actionDisplayStatus({
      id: "spec:run-1:1",
      name: "Future",
      playbook_run_id: "run-1",
      synthetic: true,
    })).toBeUndefined();
    expect(actionDisplayStatus({
      id: "action-1",
      name: "Apply",
      playbook_run_id: "run-1",
      start_time: "2026-04-27T10:00:00Z",
    })).toBe("running");
    expect(actionDisplayStatus({
      id: "action-2",
      name: "Apply",
      playbook_run_id: "run-1",
      status: "scheduled",
      start_time: "2026-04-27T10:00:00Z",
    })).toBe("running");
    expect(actionDisplayStatus({
      id: "action-3",
      name: "Apply",
      playbook_run_id: "run-1",
      status: "completed",
      start_time: "2026-04-27T10:00:00Z",
    })).toBe("completed");
    const cancelledAction: PlaybookRunAction & { cancelled_at: string } = {
      id: "action-4",
      name: "Apply",
      playbook_run_id: "run-1",
      start_time: "2026-04-27T10:00:00Z",
      cancelled_at: "2026-04-27T10:00:10Z",
    };
    expect(actionDisplayStatus(cancelledAction)).toBeUndefined();
  });

  it("groups run and step parameters for the side rail", () => {
    const groups = stepParameterGroups(
      {
        id: "run-1",
        playbook_id: "pb-1",
        parameters: { node: "ip-10-1-3-42", empty: "" },
      },
      [
        {
          id: "action-1",
          name: "Rolling restart",
          playbook_run_id: "run-1",
          status: "running",
          result: { parameters: { workload: "deployment/checkout-api", replicas: 6 } },
        },
      ],
    );

    expect(groups).toEqual([
      expect.objectContaining({ label: "Run parameters", entries: [["node", "ip-10-1-3-42"]] }),
      expect.objectContaining({ label: "Rolling restart", entries: [["workload", "deployment/checkout-api"], ["replicas", "6"]] }),
    ]);
  });

  it("builds chronological run timeline events", () => {
    const events = runTimelineEvents(
      {
        id: "run-1",
        playbook_id: "pb-1",
        status: "completed",
        created_at: "2026-04-27T10:00:00Z",
        start_time: "2026-04-27T10:00:02Z",
        end_time: "2026-04-27T10:01:00Z",
      },
      [
        {
          id: "action-1",
          name: "Drain node",
          playbook_run_id: "run-1",
          status: "completed",
          start_time: "2026-04-27T10:00:03Z",
          end_time: "2026-04-27T10:00:10Z",
        },
      ],
    );

    expect(events.map((event) => event.label)).toEqual([
      "Run created",
      "Run started",
      "Step 1 - Drain node started",
      "Step 1 - Drain node completed",
      "Run completed",
    ]);
  });

  it("extracts action error diagnostics from nested result error", () => {
    const diagnostics = errorDiagnosticsFromAction({
      id: "action-1",
      name: "Create RoleBinding",
      playbook_run_id: "run-1",
      status: "failed",
      error: "path contains illegal characters",
      result: {
        error: {
          error: "path /rolebinding-<no value>.yaml contains illegal characters",
          trace: "01KQ9J283Q80E6HZ3M82C7ZK40",
          time: "2026-04-28T08:07:36.820453Z",
          context: {
            run_id: "run-1",
            action_name: "Create RoleBinding",
          },
          stacktrace: "Oops: path contains illegal characters\n  --- at gitops.go:136",
        },
      },
    });

    expect(diagnostics).toMatchObject({
      message: "path /rolebinding-<no value>.yaml contains illegal characters",
      trace: "01KQ9J283Q80E6HZ3M82C7ZK40",
      stacktrace: "Oops: path contains illegal characters\n  --- at gitops.go:136",
      context: [["run_id", "run-1"], ["action_name", "Create RoleBinding"]],
    });
  });

  it("keeps JSON context fields and omits native output fields from diagnostics context", () => {
    const diagnostics = errorDiagnosticsFromAction({
      id: "action-1",
      name: "HTTP",
      playbook_run_id: "run-1",
      status: "failed",
      result: {
        stdout: "response body",
        stderr: "warning",
        error: {
          message: "request failed",
          context: {
            args: { method: "GET", url: "http://localhost:8080" },
            connection: "{\"type\":\"http\",\"name\":\"local\"}",
            stdout: "response body",
            stderr: "warning",
          },
        },
      },
    });

    expect(diagnostics?.context).toEqual([
      ["args", "{\"method\":\"GET\",\"url\":\"http://localhost:8080\"}"],
      ["connection", "{\"type\":\"http\",\"name\":\"local\"}"],
    ]);
  });

  it("uses primary action output before falling back to action error", () => {
    expect(primaryActionOutput({
      id: "action-1",
      name: "SQL",
      playbook_run_id: "run-1",
      status: "failed",
      result: {
        stdout: "rows",
        stderr: "warning",
      },
      error: "db timeout",
    })).toBe("rows\nwarning");

    expect(primaryActionOutput({
      id: "action-2",
      name: "SQL",
      playbook_run_id: "run-1",
      status: "failed",
      result: null,
      error: "db timeout",
    })).toBe("db timeout");
  });

  it("classifies log output for wrapping and tables for horizontal scroll", () => {
    expect(outputTextMode("stdout: this-is-a-very-long-token-without-spaces-that-should-wrap")).toBe("text");
    expect(outputTextMode([
      "NAME                                      READY   STATUS",
      "checkout-api-6f8b7d9d7c-x4pqp            1/1     Running",
      "checkout-worker-66dcbf8d8c-n2k3p         1/1     Running",
    ].join("\n"))).toBe("table");
    expect(outputTextMode([
      "| name | namespace | status |",
      "| --- | --- | --- |",
      "| checkout-api | prod | Running |",
    ].join("\n"))).toBe("table");
  });

  it("sizes step output from viewport and step count", () => {
    expect(stepOutputMaxHeight(4)).toEqual({ maxHeight: "min(20vh, calc((100vh - 200px) / 4))" });
    expect(stepOutputMaxHeight(0)).toEqual({ maxHeight: "min(20vh, calc((100vh - 200px) / 1))" });
  });

  it("parses ANSI stdout/stderr colors for rendering", () => {
    expect(ansiSegments("ok \u001b[31mfailed\u001b[0m done")).toEqual([
      { text: "ok " },
      { text: "failed", style: { color: "#f87171" } },
      { text: " done" },
    ]);
    expect(ansiSegments("\u001b[1;38;2;1;2;3mstrong\u001b[0m")).toEqual([
      { text: "strong", style: { color: "rgb(1, 2, 3)", fontWeight: 700 } },
    ]);
  });

  it("extracts run error diagnostics from the run record", () => {
    const diagnostics = errorDiagnosticsFromRun({
      id: "run-1",
      playbook_id: "pb-1",
      status: "failed",
      error: "run failed before any step completed",
      request: {
        diagnostics: {
          trace: "01KQ9J283Q80E6HZ3M82C7ZK40",
          context: {
            run_id: "run-1",
          },
        },
      },
    });

    expect(diagnostics).toMatchObject({
      message: "run failed before any step completed",
      trace: "01KQ9J283Q80E6HZ3M82C7ZK40",
      context: [["run_id", "run-1"]],
    });
  });

  it("parses diagnostics stack traces into frames", () => {
    const parsed = parseDiagnosticsStackTrace([
      "Oops: path /rolebinding-<no value>.yaml contains illegal characters",
      "  --- at github.com/flanksource/incident-commander/playbook/actions/gitops.go:136 GitOps.validatePaths()",
      "  --- at github.com/flanksource/incident-commander/playbook/runner/exec.go:139 executeAction()",
      "  --- at /Users/moshe/go/pkg/mod/gorm.io/gorm@v1.31.1/finisher_api.go:654 DB.Transaction()",
    ].join("\n"));

    expect(parsed.headline).toBe("Oops: path /rolebinding-<no value>.yaml contains illegal characters");
    expect(parsed.frames).toEqual([
      {
        raw: "--- at github.com/flanksource/incident-commander/playbook/actions/gitops.go:136 GitOps.validatePaths()",
        file: "github.com/flanksource/incident-commander/playbook/actions/gitops.go",
        line: 136,
        functionName: "GitOps.validatePaths()",
      },
      expect.objectContaining({
        file: "github.com/flanksource/incident-commander/playbook/runner/exec.go",
        line: 139,
        functionName: "executeAction()",
      }),
      expect.objectContaining({
        file: "/Users/moshe/go/pkg/mod/gorm.io/gorm@v1.31.1/finisher_api.go",
        line: 654,
        functionName: "DB.Transaction()",
      }),
    ]);
  });

  it("maps statuses to badge tones", () => {
    expect(playbookStatusTone("completed")).toBe("success");
    expect(playbookStatusTone("failed")).toBe("danger");
    expect(playbookStatusTone("running")).toBe("info");
    expect(playbookStatusTone("pending_approval")).toBe("warning");
  });

  it("builds sections from backend playbook order and run-derived favorites", () => {
    const playbooks: Playbook[] = [
      { id: "pb-sql", name: "Backup database", category: "SQL Server" },
      { id: "pb-k8s", name: "Restart pod", category: "Kubernetes" },
      { id: "pb-aws", name: "Rotate key", category: "AWS" },
    ];
    const runs: PlaybookRun[] = [
      { id: "run-1", playbook_id: "pb-k8s" },
      { id: "run-2", playbook_id: "pb-k8s" },
      { id: "run-3", playbook_id: "pb-sql" },
    ];

    const sections = buildPlaybookSections(playbooks, runs);

    expect(sections[0]).toMatchObject({ id: "favorites", icon: "heart-checkmark" });
    expect(sections[0].playbooks.map((playbook) => playbook.id)).toEqual(["pb-k8s", "pb-sql"]);
    expect(sections.slice(1).map((section) => section.label)).toEqual(["SQL Server", "Kubernetes", "AWS"]);
  });

  it("summarizes recent targets for quick re-run chips", () => {
    const runs: PlaybookRun[] = [
      {
        id: "run-1",
        playbook_id: "pb-1",
        config_id: "cfg-1",
        config: { name: "prod-api", type: "k8s-deployment" },
      },
      {
        id: "run-2",
        playbook_id: "pb-1",
        config_id: "cfg-1",
        config: { name: "prod-api", type: "k8s-deployment" },
      },
      {
        id: "run-3",
        playbook_id: "pb-1",
        component_id: "cmp-1",
        component: { name: "checkout" },
      },
    ];

    expect(targetSummaryFromRun(runs[0])).toMatchObject({
      key: "config:cfg-1",
      label: "prod-api",
      icon: "k8s-deployment",
      target: { config_id: "cfg-1" },
    });
    expect(recentTargetsForPlaybook("pb-1", runs)).toEqual([
      expect.objectContaining({ key: "config:cfg-1", count: 2 }),
      expect.objectContaining({ key: "component:cmp-1", count: 1, icon: "config" }),
    ]);
  });

  it("uses flanksource status icons for run states", () => {
    expect(statusVisual("completed")).toMatchObject({ tone: "success", icon: "checkmark" });
    expect(statusVisual("pending_approval")).toMatchObject({ tone: "warning", icon: "wait-for-approval" });
    expect(statusVisual("scheduled")).toMatchObject({ tone: "warning", icon: "add-clock" });
    expect(statusVisual("failed")).toMatchObject({ tone: "danger", icon: "scorecard-fail" });
  });
});
