/**
 * Unit tests for src/markers.ts — single source of truth for the project-root
 * CLAUDE.md managed-block markers (cw6 / spec §12).
 *
 * Coverage: regex constant shape, parseMarkers happy/missing/malformed paths,
 * version capture, namespace tail handling, nested-marker rejection,
 * renderManagedBlock round-trip, and injectMarkersIntoFile (idempotency +
 * byte-for-byte preservation of pre-existing user content).
 */

import { describe, expect, it } from "vitest";

import {
  MARKER_REGEX,
  MalformedMarkersError,
  injectMarkersIntoFile,
  parseMarkers,
  renderManagedBlock,
} from "../src/markers.js";

describe("MARKER_REGEX (spec §12.3)", () => {
  it("matches the canonical begin/end pair around a non-greedy body", () => {
    const text = `<!-- c3p:v1:begin -->
body
<!-- c3p:v1:end -->`;
    const m = text.match(MARKER_REGEX);
    expect(m).not.toBeNull();
    expect(m?.[1]).toBe("1");
    // Tail capture is the bytes between version and `-->` (canonical: " ").
    expect(m?.[3]).toContain("body");
  });

  it("body is non-greedy: the first :end terminates the match", () => {
    // Two managed blocks in one document — the regex must not collapse them.
    const text = [
      "<!-- c3p:v1:begin -->",
      "first",
      "<!-- c3p:v1:end -->",
      "between",
      "<!-- c3p:v1:begin -->",
      "second",
      "<!-- c3p:v1:end -->",
    ].join("\n");
    const m = text.match(MARKER_REGEX);
    // First match must be the FIRST block only.
    expect(m?.[3]).toContain("first");
    expect(m?.[3]).not.toContain("second");
    expect(m?.[3]).not.toContain("between");
  });
});

describe("parseMarkers", () => {
  it("happy path: returns before/section/after split + version", () => {
    const content = [
      "user header",
      "<!-- c3p:v1:begin -->",
      "<!-- Managed block. -->",
      "",
      "managed body line",
      "<!-- c3p:v1:end -->",
      "user footer",
      "",
    ].join("\n");
    const r = parseMarkers(content);
    expect(r.found).toBe(true);
    if (!r.found) return;
    expect(r.version).toBe(1);
    expect(r.before).toBe("user header\n");
    expect(r.after).toBe("\nuser footer\n");
    // section is the captured body between begin/end markers (excluding the
    // marker lines themselves).
    expect(r.section).toContain("managed body line");
  });

  it("returns absent when no markers are present", () => {
    const r = parseMarkers("just user content\n");
    expect(r.found).toBe(false);
    if (r.found) return;
    expect(r.reason).toBe("absent");
  });

  it("returns malformed when only :begin is present", () => {
    const r = parseMarkers("<!-- c3p:v1:begin -->\nno end\n");
    expect(r.found).toBe(false);
    if (r.found) return;
    expect(r.reason).toBe("malformed");
  });

  it("returns malformed when only :end is present", () => {
    const r = parseMarkers("no begin\n<!-- c3p:v1:end -->\n");
    expect(r.found).toBe(false);
    if (r.found) return;
    expect(r.reason).toBe("malformed");
  });

  it("returns malformed when versions mismatch (begin v1 / end v2)", () => {
    const text = [
      "<!-- c3p:v1:begin -->",
      "body",
      "<!-- c3p:v2:end -->",
    ].join("\n");
    const r = parseMarkers(text);
    expect(r.found).toBe(false);
    if (r.found) return;
    expect(r.reason).toBe("malformed");
  });

  it("returns malformed when more than one well-formed block exists", () => {
    // Per spec §12.3: "More than one match is reserved and currently rejected;
    // v1 implementations may treat it as malformed."
    const text = [
      "<!-- c3p:v1:begin -->",
      "first",
      "<!-- c3p:v1:end -->",
      "",
      "<!-- c3p:v1:begin -->",
      "second",
      "<!-- c3p:v1:end -->",
    ].join("\n");
    const r = parseMarkers(text);
    expect(r.found).toBe(false);
    if (r.found) return;
    expect(r.reason).toBe("malformed");
  });

  it("captures higher version numbers verbatim", () => {
    const text = `<!-- c3p:v42:begin -->\nbody\n<!-- c3p:v42:end -->`;
    const r = parseMarkers(text);
    expect(r.found).toBe(true);
    if (!r.found) return;
    expect(r.version).toBe(42);
  });

  it("supports an empty managed section", () => {
    const text = [
      "before",
      "<!-- c3p:v1:begin -->",
      "<!-- c3p:v1:end -->",
      "after",
    ].join("\n");
    const r = parseMarkers(text);
    expect(r.found).toBe(true);
    if (!r.found) return;
    expect(r.section.trim()).toBe("");
    expect(r.before).toBe("before\n");
    expect(r.after).toBe("\nafter");
  });

  // cw6.5 followup: parseMarkers must tolerate CRLF line endings (Windows
  // editors / default `git config core.autocrlf=true` checkouts). Both the
  // happy and malformed paths are pinned so a future regex tightening that
  // breaks Windows users surfaces in CI rather than after a bug report.
  describe("CRLF tolerance (cw6.5)", () => {
    it("parses a well-formed CRLF file (happy path)", () => {
      const text = [
        "user header",
        "<!-- c3p:v1:begin -->",
        "<!-- Managed block. -->",
        "",
        "managed body line",
        "<!-- c3p:v1:end -->",
        "user footer",
        "",
      ].join("\r\n");
      const r = parseMarkers(text);
      expect(r.found).toBe(true);
      if (!r.found) return;
      expect(r.version).toBe(1);
      // CRLF bytes are preserved verbatim in before/after slices.
      expect(r.before).toBe("user header\r\n");
      expect(r.after).toBe("\r\nuser footer\r\n");
      expect(r.section).toContain("managed body line");
    });

    it("flags a CRLF file with only :begin as malformed (not absent)", () => {
      const text = [
        "<!-- c3p:v1:begin -->",
        "no end",
        "",
      ].join("\r\n");
      const r = parseMarkers(text);
      expect(r.found).toBe(false);
      if (r.found) return;
      expect(r.reason).toBe("malformed");
    });

    it("flags a CRLF file with only :end as malformed (not absent)", () => {
      const text = [
        "no begin",
        "<!-- c3p:v1:end -->",
        "",
      ].join("\r\n");
      const r = parseMarkers(text);
      expect(r.found).toBe(false);
      if (r.found) return;
      expect(r.reason).toBe("malformed");
    });

    it("a CRLF file with no markers reports absent (not malformed)", () => {
      const r = parseMarkers("user content\r\nmore content\r\n");
      expect(r.found).toBe(false);
      if (r.found) return;
      expect(r.reason).toBe("absent");
    });
  });
});

