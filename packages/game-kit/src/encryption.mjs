import { constants, createCipheriv, publicEncrypt, randomBytes } from "node:crypto";
import { createReadStream } from "node:fs";
import { lstat, mkdtemp, open, rename, rm } from "node:fs/promises";
import path from "node:path";

export const ENCRYPTED_PACKAGE_MAGIC = Buffer.from("ATRIENC1", "ascii");
export const ENCRYPTED_PACKAGE_VERSION = 1;
export const ENCRYPTED_PACKAGE_CHUNK_SIZE = 1024 * 1024;
// Compatibility aliases for early game-kit consumers.
export const ATRI_ENCRYPTED_MAGIC = ENCRYPTED_PACKAGE_MAGIC;
export const ATRI_ENCRYPTED_VERSION = ENCRYPTED_PACKAGE_VERSION;
export const ATRI_CONTENT_KEY_BYTES = 32;
export const ATRI_GCM_NONCE_BYTES = 12;
export const ATRI_GCM_TAG_BYTES = 16;

// Fixed prefix: magic (8) + version (1) + wrapped-key length (uint32 BE) +
// nonce length (uint8) + GCM tag length (uint8) + chunk size (uint32 BE).
// The wrapped key and base nonce immediately follow, and the whole prefix is
// AES-GCM additional data for every frame.
const FIXED_PREFIX_BYTES = 19;

function encryptedPrefix(wrappedKey, baseNonce) {
  if (wrappedKey.length > 0xffffffff) throw new Error("wrapped content key is too large for ATRIENC1");
  const header = Buffer.alloc(FIXED_PREFIX_BYTES);
  ENCRYPTED_PACKAGE_MAGIC.copy(header, 0);
  header.writeUInt8(ENCRYPTED_PACKAGE_VERSION, 8);
  header.writeUInt32BE(wrappedKey.length, 9);
  header.writeUInt8(baseNonce.length, 13);
  header.writeUInt8(ATRI_GCM_TAG_BYTES, 14);
  header.writeUInt32BE(ENCRYPTED_PACKAGE_CHUNK_SIZE, 15);
  return Buffer.concat([header, wrappedKey, baseNonce]);
}

function wrapContentKey(contentKey, publicKey) {
  // Leaving oaepLabel unset deliberately uses RSA-OAEP's default null/empty
  // label. The server uses the same OAEP-SHA256 parameters when unwrapping.
  return publicEncrypt(
    {
      key: publicKey,
      padding: constants.RSA_PKCS1_OAEP_PADDING,
      oaepHash: "sha256",
    },
    contentKey,
  );
}

async function replaceAtomically(outputPath, temporaryPath) {
  await rename(temporaryPath, outputPath);
}

async function writeAll(handle, value) {
  let offset = 0;
  while (offset < value.length) {
    const { bytesWritten } = await handle.write(value, offset, value.length - offset, null);
    if (bytesWritten <= 0) throw new Error("failed to write encrypted package frame");
    offset += bytesWritten;
  }
}

function frameNonce(baseNonce, frameIndex) {
  const nonce = Buffer.alloc(ATRI_GCM_NONCE_BYTES);
  baseNonce.copy(nonce, 0, 0, 4);
  nonce.writeBigUInt64BE(BigInt(frameIndex), 4);
  return nonce;
}

function frameAAD(prefix, frameIndex) {
  const index = Buffer.alloc(8);
  index.writeBigUInt64BE(BigInt(frameIndex));
  return Buffer.concat([prefix, index]);
}

function sealFrame(contentKey, baseNonce, prefix, frameIndex, plaintext) {
  if (plaintext.length > ENCRYPTED_PACKAGE_CHUNK_SIZE) throw new Error("ATRIENC1 plaintext frame exceeds the configured chunk size");
  const cipher = createCipheriv("aes-256-gcm", contentKey, frameNonce(baseNonce, frameIndex), { authTagLength: ATRI_GCM_TAG_BYTES });
  cipher.setAAD(frameAAD(prefix, frameIndex));
  return Buffer.concat([cipher.update(plaintext), cipher.final(), cipher.getAuthTag()]);
}

