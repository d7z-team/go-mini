import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { MiniGo } from "@d7z-team/mini-go/node";
import { LanguageService } from "@d7z-team/mini-go/tools";

test(
  "compiler restores an unacknowledged initial workspace after worker loss",
  { timeout: 90_000 },
  async () => {
    const image = await readFile(new URL("../dist/tools/compiler.json.gz", import.meta.url));
    const workspace = JSON.parse(
      await readFile(
        new URL("../../../../testdata/language/workspace.json", import.meta.url),
        "utf8",
      ),
    );
    const wasmUrl = new URL("../dist/wasm/mini_go_wasm_bg.wasm", import.meta.url);
    let lostOpens = 2;
    const language = await LanguageService.create(
      async (bytes, options) => {
        const runtime = await MiniGo.create(bytes, options);
        const start = runtime.start.bind(runtime);
        runtime.start = (entry, arguments_, callOptions) => {
          const execution = start(entry, arguments_, callOptions);
          const request = JSON.parse(new TextDecoder().decode(arguments_[0].data.String));
          if (lostOpens > 0 && request.Operation === "workspace/open") {
            lostOpens--;
            queueMicrotask(() => runtime.terminate(new Error("injected worker loss")));
          }
          return execution;
        };
        return runtime;
      },
      image,
      { wasmUrl },
    );
    image.fill(0);
    wasmUrl.pathname = "/mutated-by-caller.wasm";
    try {
      await assert.rejects(language.open(workspace), /injected worker loss/);
      const canceled = new AbortController();
      canceled.abort();
      await assert.rejects(language.analyze(canceled.signal), { name: "AbortError" });
      await assert.rejects(language.analyze(), /injected worker loss/);
      const analysis = await language.analyze();
      assert.equal(analysis.Revision, "1");
      const hover = await language.query("hover", {
        URI: workspace.Packages[0].Files[0].URI,
        Position: { line: 2, character: 6 },
      });
      assert.match(hover.contents.value, /Answer/);
      const replacement = await readFile(
        new URL("../dist/tools/compiler.json.gz", import.meta.url),
      );
      const upgrading = language.upgrade(replacement);
      replacement.fill(0);
      const queued = language.sources([]);
      await upgrading;
      assert.deepEqual((await queued).Packages, []);
      await assert.rejects(
        language.query("hover", {
          Snapshot: analysis.Snapshot,
          URI: workspace.Packages[0].Files[0].URI,
          Position: { line: 2, character: 6 },
        }),
        { code: "stale" },
      );
    } finally {
      await language.dispose();
    }
  },
);
