import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { buildStatePaths } from "../../src/state/paths.js";
import { readStateFile, writeStateFile } from "../../src/state/state-file.js";
import {
  STATE_FILE_SCHEMA_VERSION,
  FINGERPRINT_SCHEMA_VERSION,
  defaultState,
  type StateFile,
} from "../../src/state/types.js";

async function makeRoot(): Promise<{ root: string; cleanup: () => Promise<void> }> {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-state-"));
  return { root, cleanup: () => fs.rm(root, { recursive: true, force: true }) };
}

describe("state-file IO", () => {
  let root: string;
  let cleanup: () => Promise<void>;
  beforeEach(async () => {
    ({ root, cleanup } = await makeRoot());
  });
  afterEach(async () => {
    await cleanup();
  });

  it("returns Missing warning + defaultState when file does not exist", async () => {
    const paths = buildStatePaths(root);
    const r = await readStateFile(paths);
    expect(r.warning?.code).toBe("Missing");
    expect(r.state).toEqual(defaultState());
  });

  it("round-trips a written state", async () => {
    const paths = buildStatePaths(root);
    const state: StateFile = {
      schemaVersion: STATE_FILE_SCHEMA_VERSION,
      activeProfile: "myprofile",
      materializedAt: "2026-04-25T12:00:00.000Z",
      resolvedSources: [
        { id: "base", kind: "ancestor", rootPath: "/abs/base", external: false },
        { id: "myprofile", kind: "profile", rootPath: "/abs/leaf", external: false },
      ],
      fingerprint: {
        schemaVersion: FINGERPRINT_SCHEMA_VERSION,
        files: {
          "CLAUDE.md": { size: 100, mtimeMs: 1000, contentHash: "abc" },
        },
      },
      externalTrustNotices: [],
    };
    await writeStateFile(paths, state);
    const r = await readStateFile(paths);
    expect(r.warning).toBeNull();
    expect(r.state).toEqual(state);
  });

  it("R42: degrades to defaultState + warning on unparseable JSON", async () => {
    const paths = buildStatePaths(root);
    await fs.mkdir(paths.profilesDir, { recursive: true });
    await fs.writeFile(paths.stateFile, "{not valid json");
    const r = await readStateFile(paths);
    expect(r.warning?.code).toBe("ParseError");
    expect(r.state).toEqual(defaultState());
  });

  it("R42: degrades on schemaVersion mismatch", async () => {
    const paths = buildStatePaths(root);
    await fs.mkdir(paths.profilesDir, { recursive: true });
    await fs.writeFile(
      paths.stateFile,
      JSON.stringify({ schemaVersion: 99, activeProfile: null }),
    );
    const r = await readStateFile(paths);
    expect(r.warning?.code).toBe("SchemaMismatch");
    expect(r.state).toEqual(defaultState());
  });

  it("R42: degrades on missing fields", async () => {
    const paths = buildStatePaths(root);
    await fs.mkdir(paths.profilesDir, { recursive: true });
    await fs.writeFile(
      paths.stateFile,
      JSON.stringify({ schemaVersion: STATE_FILE_SCHEMA_VERSION, activeProfile: 1 }),
    );
    const r = await readStateFile(paths);
    expect(r.warning?.code).toBe("SchemaMismatch");
  });

  it("R42: degrades when fingerprint.files entry is null", async () => {
    const paths = buildStatePaths(root);
    await fs.mkdir(paths.profilesDir, { recursive: true });
    await fs.writeFile(
      paths.stateFile,
      JSON.stringify({
        schemaVersion: STATE_FILE_SCHEMA_VERSION,
        activeProfile: null,
        materializedAt: null,
        resolvedSources: [],
        fingerprint: {
          schemaVersion: FINGERPRINT_SCHEMA_VERSION,
          files: { "a.md": null },
        },
        externalTrustNotices: [],
      }),
    );
    const r = await readStateFile(paths);
    expect(r.warning?.code).toBe("SchemaMismatch");
    expect(r.state).toEqual(defaultState());
  });

  it("R42: degrades when fingerprint.files entry has wrong-typed fields", async () => {
    const paths = buildStatePaths(root);
    await fs.mkdir(paths.profilesDir, { recursive: true });
    await fs.writeFile(
      paths.stateFile,
      JSON.stringify({
        schemaVersion: STATE_FILE_SCHEMA_VERSION,
        activeProfile: null,
        materializedAt: null,
        resolvedSources: [],
        fingerprint: {
          schemaVersion: FINGERPRINT_SCHEMA_VERSION,
          files: { "a.md": { size: "bad", mtimeMs: 0, contentHash: "" } },
        },
        externalTrustNotices: [],
      }),
    );
    const r = await readStateFile(paths);
    expect(r.warning?.code).toBe("SchemaMismatch");
    expect(r.state).toEqual(defaultState());
  });

  it("R42: degrades when fingerprint.files entry is missing contentHash", async () => {
    const paths = buildStatePaths(root);
    await fs.mkdir(paths.profilesDir, { recursive: true });
    await fs.writeFile(
      paths.stateFile,
      JSON.stringify({
        schemaVersion: STATE_FILE_SCHEMA_VERSION,
        activeProfile: null,
        materializedAt: null,
        resolvedSources: [],
        fingerprint: {
          schemaVersion: FINGERPRINT_SCHEMA_VERSION,
          files: { "a.md": { size: 10, mtimeMs: 0 } },
        },
        externalTrustNotices: [],
      }),
    );
    const r = await readStateFile(paths);
    expect(r.warning?.code).toBe("SchemaMismatch");
    expect(r.state).toEqual(defaultState());
  });

  it("R14a: write does not leave temp staging files behind", async () => {
    const paths = buildStatePaths(root);
    await writeStateFile(paths, defaultState());
    // No `.tmp` files anywhere under .claude-profiles/.
    const stale = await listAllTmpFiles(paths.profilesDir);
    expect(stale).toEqual([]);
  });
});

async function listAllTmpFiles(root: string): Promise<string[]> {
  const out: string[] = [];
  async function walk(dir: string): Promise<void> {
    let entries;
    try {
      entries = await fs.readdir(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const e of entries) {
      const abs = path.join(dir, e.name);
      if (e.isDirectory()) await walk(abs);
      else if (e.name.endsWith(".tmp")) out.push(abs);
    }
  }
  await walk(root);
  return out;
}
