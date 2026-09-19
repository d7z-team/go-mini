import { Runtime, values } from "./runtime.js";
import { copyBytes } from "./protocol.js";
import type { HostRequest, Options, Snapshot } from "./types.js";

export interface SourceFile {
  Path: string;
  Text: string;
  OriginPath?: string;
  URI?: string;
}
export interface SourcePackage {
  Editable?: boolean | null;
  Namespace: string;
  PackagePath?: string;
  ModulePath: string;
  Files: SourceFile[];
  TestFiles?: SourceFile[];
  SelectionTarget?: { tags?: string[] | null };
  SourceCandidates?:
    | {
        Path: string;
        Hash: string;
        Size: number;
        Selected: boolean;
        Test: boolean;
      }[]
    | null;
  Resources?: { Path: string; Data: string | null; SourcePath?: string }[];
}
export interface SourceTree {
  ModulePath: string;
  Editable?: boolean | null;
  Files: { Path: string; Data: string; URI?: string }[];
}
export interface SourcePackages {
  Packages: SourcePackage[];
}
export interface WorkspaceInput {
  Root: string;
  Tags?: string[];
  Packages: SourcePackage[];
}
export interface Position {
  line: number;
  character: number;
}
export interface Range {
  start: Position;
  end: Position;
}
export interface DocumentUpdate {
  Operation: "open" | "change" | "close";
  Identity: { URI: string; ModulePath?: string; Path?: string };
  Version?: number;
  Text?: string;
  Changes?: { range?: Range; text: string }[];
}
export interface Analysis {
  Revision: string;
  Snapshot: string;
  CheckedPackages: number;
  ReusedPackages: number;
  Diagnostics: Record<string, unknown>;
  WorkspaceDiagnostics?: unknown[];
}
export interface ToolsResponse {
  Format: string;
  Version: number;
  CompilerID: string;
  Session: string;
  Revision?: string;
  Analysis?: Analysis;
  Value?: unknown;
  ImageJSON?: string;
  SymbolsJSON?: string;
  Sources?: Record<string, { module: string; path: string; text: string }>;
  Diagnostics?: unknown[];
  Error?: { Code: string; Message: string } | null;
  Recovery?: Record<string, unknown> | null;
}
export class ToolsError extends Error {
  constructor(
    readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = "ToolsError";
  }
}
export type RuntimeFactory = (
  image: Uint8Array | ArrayBuffer,
  options: Options,
) => Promise<Runtime>;

export function snapshotBytes(snapshot: Snapshot): Uint8Array {
  const root = snapshot.roots[0];
  if (!root) throw new ToolsError("internal", "missing byte result");
  const data = root.data;
  if (typeof data === "object" && "String" in data) return data.String.slice();
  if (typeof data !== "object" || !("Slice" in data))
    throw new ToolsError("internal", "invalid byte result");
  const { storage, start, length } = data.Slice;
  if (storage.path.length) throw new ToolsError("internal", "invalid byte storage");
  const backing = snapshot.objects[Number(storage.object)]?.data;
  if (!backing || typeof backing !== "object")
    throw new ToolsError("internal", "missing byte storage");
  const bytes =
    "String" in backing ? backing.String : "Array" in backing ? backing.Array : undefined;
  if (!bytes || start < 0n || length < 0n || start + length > BigInt(bytes.length))
    throw new ToolsError("internal", "invalid byte range");
  if (bytes instanceof Uint8Array) return bytes.slice(Number(start), Number(start + length));
  const result = new Uint8Array(Number(length));
  for (let i = 0; i < result.length; i++) {
    const value = bytes[Number(start) + i];
    result[i] =
      typeof value.data === "object" && "Unsigned" in value.data ? Number(value.data.Unsigned) : 0;
  }
  return result;
}