async function writeFrame(handle, contentKey, baseNonce, prefix, frameIndex, plaintext) {
  const sealed = sealFrame(contentKey, baseNonce, prefix, frameIndex, plaintext);
  if (sealed.length > 0xffffffff) throw new Error("ATRIENC1 encrypted frame is too large");
  const length = Buffer.alloc(4);
  length.writeUInt32BE(sealed.length, 0);
  await writeAll(handle, length);
  await writeAll(handle, sealed);
}

/**
 * Encrypt a ZIP archive as an ATRIENC1 container without buffering the game
 * archive in memory. The format is:
 *
 *   ATRIENC1 | version | uint32BE(wrappedKeyLength) | nonceLength | tagLength
 *   uint32BE(chunkSize) | wrapped RSA-OAEP-SHA256 AES-256 key | base nonce
 *   repeated uint32BE(frameLength) | AES-GCM(ciphertext + tag)
 *   final authenticated empty frame
 *
 * Every non-terminal plaintext frame is exactly 1 MiB; only the final content
 * frame may be shorter before the authenticated empty terminator. Its nonce
 * is baseNonce[0:4] followed by uint64BE(frameIndex), and its AAD is the
 * prefix through the base nonce followed by uint64BE(frameIndex). This keeps
 * 512 MiB packages streamable on constrained importer processes without
 * weakening header authentication.
 */
export async function encryptAtriArchive(inputPath, outputPath, publicKey) {
  const source = await lstat(inputPath);
  if (!source.isFile() || source.isSymbolicLink()) throw new Error(`cannot encrypt non-file archive: ${inputPath}`);
  if (path.resolve(inputPath) === path.resolve(outputPath)) throw new Error("encrypted output must differ from the ZIP input");

  const contentKey = randomBytes(ATRI_CONTENT_KEY_BYTES);
  const baseNonce = randomBytes(ATRI_GCM_NONCE_BYTES);
  const wrappedKey = wrapContentKey(contentKey, publicKey);
  const prefix = encryptedPrefix(wrappedKey, baseNonce);
  const outputDirectory = path.dirname(outputPath);
  const temporaryDirectory = await mkdtemp(path.join(outputDirectory, `.${path.basename(outputPath)}-encrypt-`));
  const temporaryPath = path.join(temporaryDirectory, "package.atrienc");

  try {
    const output = await open(temporaryPath, "wx", 0o600);
    try {
      await writeAll(output, prefix);
      let frameIndex = 0;
      const writeContentFrame = async (plaintext) => {
        await writeFrame(output, contentKey, baseNonce, prefix, frameIndex, plaintext);
        frameIndex += 1;
      };
      let pending = Buffer.alloc(0);
      for await (const sourceChunk of createReadStream(inputPath, { highWaterMark: ENCRYPTED_PACKAGE_CHUNK_SIZE })) {
        const chunk = Buffer.from(sourceChunk);
        let offset = 0;
        if (pending.length > 0) {
          const needed = ENCRYPTED_PACKAGE_CHUNK_SIZE - pending.length;
          if (chunk.length < needed) {
            pending = Buffer.concat([pending, chunk]);
            continue;
          }
          await writeContentFrame(Buffer.concat([pending, chunk.subarray(0, needed)]));
          pending = Buffer.alloc(0);
          offset = needed;
        }
        while (offset + ENCRYPTED_PACKAGE_CHUNK_SIZE <= chunk.length) {
          await writeContentFrame(chunk.subarray(offset, offset + ENCRYPTED_PACKAGE_CHUNK_SIZE));
          offset += ENCRYPTED_PACKAGE_CHUNK_SIZE;
        }
        if (offset < chunk.length) {
          // Hold a partial read until EOF so the importer sees only full
          // content frames, except for the optional final content frame.
          pending = Buffer.from(chunk.subarray(offset));
        }
      }
      if (pending.length > 0) await writeContentFrame(pending);
      // This authenticated zero-length frame is the unambiguous end marker.
      await writeFrame(output, contentKey, baseNonce, prefix, frameIndex, Buffer.alloc(0));
      await output.sync();
    } finally {
      await output.close();
    }
    await replaceAtomically(outputPath, temporaryPath);
  } finally {
    await rm(temporaryDirectory, { recursive: true, force: true });
  }
}
