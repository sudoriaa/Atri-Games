import { constants, createDecipheriv, generateKeyPairSync, privateDecrypt } from "node:crypto";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import assert from "node:assert/strict";
import {
  ATRI_GCM_TAG_BYTES,
  ENCRYPTED_PACKAGE_CHUNK_SIZE,
  ENCRYPTED_PACKAGE_MAGIC,
  ENCRYPTED_PACKAGE_VERSION,
  encryptAtriArchive,
} from "../src/encryption.mjs";

function testKeyPair() {
  return generateKeyPairSync("rsa", {
    modulusLength: 2048,
    publicKeyEncoding: { type: "spki", format: "pem" },
    privateKeyEncoding: { type: "pkcs8", format: "pem" },
  });
}

function decryptContainer(container, privateKey) {
  assert.deepEqual(container.subarray(0, 8), ENCRYPTED_PACKAGE_MAGIC);
  assert.equal(container.readUInt8(8), ENCRYPTED_PACKAGE_VERSION);
  const wrappedKeyLength = container.readUInt32BE(9);
  const nonceLength = container.readUInt8(13);
  const tagLength = container.readUInt8(14);
  const chunkSize = container.readUInt32BE(15);
  const prefixLength = 19 + wrappedKeyLength + nonceLength;
  assert.equal(nonceLength, 12);
  assert.equal(tagLength, ATRI_GCM_TAG_BYTES);
  assert.equal(chunkSize, ENCRYPTED_PACKAGE_CHUNK_SIZE);
  assert.ok(prefixLength + 4 + tagLength <= container.length, "container body is truncated");

  const prefix = container.subarray(0, prefixLength);
  const wrappedKey = container.subarray(19, 19 + wrappedKeyLength);
  const baseNonce = container.subarray(19 + wrappedKeyLength, prefixLength);
  const contentKey = privateDecrypt(
    { key: privateKey, padding: constants.RSA_PKCS1_OAEP_PADDING, oaepHash: "sha256" },
    wrappedKey,
  );
  const frames = [];
  let offset = prefixLength;
  let frameIndex = 0;
  let foundFinalFrame = false;
  while (offset < container.length) {
    assert.ok(offset + 4 <= container.length, "frame length is truncated");
    const frameLength = container.readUInt32BE(offset);
    offset += 4;
    assert.ok(frameLength >= tagLength && offset + frameLength <= container.length, "frame is truncated");
    const sealed = container.subarray(offset, offset + frameLength);
    offset += frameLength;
    const nonce = Buffer.alloc(12);
    baseNonce.copy(nonce, 0, 0, 4);
    nonce.writeBigUInt64BE(BigInt(frameIndex), 4);
    const index = Buffer.alloc(8);
    index.writeBigUInt64BE(BigInt(frameIndex));
    const decipher = createDecipheriv("aes-256-gcm", contentKey, nonce, { authTagLength: tagLength });
    decipher.setAAD(Buffer.concat([prefix, index]));
    decipher.setAuthTag(sealed.subarray(sealed.length - tagLength));
    const plaintext = Buffer.concat([decipher.update(sealed.subarray(0, sealed.length - tagLength)), decipher.final()]);
    if (plaintext.length === 0) {
      assert.equal(sealed.length, tagLength, "only the final frame may be empty");
      assert.equal(offset, container.length, "final frame must terminate the container");
      foundFinalFrame = true;
      break;
    }
    assert.ok(plaintext.length <= chunkSize, "plaintext frame exceeds the declared chunk size");
    frames.push(plaintext);
    frameIndex += 1;
  }
  assert.equal(foundFinalFrame, true, "container has no authenticated final frame");
  for (let index = 0; index < frames.length - 1; index += 1) {
    assert.equal(frames[index]?.length, chunkSize, "only the final content frame may be short");
  }
  return Buffer.concat(frames);
}

test("ATRIENC1 encrypts a ZIP payload and authenticates the entire prefix as AAD", async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "atri-game-encryption-"));
  const inputPath = path.join(root, "package.zip");
  const outputPath = path.join(root, "package.atri");
  const archive = Buffer.concat([
    Buffer.from([0x50, 0x4b, 0x03, 0x04]),
    Buffer.alloc(ENCRYPTED_PACKAGE_CHUNK_SIZE + 47, "fixture ZIP payload"),
  ]);
  const { publicKey, privateKey } = testKeyPair();
  await writeFile(inputPath, archive);

  await encryptAtriArchive(inputPath, outputPath, publicKey);
  const encrypted = await readFile(outputPath);

  assert.equal(encrypted.subarray(0, 8).toString("ascii"), "ATRIENC1");
  assert.notDeepEqual(encrypted.subarray(0, 4), archive.subarray(0, 4));
  assert.equal(encrypted.readUInt8(14), ATRI_GCM_TAG_BYTES);
  assert.deepEqual(decryptContainer(encrypted, privateKey), archive);

  const tampered = Buffer.from(encrypted);
  const prefixLength = 19 + tampered.readUInt32BE(9) + tampered.readUInt8(13);
  // The nonce suffix is intentionally not used to derive per-frame nonces;
  // changing it therefore verifies that the full prefix is authenticated AAD.
  tampered[prefixLength - 1] ^= 1;
  assert.throws(() => decryptContainer(tampered, privateKey));
});