describe("renderManagedBlock", () => {
  it("emits the canonical v1 block with begin, self-doc comment, body, end", () => {
    const block = renderManagedBlock("hello world\n");
    expect(block).toContain("<!-- c3p:v1:begin -->");
    expect(block).toContain("<!-- c3p:v1:end -->");
    expect(block).toContain("Managed block");
    expect(block).toContain("hello world");
  });

  it("round-trips through parseMarkers", () => {
    const body = "line a\nline b\n";
    const block = renderManagedBlock(body);
    const r = parseMarkers(block);
    expect(r.found).toBe(true);
    if (!r.found) return;
    expect(r.section).toContain("line a");
    expect(r.section).toContain("line b");
    expect(r.version).toBe(1);
  });

  it("accepts an empty body", () => {
    const block = renderManagedBlock("");
    const r = parseMarkers(block);
    expect(r.found).toBe(true);
    if (!r.found) return;
    expect(r.version).toBe(1);
  });
});

describe("injectMarkersIntoFile", () => {
  it("appends a marker block when input has no markers (preserves content above)", () => {
    const original = "# Project\n\nuser-authored content\n";
    const out = injectMarkersIntoFile(original);
    // Original content present, byte-for-byte, at the start of the new file.
    expect(out.startsWith(original)).toBe(true);
    // Markers appended after.
    expect(out).toContain("<!-- c3p:v1:begin -->");
    expect(out).toContain("<!-- c3p:v1:end -->");
    // The result parses cleanly.
    const parsed = parseMarkers(out);
    expect(parsed.found).toBe(true);
  });

  it("is a no-op when markers already exist", () => {
    const original = [
      "# Project",
      "",
      "<!-- c3p:v1:begin -->",
      "managed body",
      "<!-- c3p:v1:end -->",
      "",
      "footer",
    ].join("\n");
    const out = injectMarkersIntoFile(original);
    expect(out).toBe(original);
  });

  it("is idempotent (running twice == running once)", () => {
    const original = "# user content\n";
    const once = injectMarkersIntoFile(original);
    const twice = injectMarkersIntoFile(once);
    expect(twice).toBe(once);
  });

  it("preserves trailing-newline absence in user content", () => {
    // No trailing newline on user content. We must not silently mutate that —
    // the only added bytes must be the marker block itself.
    const original = "no trailing newline";
    const out = injectMarkersIntoFile(original);
    expect(out.startsWith(original)).toBe(true);
  });

  // cw6.3 followup: malformed input must fail closed rather than silently
  // appending a second fresh block on top of partial markers.
  it("throws MalformedMarkersError when input has a lone :begin (not appended-to)", () => {
    const original = "<!-- c3p:v1:begin -->\nno end\n";
    expect(() => injectMarkersIntoFile(original)).toThrow(MalformedMarkersError);
  });

  it("throws MalformedMarkersError when input has a lone :end", () => {
    const original = "no begin\n<!-- c3p:v1:end -->\n";
    expect(() => injectMarkersIntoFile(original)).toThrow(MalformedMarkersError);
  });

  it("throws MalformedMarkersError when input has version-mismatched markers", () => {
    const original = [
      "<!-- c3p:v1:begin -->",
      "body",
      "<!-- c3p:v2:end -->",
      "",
    ].join("\n");
    expect(() => injectMarkersIntoFile(original)).toThrow(MalformedMarkersError);
  });

  it("throws MalformedMarkersError when input has multiple well-formed blocks", () => {
    const original = [
      "<!-- c3p:v1:begin -->",
      "first",
      "<!-- c3p:v1:end -->",
      "",
      "<!-- c3p:v1:begin -->",
      "second",
      "<!-- c3p:v1:end -->",
      "",
    ].join("\n");
    expect(() => injectMarkersIntoFile(original)).toThrow(MalformedMarkersError);
  });

  it("preserves multiline user content with embedded HTML byte-for-byte", () => {
    const original = [
      "# Project",
      "",
      "<details><summary>more</summary>",
      "Some <b>html</b> inside markdown.",
      "</details>",
      "",
    ].join("\n");
    const out = injectMarkersIntoFile(original);
    expect(out.startsWith(original)).toBe(true);
    const parsed = parseMarkers(out);
    expect(parsed.found).toBe(true);
    if (!parsed.found) return;
    expect(parsed.before).toContain("<details>");
  });
});
