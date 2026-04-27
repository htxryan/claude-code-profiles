/**
 * Tests for src/cli/preview.ts (azp).
 */

import { describe, expect, it } from "vitest";

import {
  isBinary,
  renderHeadPreview,
  renderUnifiedDiff,
} from "../../src/cli/preview.js";

describe("isBinary", () => {
  it("returns false for plain ASCII text", () => {
    expect(isBinary(Buffer.from("hello world\n"))).toBe(false);
  });

  it("returns false for UTF-8 with non-ASCII codepoints", () => {
    expect(isBinary(Buffer.from("héllo — wörld\n", "utf8"))).toBe(false);
  });

  it("returns true on a NUL byte in the first 8KB", () => {
    const buf = Buffer.concat([Buffer.from("text"), Buffer.from([0]), Buffer.from("more")]);
    expect(isBinary(buf)).toBe(true);
  });

  it("returns true on common binary file headers (PNG)", () => {
    // PNG starts with 0x89 0x50 0x4E 0x47 0x0D 0x0A 0x1A 0x0A then a 0x00.
    const png = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00]);
    expect(isBinary(png)).toBe(true);
  });

  it("only sniffs the first 8KB", () => {
    // Pad past 8KB with text, then a NUL — should NOT register as binary.
    const text = Buffer.alloc(9000, "a".charCodeAt(0));
    const trailing = Buffer.concat([text, Buffer.from([0])]);
    expect(isBinary(trailing)).toBe(false);
  });
});

describe("renderUnifiedDiff", () => {
  it("renders a small change with prefix/suffix context", () => {
    const a = Buffer.from("line1\nline2\nline3\n");
    const b = Buffer.from("line1\nLINE2\nline3\n");
    const out = renderUnifiedDiff(a, b);
    // Prefix line is context; the divergent middle is delete-then-add.
    expect(out).toContain(" line1");
    expect(out).toContain("-line2");
    expect(out).toContain("+LINE2");
    expect(out).toContain(" line3");
  });

  it("produces empty output for identical inputs (only context)", () => {
    const buf = Buffer.from("a\nb\n");
    const out = renderUnifiedDiff(buf, buf);
    // All lines render as context; no `+`/`-` lines.
    expect(out).not.toMatch(/^[+-]/m);
    expect(out).toBe(" a\n b");
  });

  it("truncates output past maxLines and emits a tail footer", () => {
    const aLines = Array.from({ length: 50 }, (_, i) => `a${i}`).join("\n") + "\n";
    const bLines = Array.from({ length: 50 }, (_, i) => `b${i}`).join("\n") + "\n";
    const out = renderUnifiedDiff(Buffer.from(aLines), Buffer.from(bLines), { maxLines: 10 });
    const lines = out.split("\n");
    // 10 rendered lines + 1 truncation footer.
    expect(lines.length).toBe(11);
    expect(lines[lines.length - 1]).toMatch(/truncated, \d+ more lines/);
  });

  it("substitutes binary placeholder when either side is binary", () => {
    const text = Buffer.from("abc\n");
    const bin = Buffer.concat([Buffer.from([0]), Buffer.from("x")]);
    const out = renderUnifiedDiff(text, bin);
    expect(out).toMatch(/binary file — \d+ bytes/);
  });
});

describe("renderHeadPreview", () => {
  it("returns first N lines for a multi-line text buffer", () => {
    const buf = Buffer.from("a\nb\nc\nd\ne\nf\n");
    const out = renderHeadPreview(buf, { maxLines: 3 });
    const lines = out.split("\n");
    // 3 head lines + truncation footer.
    expect(lines.length).toBe(4);
    expect(lines[0]).toBe("a");
    expect(lines[2]).toBe("c");
    expect(lines[3]).toMatch(/truncated, 3 more lines/);
  });

  it("returns the entire content when fewer than maxLines", () => {
    const buf = Buffer.from("only\nlines\n");
    const out = renderHeadPreview(buf, { maxLines: 10 });
    expect(out).toBe("only\nlines");
  });

  it("substitutes binary placeholder for binary content", () => {
    const bin = Buffer.from([0x00, 0x01, 0x02]);
    expect(renderHeadPreview(bin)).toMatch(/binary file — 3 bytes/);
  });
});
