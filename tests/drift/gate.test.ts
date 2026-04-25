import { describe, expect, it } from "vitest";

import { decideGate } from "../../src/drift/gate.js";
import {
  DRIFT_REPORT_SCHEMA_VERSION,
  type DriftReport,
} from "../../src/drift/types.js";

const cleanReport: DriftReport = {
  schemaVersion: DRIFT_REPORT_SCHEMA_VERSION,
  active: "leaf",
  fingerprintOk: true,
  entries: [],
  scannedFiles: 5,
  fastPathHits: 5,
  slowPathHits: 0,
};

const driftedReport: DriftReport = {
  schemaVersion: DRIFT_REPORT_SCHEMA_VERSION,
  active: "leaf",
  fingerprintOk: true,
  entries: [
    {
      relPath: "CLAUDE.md",
      status: "modified",
      provenance: [],
    },
  ],
  scannedFiles: 5,
  fastPathHits: 4,
  slowPathHits: 1,
};

const noActiveReport: DriftReport = {
  schemaVersion: DRIFT_REPORT_SCHEMA_VERSION,
  active: null,
  fingerprintOk: false,
  entries: [],
  scannedFiles: 0,
  fastPathHits: 0,
  slowPathHits: 0,
};

describe("decideGate (R21, R23, R24)", () => {
  it("returns no-drift when fingerprintOk and entries is empty", () => {
    const out = decideGate({ report: cleanReport, mode: "interactive" });
    expect(out.kind).toBe("no-drift");
    expect(out.choice).toBe("no-drift-proceed");
  });

  it("returns no-drift when fingerprintOk is false (NoActive)", () => {
    const out = decideGate({ report: noActiveReport, mode: "interactive" });
    expect(out.kind).toBe("no-drift");
    expect(out.choice).toBe("no-drift-proceed");
  });

  it("R21 invariant: interactive + drift + no flag → prompt", () => {
    const out = decideGate({ report: driftedReport, mode: "interactive" });
    expect(out.kind).toBe("prompt");
    expect(out.choice).toBeNull();
  });

  it("hard-block invariant: non-interactive + drift + no flag → auto abort", () => {
    const out = decideGate({ report: driftedReport, mode: "non-interactive" });
    expect(out.kind).toBe("auto");
    expect(out.choice).toBe("abort");
    expect(out.reason).toMatch(/non-interactive/);
  });

  it("flag wins over interactive prompt: --on-drift=discard", () => {
    const out = decideGate({
      report: driftedReport,
      mode: "interactive",
      onDriftFlag: "discard",
    });
    expect(out.kind).toBe("auto");
    expect(out.choice).toBe("discard");
  });

  it("flag honored in non-interactive mode: --on-drift=persist", () => {
    const out = decideGate({
      report: driftedReport,
      mode: "non-interactive",
      onDriftFlag: "persist",
    });
    expect(out.kind).toBe("auto");
    expect(out.choice).toBe("persist");
  });

  it("flag honored in non-interactive mode: --on-drift=abort", () => {
    const out = decideGate({
      report: driftedReport,
      mode: "non-interactive",
      onDriftFlag: "abort",
    });
    expect(out.kind).toBe("auto");
    expect(out.choice).toBe("abort");
  });

  it("non-interactive never returns prompt (epic invariant)", () => {
    // Across every combination of flag + drift state, non-interactive mode
    // must never produce a "prompt" outcome — that would block forever.
    for (const flag of [undefined, "discard", "persist", "abort"] as const) {
      for (const report of [cleanReport, driftedReport, noActiveReport]) {
        const out = decideGate({
          report,
          mode: "non-interactive",
          onDriftFlag: flag,
        });
        expect(out.kind).not.toBe("prompt");
      }
    }
  });
});
