/**
 * Minimal QR Code encoder (ISO/IEC 18004) for byte-mode payloads.
 *
 * The repository deliberately avoids runtime dependencies for anything a game
 * package or the portal ships to a browser, so the share panel encodes its own
 * codes. Scope is intentionally narrow: byte mode, error-correction level M,
 * versions 1 through 10 (up to 213 bytes). Game URLs are far shorter than that.
 *
 * The output is a square boolean matrix where `true` is a dark module.
 */

/** Data codeword capacity per version at level M, index 0 = version 1. */
const DATA_CODEWORDS_M = [16, 28, 44, 64, 86, 108, 124, 154, 182, 216] as const;

/** Error-correction blocks per version at level M: [count, dataCodewords][]. */
const EC_BLOCKS_M: ReadonlyArray<ReadonlyArray<readonly [number, number]>> = [
  [[1, 16]],
  [[1, 28]],
  [[1, 44]],
  [[2, 32]],
  [[2, 43]],
  [[4, 27]],
  [[4, 31]],
  [[2, 38], [2, 39]],
  [[3, 36], [2, 37]],
  [[4, 43], [1, 44]],
];

/** EC codewords per block per version at level M. */
const EC_CODEWORDS_PER_BLOCK_M = [10, 16, 26, 18, 24, 16, 18, 22, 22, 26] as const;

/** Alignment pattern centre coordinates per version. */
const ALIGNMENT_CENTERS: ReadonlyArray<readonly number[]> = [
  [],
  [6, 18],
  [6, 22],
  [6, 26],
  [6, 30],
  [6, 34],
  [6, 22, 38],
  [6, 24, 42],
  [6, 26, 46],
  [6, 28, 50],
];

// --- GF(256) arithmetic for Reed-Solomon -----------------------------------

const GF_EXP = new Uint8Array(512);
const GF_LOG = new Uint8Array(256);

(function initGaloisField() {
  let value = 1;
  for (let index = 0; index < 255; index += 1) {
    GF_EXP[index] = value;
    GF_LOG[value] = index;
    value <<= 1;
    // 0x11d is the QR primitive polynomial x^8 + x^4 + x^3 + x^2 + 1.
    if (value & 0x100) value ^= 0x11d;
  }
  for (let index = 255; index < 512; index += 1) {
    GF_EXP[index] = tableAt(GF_EXP, index - 255, "GF exponent");
  }
})();

function gfMultiply(a: number, b: number): number {
  if (a === 0 || b === 0) return 0;
  return tableAt(GF_EXP, tableAt(GF_LOG, a, "GF logarithm") + tableAt(GF_LOG, b, "GF logarithm"), "GF exponent");
}

/** Builds the RS generator polynomial of the given degree. */
function generatorPolynomial(degree: number): Uint8Array {
  let poly = new Uint8Array([1]);
  for (let index = 0; index < degree; index += 1) {
    const next = new Uint8Array(poly.length + 1);
    for (let term = 0; term < poly.length; term += 1) {
      next[term] = tableAt(next, term, "generator coefficient") ^ tableAt(poly, term, "generator coefficient");
      next[term + 1] = tableAt(next, term + 1, "generator coefficient") ^
        gfMultiply(tableAt(poly, term, "generator coefficient"), tableAt(GF_EXP, index, "GF exponent"));
    }
    poly = next;
  }
  return poly;
}

/** Returns the EC codewords for one data block. */
function errorCorrection(data: Uint8Array, ecLength: number): Uint8Array {
  const generator = generatorPolynomial(ecLength);
  const remainder = new Uint8Array(ecLength);
  for (const byte of data) {
    const factor = byte ^ tableAt(remainder, 0, "RS remainder");
    remainder.copyWithin(0, 1);
    remainder[ecLength - 1] = 0;
    if (factor !== 0) {
      for (let index = 0; index < ecLength; index += 1) {
        remainder[index] = tableAt(remainder, index, "RS remainder") ^
          gfMultiply(tableAt(generator, index + 1, "RS generator"), factor);
      }
    }
  }
  return remainder;
}

