import { readFile } from "node:fs/promises";
import { parentPort } from "node:worker_threads";
import init from "./wasm/mini_go_wasm.js";
import { runWorker } from "./worker.js";

if (!parentPort) throw new Error("runtime worker requires a parent message port");
const port = parentPort;
runWorker(
  {
    send: (message) => port.postMessage(message),
    listen: (handler) => {
      port.on("message", handler);
    },
  },
  async (override) => {
    const url = override
      ? new URL(override)
      : new URL("./wasm/mini_go_wasm_bg.wasm", import.meta.url);
    return init({ module_or_path: url.protocol === "file:" ? await readFile(url) : url });
  },
  // Yield to the next event-loop turn so parent-port control replies are not
  // starved by a continuously runnable VM on another MessagePort.
  (callback) => setImmediate(callback),
);
