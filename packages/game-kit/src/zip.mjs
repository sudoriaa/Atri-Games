import { createReadStream } from "node:fs";
import { open, readdir, stat } from "node:fs/promises";
import path from "node:path";

const crcTable = new Uint32Array(256);
for (let n = 0; n < 256; n += 1) {
  let value = n;
  for (let bit = 0; bit < 8; bit += 1) value = (value & 1) ? 0xedb88320 ^ (value >>> 1) : value >>> 1;
  crcTable[n] = value >>> 0;
}

function crc32(buffer, previous = 0xffffffff) {
  let value = previous;
  for (const byte of buffer) value = crcTable[(value ^ byte) & 0xff] ^ (value >>> 8);
  return value;
}

async function scanFile(source) {
  let size = 0;
  let checksum = 0xffffffff;
  for await (const chunk of createReadStream(source)) {
    size += chunk.length;
    checksum = crc32(chunk, checksum);
  }
  return { size, checksum: (checksum ^ 0xffffffff) >>> 0 };
}

function header(signature, fields) {
  const buffer = Buffer.alloc(30);
  buffer.writeUInt32LE(signature, 0);
  buffer.writeUInt16LE(fields.versionNeeded ?? 20, 4);
  buffer.writeUInt16LE(fields.flags ?? 0, 6);
  buffer.writeUInt16LE(fields.compression ?? 0, 8);
  buffer.writeUInt16LE(0, 10);
  buffer.writeUInt16LE(0, 12);
  buffer.writeUInt32LE(fields.crc >>> 0, 14);
  buffer.writeUInt32LE(fields.size >>> 0, 18);
  buffer.writeUInt32LE(fields.size >>> 0, 22);
  buffer.writeUInt16LE(fields.nameLength, 26);
  buffer.writeUInt16LE(0, 28);
  return buffer;
}

async function writeAll(handle, value) {
  let offset = 0;
  while (offset < value.length) {
    const result = await handle.write(value, offset, value.length - offset);
    offset += result.bytesWritten;
  }
}

async function copyFile(handle, source) {
  for await (const chunk of createReadStream(source)) await writeAll(handle, chunk);
}

function centralHeader(entry) {
  const buffer = Buffer.alloc(46);
  buffer.writeUInt32LE(0x02014b50, 0);
  buffer.writeUInt16LE(20, 4);
  buffer.writeUInt16LE(20, 6);
  buffer.writeUInt16LE(0, 8);
  buffer.writeUInt16LE(0, 10);
  buffer.writeUInt16LE(0, 12);
  buffer.writeUInt16LE(0, 14);
  buffer.writeUInt32LE(entry.crc >>> 0, 16);
  buffer.writeUInt32LE(entry.size >>> 0, 20);
  buffer.writeUInt32LE(entry.size >>> 0, 24);
  buffer.writeUInt16LE(entry.nameBuffer.length, 28);
  buffer.writeUInt16LE(0, 30);
  buffer.writeUInt16LE(0, 32);
  buffer.writeUInt16LE(0, 34);
  buffer.writeUInt16LE(0, 36);
  buffer.writeUInt32LE(0, 38);
  buffer.writeUInt32LE(entry.offset >>> 0, 42);
  return buffer;
}

/**
 * Creates a small, deterministic, store-only ZIP archive. Store-only keeps
 * the builder dependency-free and lets already-compressed game assets pass
 * through without wasting CPU on the developer machine.
 */
export async function createZip(outputPath, entries) {
  const ordered = [...entries].sort((left, right) => (left.name < right.name ? -1 : left.name > right.name ? 1 : 0));
  const names = new Set();
  for (const entry of ordered) {
    const name = entry.name.replaceAll("\\", "/");
    if (!name || name.length > 0xffff || name.split("/").some((part) => !part || part === "." || part === "..")) {
      throw new Error(`invalid archive entry name: ${entry.name}`);
    }
    if (names.has(name)) throw new Error(`duplicate archive entry name: ${name}`);
    names.add(name);
  }
  if (ordered.length > 0xffff) throw new Error("ZIP packages cannot contain more than 65535 files");
  const output = await open(outputPath, "w");
  const central = [];
  let offset = 0;
  try {
    for (const entry of ordered) {
      const sourceInfo = await stat(entry.source);
      if (!sourceInfo.isFile()) throw new Error(`cannot package non-file entry: ${entry.source}`);
      const nameBuffer = Buffer.from(entry.name.replaceAll("\\", "/"));
      if (nameBuffer.length === 0 || nameBuffer.length > 0xffff) throw new Error(`invalid archive entry name: ${entry.name}`);
      const scanned = await scanFile(entry.source);
      if (scanned.size > 0xffffffff || offset > 0xffffffff - scanned.size) {
        throw new Error("ZIP package exceeds the classic ZIP 4 GiB limit");
      }
      const local = header(0x04034b50, { crc: scanned.checksum, size: scanned.size, nameLength: nameBuffer.length });
      await writeAll(output, local);
      await writeAll(output, nameBuffer);
      await copyFile(output, entry.source);
      central.push({ ...scanned, nameBuffer, offset });
      offset += local.length + nameBuffer.length + scanned.size;
    }
    const centralOffset = offset;
    for (const entry of central) {
      const value = centralHeader(entry);
      await writeAll(output, value);
      await writeAll(output, entry.nameBuffer);
      offset += value.length + entry.nameBuffer.length;
    }
    const end = Buffer.alloc(22);
    end.writeUInt32LE(0x06054b50, 0);
    end.writeUInt16LE(0, 4);
    end.writeUInt16LE(0, 6);
    end.writeUInt16LE(central.length, 8);
    end.writeUInt16LE(central.length, 10);
    end.writeUInt32LE(offset - centralOffset, 12);
    end.writeUInt32LE(centralOffset, 16);
    await writeAll(output, end);
  } finally {
    await output.close();
  }
}

export async function collectFiles(rootDir) {
  const result = [];
  async function walk(current, prefix) {
    const items = await readdir(current, { withFileTypes: true });
    items.sort((left, right) => (left.name < right.name ? -1 : left.name > right.name ? 1 : 0));
    for (const item of items) {
      const source = path.join(current, item.name);
      const name = prefix ? `${prefix}/${item.name}` : item.name;
      if (item.isSymbolicLink()) throw new Error(`symbolic links are not allowed in packages: ${name}`);
      if (item.isDirectory()) await walk(source, name);
      else if (item.isFile()) result.push({ source, name });
      else throw new Error(`unsupported filesystem entry: ${name}`);
    }
  }
  await walk(rootDir, "");
  return result;
}