// --- BCH codes for format and version information --------------------------

function bch(value: number, generator: number, generatorBits: number): number {
  let remainder = value;
  while (bitLength(remainder) >= generatorBits) {
    remainder ^= generator << (bitLength(remainder) - generatorBits);
  }
  return remainder;
}

function bitLength(value: number): number {
  let length = 0;
  while (value >>> length) length += 1;
  return length;
}

/** 15-bit format information: 2 bits EC level (M = 0b00) + 3 bits mask. */
function formatBits(mask: number): number {
  const data = (0b00 << 3) | mask;
  return ((data << 10) | bch(data << 10, 0b10100110111, 11)) ^ 0b101010000010010;
}

/** 18-bit version information, required for version 7 and above. */
function versionBits(version: number): number {
  return (version << 12) | bch(version << 12, 0b1111100100101, 13);
}

// --- Bit buffer -------------------------------------------------------------

class BitBuffer {
  private readonly bits: number[] = [];

  push(value: number, length: number) {
    for (let index = length - 1; index >= 0; index -= 1) {
      this.bits.push((value >>> index) & 1);
    }
  }

  get length() {
    return this.bits.length;
  }

  /** Pads to a byte boundary and returns the codeword sequence. */
  toCodewords(capacityBits: number): Uint8Array {
    if (this.bits.length > capacityBits) {
      throw new RangeError(`QR bit buffer exceeds its ${capacityBits}-bit capacity`);
    }
    // Terminator, at most four zero bits.
    const terminator = Math.min(4, capacityBits - this.bits.length);
    for (let index = 0; index < terminator; index += 1) this.bits.push(0);
    while (this.bits.length % 8 !== 0) this.bits.push(0);

    const codewords = new Uint8Array(capacityBits / 8);
    for (let index = 0; index < this.bits.length; index += 1) {
      if (tableAt(this.bits, index, "QR bit buffer")) {
        const codewordIndex = index >>> 3;
        codewords[codewordIndex] = tableAt(codewords, codewordIndex, "data codeword") | (0x80 >>> (index & 7));
      }
    }
    // Alternating pad codewords fill the remainder.
    const padBytes = [0xec, 0x11] as const;
    for (let index = this.bits.length / 8, pad = 0; index < codewords.length; index += 1, pad += 1) {
      codewords[index] = tableAt(padBytes, pad % 2, "pad codeword");
    }
    return codewords;
  }
}

// --- Matrix construction ----------------------------------------------------

// Module values are a two-bit field: bit 0 is darkness, bit 1 marks a function
// pattern. So 0 = light data, 1 = dark data, 2 = light function, 3 = dark
// function. Keeping darkness in bit 0 lets every reader use `& 1`, while
// `>= 2` identifies the cells the data and mask passes must leave alone.
type Matrix = Uint8Array[];

function rowAt(matrix: Matrix, row: number): Uint8Array {
  const value = matrix[row];
  if (!value) throw new RangeError(`QR row ${row} is outside the matrix`);
  return value;
}

function moduleAt(matrix: Matrix, row: number, col: number): number {
  const value = rowAt(matrix, row)[col];
  if (value === undefined) throw new RangeError(`QR column ${col} is outside row ${row}`);
  return value;
}

function setModule(matrix: Matrix, row: number, col: number, value: number) {
  const target = rowAt(matrix, row);
  if (target[col] === undefined) throw new RangeError(`QR column ${col} is outside row ${row}`);
  target[col] = value;
}

function tableAt<T>(values: ArrayLike<T>, index: number, label: string): T {
  const value = values[index];
  if (value === undefined) throw new RangeError(`${label} index ${index} is out of range`);
  return value;
}

function createMatrix(size: number): Matrix {
  return Array.from({ length: size }, () => new Uint8Array(size));
}