/** Owns one compiler worker and serializes its bounded request queue. */
export class LanguageService {
  private runtime!: Runtime;
  private sequence = 0n;
  private session = "";
  private revision = "";
  private snapshot = "";
  private queue: Promise<unknown> = Promise.resolve();
  private pending = 0;
  private closing = false;
  private closePromise?: Promise<void>;
  private faulted = false;
  private epoch = 1n;
  private recovery?: Record<string, unknown>;
  private uncertain?: Record<string, unknown>;
  private controls = new Map<string, { canceled: boolean; finish?: (value: Uint8Array) => void }>();
  private constructor(
    private readonly factory: RuntimeFactory,
    private image: Uint8Array | ArrayBuffer,
    private readonly options: Options,
  ) {}
  static async create(
    factory: RuntimeFactory,
    image: Uint8Array | ArrayBuffer,
    options: Options = {},
  ): Promise<LanguageService> {
    const service = new LanguageService(factory, copyBytes(image), {
      ...options,
      capabilities: options.capabilities?.slice(),
      symbols: options.symbols ? copyBytes(options.symbols) : undefined,
      providerModule: options.providerModule?.toString(),
      workerUrl: options.workerUrl?.toString(),
      wasmUrl: options.wasmUrl?.toString(),
      signal: undefined,
    });
    service.runtime = await service.createRuntime(options.signal);
    try {
      await service.request({ Operation: "hello" }, options.signal);
      return service;
    } catch (error) {
      await service.dispose();
      throw error;
    }
  }
  private createRuntime(signal?: AbortSignal): Promise<Runtime> {
    return this.factory(this.image, {
      ...this.options,
      signal,
      workload: "compiler",
      capabilities: [...new Set([...(this.options.capabilities ?? []), "minigo.tools.control"])],
      provider: (request) => this.control(request),
    });
  }
  private async control(request: HostRequest): Promise<Uint8Array> {
    if (request.route !== "minigo.tools.control") {
      if (this.options.provider) return this.options.provider(request);
      throw new ToolsError("provider", "unknown compiler route");
    }
    const { Operation, Token } = JSON.parse(new TextDecoder().decode(request.payload)) as {
      Operation: string;
      Token: string;
    };
    const state = this.controls.get(Token);
    if (!state) throw new ToolsError("stale", "unknown compiler request token");
    if (Operation === "finish") {
      state.finish?.(new Uint8Array());
      this.controls.delete(Token);
      return new Uint8Array();
    }
    if (Operation !== "wait" || state.finish)
      throw new ToolsError("invalid_argument", "invalid compiler control request");
    if (state.canceled) return new TextEncoder().encode("canceled");
    return new Promise((resolve, reject) => {
      const abort = () => {
        this.controls.delete(Token);
        reject(request.signal.reason ?? new Error("compiler stopped"));
      };
      state.finish = (bytes) => {
        request.signal.removeEventListener("abort", abort);
        resolve(bytes);
      };
      request.signal.addEventListener("abort", abort, { once: true });
      if (request.signal.aborted) abort();
    });
  }
  private enqueue<T>(action: () => Promise<T>, signal?: AbortSignal): Promise<T> {
    if (this.closing) return Promise.reject(new ToolsError("closed", "language service closed"));
    if (this.pending >= 128) return Promise.reject(new ToolsError("budget", "language queue full"));
    this.pending++;
    const operation = this.queue.then(() => {
      signal?.throwIfAborted();
      if (this.closing) throw new ToolsError("closed", "language service closed");
      return action();
    });
    this.queue = operation
      .catch(() => {})
      .finally(() => {
        this.pending--;
      });
    return operation;
  }
  request(input: Record<string, unknown>, signal?: AbortSignal): Promise<ToolsResponse> {
    input = structuredClone(input);
    return this.enqueue(async () => {
      if (this.faulted) {
        this.runtime = await this.createRuntime(signal);
        this.epoch++;
        this.faulted = false;
        this.controls.clear();
        const pending = this.uncertain;
        this.session = "";
        this.snapshot = "";
        try {
          if (this.recovery) await this.invoke(this.recovery, signal);
          if (pending) await this.invoke(pending, signal);
          this.uncertain = undefined;
          if (this.session) await this.invoke({ Operation: "workspace/analyze" }, signal);
        } catch (error) {
          this.faulted = true;
          this.runtime.terminate(error);
          throw error;
        }
      }
      if (
        ["workspace/open", "workspace/update", "document/update"].includes(String(input.Operation))
      )
        this.uncertain = input;
      try {
        return await this.invoke(input, signal);
      } finally {
        if (!this.faulted) this.uncertain = undefined;
      }
    }, signal);
  }
  private async invoke(
    input: Record<string, unknown>,
    signal?: AbortSignal,
  ): Promise<ToolsResponse> {
    signal?.throwIfAborted();
    const token = String(++this.sequence);
    const control: { canceled: boolean; finish?: (bytes: Uint8Array) => void } = {
      canceled: false,
    };
    this.controls.set(token, control);
    let hardStop: ReturnType<typeof setTimeout> | undefined;
    const cancel = () => {
      control.canceled = true;
      control.finish?.(new TextEncoder().encode("canceled"));
      if (hardStop === undefined)
        hardStop = setTimeout(() => {
          this.faulted = true;
          this.runtime.terminate();
        }, 2000);
    };
    signal?.addEventListener("abort", cancel, { once: true });
    const timer = setTimeout(cancel, 30_000);
    try {
      const request = {
        Format: "mini-go-tools",
        Version: 2,
        Session: this.session,
        Revision: this.revision,
        ...input,
        Token: token,
        Epoch: String(this.epoch),
        Deadline: input.Deadline ?? String((BigInt(Date.now()) + 30_000n) * 1_000_000n),
      };
      const execution = this.runtime.start("tools", [
        values.bytes(new TextEncoder().encode(JSON.stringify(request))),
      ]);
      const response = JSON.parse(
        new TextDecoder().decode(snapshotBytes(await execution.result)),
      ) as ToolsResponse;
      await execution.settled;
      if (response.Format !== "mini-go-tools" || response.Version !== 2)
        throw new ToolsError("identity", "unsupported compiler tools response");
      if (response.Error) {
        throw new ToolsError(response.Error.Code, response.Error.Message);
      }
      this.session = response.Session;
      if (response.Revision) this.revision = response.Revision;
      if (response.Analysis) this.snapshot = response.Analysis.Snapshot;
      if (response.Recovery) this.recovery = structuredClone(response.Recovery);
      return response;
    } catch (error) {
      if (!(error instanceof ToolsError) || error.code === "identity") {
        this.faulted = true;
        this.runtime.terminate(error);
      }
      throw error;
    } finally {
      clearTimeout(timer);
      clearTimeout(hardStop);
      signal?.removeEventListener("abort", cancel);
      this.controls.delete(token);
    }
  }
  async open(input: WorkspaceInput, signal?: AbortSignal): Promise<string> {
    return (await this.request({ Operation: "workspace/open", ...input }, signal)).Revision!;
  }
  async update(changes: DocumentUpdate[], signal?: AbortSignal): Promise<string> {
    return (await this.request({ Operation: "document/update", Changes: changes }, signal))
      .Revision!;
  }
  async replaceWorkspace(
    input: WorkspaceInput,
    changes: DocumentUpdate[] = [],
    signal?: AbortSignal,
  ): Promise<string> {
    return (
      await this.request({ Operation: "workspace/update", ...input, Changes: changes }, signal)
    ).Revision!;
  }
  async analyze(signal?: AbortSignal): Promise<Analysis> {
    return (await this.request({ Operation: "workspace/analyze" }, signal)).Analysis!;
  }
  async query<T = unknown>(
    operation: string,
    parameters: Record<string, unknown> = {},
    signal?: AbortSignal,
  ): Promise<T> {
    return (
      await this.request(
        { Operation: `language/${operation}`, Query: { Snapshot: this.snapshot, ...parameters } },
        signal,
      )
    ).Value as T;
  }
  async sources(trees: SourceTree[], signal?: AbortSignal): Promise<SourcePackages> {
    return (await this.request({ Operation: "workspace/sources", Trees: trees }, signal))
      .Value as SourcePackages;
  }
  prepare(options: Record<string, unknown> = {}, signal?: AbortSignal): Promise<ToolsResponse> {
    return this.request(
      { Operation: "build/prepare", Build: { Revision: this.revision, ...options } },
      signal,
    );
  }
  stats() {
    return this.runtime.stats();
  }
  upgrade(image: Uint8Array | ArrayBuffer, signal?: AbortSignal): Promise<void> {
    const bytes = copyBytes(image);
    return this.enqueue(async () => {
      const candidate = await LanguageService.create(this.factory, bytes, {
        ...this.options,
        signal,
      });
      let adopted = false;
      try {
        candidate.epoch = this.epoch + 1n;
        if (this.recovery) {
          await candidate.request(this.recovery, signal);
          await candidate.analyze(signal);
        }
        signal?.throwIfAborted();
        const previous = this.runtime;
        this.runtime = candidate.runtime;
        this.image = candidate.image;
        this.controls = candidate.controls;
        this.session = candidate.session;
        this.revision = candidate.revision;
        this.snapshot = candidate.snapshot;
        this.recovery = candidate.recovery;
        this.epoch = candidate.epoch;
        this.sequence = candidate.sequence;
        this.uncertain = undefined;
        this.faulted = false;
        adopted = true;
        await previous.close();
      } finally {
        if (!adopted) await candidate.dispose();
      }
    }, signal);
  }
  dispose(): Promise<void> {
    if (this.closePromise) return this.closePromise;
    this.closing = true;
    for (const control of this.controls.values()) {
      control.canceled = true;
      control.finish?.(new TextEncoder().encode("canceled"));
    }
    this.closePromise = (async () => {
      const timeout = setTimeout(() => this.runtime.terminate(), 2000);
      try {
        await this.queue;
        if (!this.faulted) await this.runtime.close();
      } finally {
        clearTimeout(timeout);
        this.controls.clear();
        this.recovery = undefined;
        this.uncertain = undefined;
      }
    })();
    return this.closePromise;
  }
}
