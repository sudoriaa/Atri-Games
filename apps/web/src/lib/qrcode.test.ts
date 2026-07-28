import { describe, expect, it } from "vitest";
import { encodeQR, QrPayloadTooLongError, qrToSvgPath } from "./qrcode";

/** Strips the quiet zone so tests can address raw module coordinates. */
function withoutQuiet(matrix: boolean[][], quiet = 4) {
  return matrix.slice(quiet, matrix.length - quiet).map((row) => row.slice(quiet, row.length - quiet));
}

function moduleAt(matrix: boolean[][], row: number, col: number): boolean {
  const modules = matrix[row];
  if (!modules || modules[col] === undefined) {
    throw new RangeError(`test module (${row}, ${col}) is outside the matrix`);
  }
  return modules[col];
}

function coordinateAt(values: readonly number[], index: number): number {
  const value = values[index];
  if (value === undefined) throw new RangeError(`test coordinate ${index} is missing`);
  return value;
}

/** Reads the 15-bit format string back from row 8 / column 8. */
function readFormatBits(modules: boolean[][]) {
  const size = modules.length;
  let bits = 0;
  for (let index = 0; index < 15; index += 1) {
    const bit = index < 8
      ? moduleAt(modules, 8, size - 1 - index)
      : moduleAt(modules, size - 15 + index, 8);
    if (bit) bits |= 1 << index;
  }
  return bits ^ 0b101010000010010;
}

describe("encodeQR", () => {
  it("picks version 1 for a short payload and sizes the matrix with the quiet zone", () => {
    const matrix = encodeQR("HI");
    expect(matrix.length).toBe(21 + 8);
    expect(matrix.every((row) => row.length === matrix.length)).toBe(true);
  });

  it("grows the version as the payload grows", () => {
    const small = encodeQR("a".repeat(14));
    const medium = encodeQR("a".repeat(60));
    const large = encodeQR("a".repeat(200));
    expect(small.length).toBe(21 + 8);
    expect(medium.length).toBeGreaterThan(small.length);
    expect(large.length).toBeGreaterThan(medium.length);
    expect((large.length - 8 - 17) % 4).toBe(0);
  });

  it("keeps the quiet zone free of dark modules", () => {
    const matrix = encodeQR("https://atri.games/games/neon-relay");
    const last = matrix.length - 1;
    for (let index = 0; index < matrix.length; index += 1) {
      for (let edge = 0; edge < 4; edge += 1) {
        expect(moduleAt(matrix, edge, index)).toBe(false);
        expect(moduleAt(matrix, last - edge, index)).toBe(false);
        expect(moduleAt(matrix, index, edge)).toBe(false);
        expect(moduleAt(matrix, index, last - edge)).toBe(false);
      }
    }
  });

  it("writes the three finder patterns", () => {
    const modules = withoutQuiet(encodeQR("https://atri.games/games/echo-vault"));
    const size = modules.length;
    const finders: ReadonlyArray<readonly [number, number]> = [
      [0, 0],
      [0, size - 7],
      [size - 7, 0],
    ];
    for (const [top, left] of finders) {
      for (let index = 0; index < 7; index += 1) {
        expect(moduleAt(modules, top, left + index)).toBe(true);
        expect(moduleAt(modules, top + 6, left + index)).toBe(true);
        expect(moduleAt(modules, top + index, left)).toBe(true);
        expect(moduleAt(modules, top + index, left + 6)).toBe(true);
      }
      expect(moduleAt(modules, top + 1, left + 1)).toBe(false);
      expect(moduleAt(modules, top + 3, left + 3)).toBe(true);
    }
  });

  it("writes the alternating timing patterns", () => {
    const modules = withoutQuiet(encodeQR("https://atri.games/games/paper-orbit"));
    for (let index = 8; index <= modules.length - 9; index += 1) {
      expect(moduleAt(modules, 6, index)).toBe(index % 2 === 0);
      expect(moduleAt(modules, index, 6)).toBe(index % 2 === 0);
    }
  });

  it("sets the fixed dark module below the lower-left finder", () => {
    const modules = withoutQuiet(encodeQR("dark module"));
    expect(moduleAt(modules, modules.length - 8, 8)).toBe(true);
  });

  it("encodes a decodable format string that reports EC level M", () => {
    const modules = withoutQuiet(encodeQR("https://atri.games/games/pixel-forge"));
    const format = readFormatBits(modules);
    const data = format >>> 10;
    expect(data >>> 3).toBe(0b00);
    expect(data & 0b111).toBeGreaterThanOrEqual(0);
    expect(data & 0b111).toBeLessThanOrEqual(7);
  });

  it("stores the same format string in both strips", () => {
    const modules = withoutQuiet(encodeQR("mirrored format"));
    const size = modules.length;
    const primary = [0, 1, 2, 3, 4, 5, 7, 8] as const;
    for (let index = 0; index < 15; index += 1) {
      const vertical = index < 8
        ? moduleAt(modules, coordinateAt(primary, index), 8)
        : moduleAt(modules, size - 15 + index, 8);
      const horizontal = index < 8
        ? moduleAt(modules, 8, size - 1 - index)
        : moduleAt(modules, 8, coordinateAt(primary, 14 - index));
      expect(vertical).toBe(horizontal);
    }
  });

  it("is deterministic for the same payload", () => {
    expect(encodeQR("https://atri.games/games/memory-tide")).toEqual(
      encodeQR("https://atri.games/games/memory-tide"),
    );
  });

  it("produces different matrices for different payloads", () => {
    expect(encodeQR("https://atri.games/games/a")).not.toEqual(encodeQR("https://atri.games/games/b"));
  });

  it("handles multi-byte UTF-8 payloads by their encoded length", () => {
    expect(encodeQR("霓".repeat(60)).length).toBeGreaterThan(21 + 8);
  });

  it("rejects a payload beyond the level M version 10 capacity", () => {
    expect(() => encodeQR("a".repeat(300))).toThrow(QrPayloadTooLongError);
  });

  it("honours a custom quiet zone", () => {
    const matrix = encodeQR("HI", { quiet: 0 });
    expect(matrix.length).toBe(21);
    expect(moduleAt(matrix, 0, 0)).toBe(true);
  });
});

describe("qrToSvgPath", () => {
  it("emits one unit square per dark module", () => {
    const matrix = encodeQR("HI", { quiet: 0 });
    const darkCount = matrix.flat().filter(Boolean).length;
    const path = qrToSvgPath(matrix);
    expect(path.match(/M/g)?.length).toBe(darkCount);
    expect(path.startsWith("M0 0h1v1h-1z")).toBe(true);
  });

  it("returns an empty string for an all-light matrix", () => {
    expect(qrToSvgPath([[false, false], [false, false]])).toBe("");
  });
});