function finderPattern(m: Matrix, row: number, col: number) {
  for (let dr = -1; dr <= 7; dr += 1) {
    for (let dc = -1; dc <= 7; dc += 1) {
      const r = row + dr;
      const c = col + dc;
      if (r < 0 || r >= m.length || c < 0 || c >= m.length) continue;
      const dark =
        dr >= 0 && dr <= 6 && dc >= 0 && dc <= 6 &&
        (dr === 0 || dr === 6 || dc === 0 || dc === 6 || (dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4));
      setModule(m, r, c, dark ? 3 : 2);
    }
  }
}

function timingPattern(m: Matrix) {
  for (let index = 8; index <= m.length - 9; index += 1) {
    const value = index % 2 === 0 ? 3 : 2;
    setModule(m, 6, index, value);
    setModule(m, index, 6, value);
  }
}

function alignmentPatterns(m: Matrix, centers: readonly number[]) {
  for (const cr of centers) {
    for (const cc of centers) {
      // Centres overlapping a finder pattern are skipped by the spec.
      if (moduleAt(m, cr, cc) !== 0) continue;
      for (let dr = -2; dr <= 2; dr += 1) {
        for (let dc = -2; dc <= 2; dc += 1) {
          const dark = Math.abs(dr) === 2 || Math.abs(dc) === 2 || (dr === 0 && dc === 0);
          setModule(m, cr + dr, cc + dc, dark ? 3 : 2);
        }
      }
    }
  }
}

/** Reserves the format-information strips and sets the fixed dark module. */
function reserveFormatAreas(m: Matrix) {
  const size = m.length;
  for (let index = 0; index <= 8; index += 1) {
    if (moduleAt(m, 8, index) === 0) setModule(m, 8, index, 2);
    if (moduleAt(m, index, 8) === 0) setModule(m, index, 8, 2);
    if (moduleAt(m, 8, size - 1 - index) === 0) setModule(m, 8, size - 1 - index, 2);
    if (moduleAt(m, size - 1 - index, 8) === 0) setModule(m, size - 1 - index, 8, 2);
  }
  setModule(m, size - 8, 8, 3);
}

function placeVersionInfo(m: Matrix, version: number) {
  if (version < 7) return;
  const bits = versionBits(version);
  const size = m.length;
  for (let index = 0; index < 18; index += 1) {
    const value = (bits >>> index) & 1 ? 3 : 2;
    const row = Math.floor(index / 3);
    const col = index % 3;
    setModule(m, row, size - 11 + col, value);
    setModule(m, size - 11 + col, row, value);
  }
}

/** Walks the two-module-wide zigzag from the bottom-right, skipping column 6. */
function placeData(m: Matrix, codewords: Uint8Array) {
  const size = m.length;
  const totalBits = codewords.length * 8;
  let bitIndex = 0;
  let upward = true;
  for (let col = size - 1; col > 0; col -= 2) {
    if (col === 6) col -= 1;
    for (let offset = 0; offset < size; offset += 1) {
      const row = upward ? size - 1 - offset : offset;
      for (let inner = 0; inner <= 1; inner += 1) {
        const c = col - inner;
        if (moduleAt(m, row, c) !== 0) continue;
        const bit = bitIndex < totalBits
          ? (tableAt(codewords, bitIndex >>> 3, "interleaved codeword") >>> (7 - (bitIndex & 7))) & 1
          : 0;
        setModule(m, row, c, bit);
        bitIndex += 1;
      }
    }
    upward = !upward;
  }
}

const MASK_PREDICATES: ReadonlyArray<(row: number, col: number) => boolean> = [
  (row, col) => (row + col) % 2 === 0,
  (row) => row % 2 === 0,
  (_row, col) => col % 3 === 0,
  (row, col) => (row + col) % 3 === 0,
  (row, col) => (Math.floor(row / 2) + Math.floor(col / 3)) % 2 === 0,
  (row, col) => ((row * col) % 2) + ((row * col) % 3) === 0,
  (row, col) => (((row * col) % 2) + ((row * col) % 3)) % 2 === 0,
  (row, col) => (((row + col) % 2) + ((row * col) % 3)) % 2 === 0,
];

