// This scenario runs unchanged in Node and browser workers.
export async function exerciseCompiler(MiniGo, values, image) {
  const vm = await MiniGo.create(image, {
    workload: "compiler",
    maxSteps: 5_000_000,
    signal: AbortSignal.timeout(20_000),
  });
  try {
    let envelope;
    for (let i = 0; i < 3; i++) {
      const request =
        i === 0
          ? {}
          : {
              Format: envelope.Format,
              Version: envelope.Version,
              Operation: "check",
              Root: "probe",
              Packages: [
                {
                  Namespace: "module:probe",
                  PackagePath: "",
                  ModulePath: "probe",
                  Files: [{ Path: "main.mgo", Text: "package main\nfunc main() {}\n" }],
                },
              ],
            };
      const execution = vm.start("default", [
        values.bytes(new TextEncoder().encode(JSON.stringify(request))),
      ]);
      const snapshot = await execution.result;
      await execution.settled;
      const data = snapshot.roots[0].data;
      let bytes;
      if ("String" in data) bytes = data.String;
      else {
        const { storage, start, length } = data.Slice;
        if (storage.path.length) throw new Error("unexpected compiler result address");
        const backing = snapshot.objects[Number(storage.object)].data;
        bytes =
          "String" in backing
            ? backing.String
            : Uint8Array.from(backing.Array, (value) => Number(value.data.Unsigned));
        bytes = bytes.slice(Number(start), Number(start + length));
      }
      envelope = JSON.parse(new TextDecoder().decode(bytes));
      if (i === 0 ? !envelope.Error : envelope.Error || envelope.Diagnostics?.length) {
        throw new Error(`compiler response: ${JSON.stringify(envelope)}`);
      }
    }
    const stats = await vm.stats();
    if (stats.activeScopes !== 0n || stats.tasks !== 0n || stats.ffiCalls !== 0n)
      throw new Error("compiler work retained after settlement");
    await vm.close();
  } finally {
    vm.terminate();
  }
}
