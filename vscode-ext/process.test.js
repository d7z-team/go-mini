const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const vm = require('node:vm');
const { spawn } = require('node:child_process');
const { once } = require('node:events');
const test = require('node:test');

test('default PATH and configured executable start a controlled server process', { skip: process.platform === 'win32' }, async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'mini-go process '));
    const executable = path.join(directory, 'mini-go');
    fs.writeFileSync(executable, `#!${process.execPath}\nprocess.stdout.write(JSON.stringify(process.argv.slice(2))+'\\n'); process.stdin.resume();\n`, { mode: 0o700 });
    let configured = '';
    let restart;
    let watchers = 0;
    const errors = [];
    const children = new Set();
    const vscode = {
        workspace: {
            getConfiguration: () => ({ get: () => configured }),
            createFileSystemWatcher: () => { watchers++; return { dispose() { watchers--; } }; },
            onDidChangeConfiguration: () => ({ dispose() {} }),
        },
        commands: { registerCommand: (_, callback) => { restart = callback; return { dispose() {} }; } },
        window: { showErrorMessage: error => errors.push(error) },
    };
    class LanguageClient {
        constructor(_, __, server) { this.server = server; }
        async start() {
            this.child = spawn(this.server.command, this.server.args, { env: { ...process.env, PATH: directory + path.delimiter + process.env.PATH } });
            children.add(this.child);
            this.closed = new Promise(resolve => this.child.once('close', () => { children.delete(this.child); resolve(); }));
            const [output] = await Promise.race([once(this.child.stdout, 'data'), once(this.child, 'error').then(([error]) => { throw error; })]);
            assert.deepEqual(JSON.parse(output.toString()), ['lsp']);
        }
        async dispose() {
            if (this.child && this.child.pid && this.child.exitCode === null) {
                this.child.kill();
            }
            if (this.closed) await this.closed;
        }
    }
    const sandbox = { module: { exports: {} }, require: name => name === 'vscode' ? vscode : { LanguageClient } };
    vm.runInNewContext(fs.readFileSync(require.resolve('./extension.js'), 'utf8'), sandbox);
    const extension = sandbox.module.exports;
    try {
        extension.activate({ subscriptions: [] });
        await restart();
        assert.equal(watchers, 1);
        configured = executable;
        await restart();
        assert.equal(watchers, 1);
        configured = path.join(directory, 'missing');
        await restart();
        assert.equal(watchers, 0);
        assert.match(errors.at(-1), /ENOENT/);
    } finally {
        await extension.deactivate();
        await new Promise(resolve => setImmediate(resolve));
        assert.equal(children.size, 0);
        fs.rmSync(directory, { recursive: true, force: true });
    }
});