function applyMask(m: Matrix, mask: number) {
  const predicate = tableAt(MASK_PREDICATES, mask, "QR mask");
  for (let row = 0; row < m.length; row += 1) {
    for (let col = 0; col < m.length; col += 1) {
      const value = moduleAt(m, row, col);
      if (value < 2 && predicate(row, col)) setModule(m, row, col, value ^ 1);
    }
  }
}

function placeFormatBits(m: Matrix, mask: number) {
  const bits = formatBits(mask);
  const size = m.length;
  // Bit i of the format string maps to these coordinates along row 8 and
  // column 8; the split skips the timing module at index 6 and the dark module.
  const primary = [0, 1, 2, 3, 4, 5, 7, 8] as const;
  for (let index = 0; index < 15; index += 1) {
    const value = (bits >>> index) & 1 ? 3 : 2;
    if (index < 8) {
      setModule(m, 8, size - 1 - index, value);
      setModule(m, tableAt(primary, index, "format coordinate"), 8, value);
    } else {
      setModule(m, 8, tableAt(primary, 14 - index, "format coordinate"), value);
      setModule(m, size - 15 + index, 8, value);
    }
  }
  setModule(m, size - 8, 8, 3);
}

/** ISO/IEC 18004 penalty rules 1 through 4, used to pick the best mask. */
function penaltyScore(m: Matrix): number {
  const size = m.length;
  let score = 0;

  for (let line = 0; line < size; line += 1) {
    for (const horizontal of [true, false]) {
      let run = 1;
      let previous = (horizontal ? moduleAt(m, line, 0) : moduleAt(m, 0, line)) & 1;
      for (let index = 1; index < size; index += 1) {
        const value = (horizontal ? moduleAt(m, line, index) : moduleAt(m, index, line)) & 1;
        if (value === previous) {
          run += 1;
          continue;
        }
        if (run >= 5) score += 3 + run - 5;
        run = 1;
        previous = value;
      }
      if (run >= 5) score += 3 + run - 5;
    }
  }

  for (let row = 0; row < size - 1; row += 1) {
    for (let col = 0; col < size - 1; col += 1) {
      const value = moduleAt(m, row, col) & 1;
      if ((moduleAt(m, row, col + 1) & 1) === value && (moduleAt(m, row + 1, col) & 1) === value && (moduleAt(m, row + 1, col + 1) & 1) === value) {
        score += 3;
      }
    }
  }

  const finderLike = [1, 0, 1, 1, 1, 0, 1, 0, 0, 0, 0];
  const reversed = [...finderLike].reverse();
  for (let line = 0; line < size; line += 1) {
    for (let start = 0; start <= size - 11; start += 1) {
      const row = (offset: number) => moduleAt(m, line, start + offset) & 1;
      const col = (offset: number) => moduleAt(m, start + offset, line) & 1;
      if (finderLike.every((v, i) => row(i) === v) || reversed.every((v, i) => row(i) === v)) score += 40;
      if (finderLike.every((v, i) => col(i) === v) || reversed.every((v, i) => col(i) === v)) score += 40;
    }
  }

  let dark = 0;
  for (let row = 0; row < size; row += 1) {
    for (let col = 0; col < size; col += 1) if (moduleAt(m, row, col) & 1) dark += 1;
  }
  const percent = (dark * 100) / (size * size);
  const deviation = Math.floor(Math.abs(percent - 50) / 5);
  score += deviation * 10;

  return score;
}

// --- Public API -------------------------------------------------------------

export interface QrOptions {
  /** Quiet-zone width in modules. The spec recommends 4. */
  quiet?: number;
}

export class QrPayloadTooLongError extends RangeError {
  constructor(byteLength: number) {
    super(`二维码内容过长：${byteLength} 字节，上限 213 字节`);
    this.name = "QrPayloadTooLongError";
  }
}

