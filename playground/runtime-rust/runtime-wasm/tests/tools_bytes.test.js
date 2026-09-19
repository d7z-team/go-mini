import assert from "node:assert/strict";
import test from "node:test";
import { snapshotBytes } from "../dist/tools.js";
import { copyBytes } from "../dist/protocol.js";

test("binary input copies preserve visible bytes and caller ownership across transfer", () => {
  for (const source of [
    Uint8Array.of(1, 2, 3, 4).subarray(1, 3),
    Buffer.from([1, 2, 3, 4]).subarray(1, 3),
    Uint8Array.of(2, 3).buffer,
  ]) {
    const copy = copyBytes(source);
    const transferred = structuredClone(copy, { transfer: [copy.buffer] });
    assert.equal(copy.byteLength, 0);
    assert.deepEqual(transferred, Uint8Array.of(2, 3));
    transferred[0] = 99;
    const original = source instanceof ArrayBuffer ? new Uint8Array(source) : source;
    assert.deepEqual(Array.from(original), [2, 3]);
  }
});

test("compiler byte results preserve slice ranges and independent ownership", () => {
  for (const backing of [
    { String: Uint8Array.of(10, 20, 255, 40) },
    { Array: [10, 20, 255, 40].map((value) => ({ data: { Unsigned: BigInt(value) } })) },
  ]) {
    const slice = { storage: { object: 0n, path: [] }, start: 1n, length: 2n };
    const snapshot = { roots: [{ data: { Slice: slice } }], objects: [{ data: backing }] };
    const bytes = snapshotBytes(snapshot);
    assert.deepEqual(bytes, Uint8Array.of(20, 255));
    bytes[0] = 99;
    assert.deepEqual(snapshotBytes(snapshot), Uint8Array.of(20, 255));
    slice.start = 4n;
    slice.length = 0n;
    assert.deepEqual(snapshotBytes(snapshot), new Uint8Array());
    for (const [start, length] of [
      [-1n, 1n],
      [0n, -1n],
      [4n, 1n],
      [1n << 60n, 1n],
    ]) {
      slice.start = start;
      slice.length = length;
      assert.throws(() => snapshotBytes(snapshot), /invalid byte range/);
    }
  }
  const bytes = Uint8Array.of(0, 255);
  const result = snapshotBytes({ roots: [{ data: { String: bytes } }], objects: [] });
  result[0] = 9;
  assert.deepEqual(bytes, Uint8Array.of(0, 255));
});

test("compiler byte results reject missing and indirect storage", () => {
  assert.throws(() => snapshotBytes({ roots: [], objects: [] }), /missing byte result/);
  for (const path of [[], [0]]) {
    const snapshot = {
      roots: [{ data: { Slice: { storage: { object: 0n, path }, start: 0n, length: 1n } } }],
      objects: [],
    };
    assert.throws(() => snapshotBytes(snapshot), /byte storage/);
  }
});
