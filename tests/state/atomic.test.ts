import { promises as fs } from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  atomicRename,
  atomicWriteFile,
  fsyncDir,
  pathExists,
  rmrf,
} from "../../src/state/atomic.js";

describe("atomic primitives", () => {
  let tmp: string;
  beforeEach(async () => {
    tmp = await fs.mkdtemp(path.join(os.tmpdir(), "ccp-atomic-"));
  });
  afterEach(async () => {
    await fs.rm(tmp, { recursive: true, force: true });
  });

  describe("atomicWriteFile", () => {
    it("writes contents to dest via temp+rename", async () => {
      const dest = path.join(tmp, "target.json");
      const tmpPath = `${dest}.tmp`;
      await atomicWriteFile(dest, tmpPath, '{"a":1}');
      const read = await fs.readFile(dest, "utf8");
      expect(read).toBe('{"a":1}');
      // Temp file should not remain after successful rename.
      expect(await pathExists(tmpPath)).toBe(false);
    });

    it("overwrites an existing dest atomically", async () => {
      const dest = path.join(tmp, "target.json");
      await fs.writeFile(dest, '{"old":true}');
      await atomicWriteFile(dest, `${dest}.tmp`, '{"new":true}');
      expect(await fs.readFile(dest, "utf8")).toBe('{"new":true}');
    });

    it("accepts Buffer contents", async () => {
      const dest = path.join(tmp, "buf.bin");
      const buf = Buffer.from([0xde, 0xad, 0xbe, 0xef]);
      await atomicWriteFile(dest, `${dest}.tmp`, buf);
      const read = await fs.readFile(dest);
      expect(read).toEqual(buf);
    });
  });

  describe("atomicRename", () => {
    it("renames a file", async () => {
      const src = path.join(tmp, "src.txt");
      const dst = path.join(tmp, "dst.txt");
      await fs.writeFile(src, "hi");
      await atomicRename(src, dst);
      expect(await pathExists(src)).toBe(false);
      expect(await fs.readFile(dst, "utf8")).toBe("hi");
    });

    it("renames a directory", async () => {
      const src = path.join(tmp, "srcdir");
      const dst = path.join(tmp, "dstdir");
      await fs.mkdir(src);
      await fs.writeFile(path.join(src, "f"), "x");
      await atomicRename(src, dst);
      expect(await pathExists(src)).toBe(false);
      expect(await fs.readFile(path.join(dst, "f"), "utf8")).toBe("x");
    });

    it("propagates ENOENT for missing source", async () => {
      await expect(atomicRename(path.join(tmp, "nope"), path.join(tmp, "x"))).rejects.toThrow();
    });
  });

  describe("rmrf", () => {
    it("removes a directory tree", async () => {
      const dir = path.join(tmp, "tree");
      await fs.mkdir(path.join(dir, "sub"), { recursive: true });
      await fs.writeFile(path.join(dir, "sub", "f"), "x");
      await rmrf(dir);
      expect(await pathExists(dir)).toBe(false);
    });

    it("tolerates missing target", async () => {
      await expect(rmrf(path.join(tmp, "missing"))).resolves.toBeUndefined();
    });
  });

  describe("pathExists", () => {
    it("returns true for existing", async () => {
      const f = path.join(tmp, "f");
      await fs.writeFile(f, "x");
      expect(await pathExists(f)).toBe(true);
    });
    it("returns false for missing", async () => {
      expect(await pathExists(path.join(tmp, "no"))).toBe(false);
    });
  });

  describe("fsyncDir", () => {
    it("does not throw on a real directory", () => {
      // Best-effort: just ensure it doesn't throw.
      fsyncDir(path.join(tmp, "f.json"));
    });
  });
});