/**
 * Encodes `text` as a QR Code and returns a square matrix of booleans where
 * `true` is a dark module. Byte mode, EC level M, versions 1 through 10.
 */
export function encodeQR(text: string, options: QrOptions = {}): boolean[][] {
  const quiet = options.quiet ?? 4;
  const bytes = new TextEncoder().encode(text);

  const versionIndex = DATA_CODEWORDS_M.findIndex((capacity, index) => {
    const version = index + 1;
    const countBits = version < 10 ? 8 : 16;
    return 4 + countBits + bytes.length * 8 <= capacity * 8;
  });
  if (versionIndex < 0) throw new QrPayloadTooLongError(bytes.length);
  const version = versionIndex + 1;

  const buffer = new BitBuffer();
  buffer.push(0b0100, 4); // byte mode
  buffer.push(bytes.length, version < 10 ? 8 : 16);
  for (const byte of bytes) buffer.push(byte, 8);

  const totalData = tableAt(DATA_CODEWORDS_M, versionIndex, "data capacity");
  const codewords = buffer.toCodewords(totalData * 8);
  const ecPerBlock = tableAt(EC_CODEWORDS_PER_BLOCK_M, versionIndex, "EC block size");
  const blockSpec = tableAt(EC_BLOCKS_M, versionIndex, "EC block specification");

  const dataBlocks: Uint8Array[] = [];
  const ecBlocks: Uint8Array[] = [];
  let offset = 0;
  for (const [count, blockLength] of blockSpec) {
    for (let block = 0; block < count; block += 1) {
      const slice = codewords.slice(offset, offset + blockLength);
      dataBlocks.push(slice);
      ecBlocks.push(errorCorrection(slice, ecPerBlock));
      offset += blockLength;
    }
  }

  // Interleave data codewords column-wise, then all EC codewords.
  const interleaved: number[] = [];
  const longestBlock = Math.max(...dataBlocks.map((block) => block.length));
  for (let index = 0; index < longestBlock; index += 1) {
    for (const block of dataBlocks) if (index < block.length) interleaved.push(tableAt(block, index, "data block"));
  }
  for (let index = 0; index < ecPerBlock; index += 1) {
    for (const block of ecBlocks) interleaved.push(tableAt(block, index, "EC block"));
  }
  const finalCodewords = new Uint8Array(interleaved);

  const size = version * 4 + 17;
  const template = createMatrix(size);
  finderPattern(template, 0, 0);
  finderPattern(template, 0, size - 7);
  finderPattern(template, size - 7, 0);
  timingPattern(template);
  alignmentPatterns(template, tableAt(ALIGNMENT_CENTERS, versionIndex, "alignment centers"));
  reserveFormatAreas(template);
  placeVersionInfo(template, version);

  let best: Matrix = template;
  let bestScore = Number.POSITIVE_INFINITY;
  for (let mask = 0; mask < 8; mask += 1) {
    const candidate = template.map((row) => new Uint8Array(row));
    placeData(candidate, finalCodewords);
    applyMask(candidate, mask);
    placeFormatBits(candidate, mask);
    const score = penaltyScore(candidate);
    if (score < bestScore) {
      bestScore = score;
      best = candidate;
    }
  }

  const side = size + quiet * 2;
  return Array.from({ length: side }, (_, row) =>
    Array.from({ length: side }, (_, col) => {
      const r = row - quiet;
      const c = col - quiet;
      if (r < 0 || r >= size || c < 0 || c >= size) return false;
      return (moduleAt(best, r, c) & 1) === 1;
    }),
  );
}

/** Renders a matrix into an SVG path string, one unit per module. */
export function qrToSvgPath(matrix: boolean[][]): string {
  const parts: string[] = [];
  for (let row = 0; row < matrix.length; row += 1) {
    const modules = tableAt(matrix, row, "SVG row");
    for (let col = 0; col < modules.length; col += 1) {
      if (tableAt(modules, col, "SVG module")) parts.push(`M${col} ${row}h1v1h-1z`);
    }
  }
  return parts.join("");
}

