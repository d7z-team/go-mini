const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const test = require('node:test');

test('server startup, queued restart, failure and shutdown release ownership', async () => {
    let configured = '';
    let restart;
    let changed;
    let watchers = 0;
    let active = 0;
    let fail = false;
    const commands = [];
    const options = [];
    const errors = [];
    const vscode = {
        workspace: {
            getConfiguration: () => ({ get: (key, fallback) => key === 'lsp.path' ? configured : fallback }),
            createFileSystemWatcher: () => {
                watchers++;
                let disposed = false;
                return { dispose() { assert.equal(disposed, false); disposed = true; watchers--; } };
            },
            onDidChangeConfiguration(callback) { changed = callback; return { dispose() {} }; },
        },
        commands: { registerCommand(name, callback) { restart = callback; return { dispose() {} }; } },
        window: { showErrorMessage(message) { errors.push(message); } },
    };
    class LanguageClient {
        constructor(id, name, server, config) { options.push(config); commands.push(server); this.running = false; }
        async start() {
            assert.equal(active, 0);
            await Promise.resolve();
            if (fail) { fail = false; throw new Error('controlled startup failure'); }
            this.running = true;
            active++;
        }
        async dispose() {
            await Promise.resolve();
            if (this.running) { this.running = false; active--; }
        }
    }
    const sandbox = {
        module: { exports: {} },
        require(name) {
            if (name === 'vscode') return vscode;
            if (name === 'vscode-languageclient/node') return { LanguageClient };
            throw new Error(name);
        },
    };
    vm.runInNewContext(fs.readFileSync(require.resolve('./extension.js'), 'utf8'), sandbox);
    const extension = sandbox.module.exports;
    const tick = () => new Promise(resolve => setImmediate(resolve));
    extension.activate({ subscriptions: [] });
    await tick();
    assert.equal(commands[0].command, 'mini-go');
    assert.equal(commands[0].args.join(' '), 'lsp');
    assert.equal(options[0].initializationOptions.module, 'app');
    assert.equal(options[0].initializationOptions.sources.length, 0);
    assert.equal(active, 1);
    assert.equal(watchers, 1);
    configured = '/controlled/bin/mini-go';
    changed({ affectsConfiguration: () => true });
    await Promise.all([restart(), restart()]);
    assert.equal(commands.at(-1).command, configured);
    assert.equal(active, 1);
    assert.equal(watchers, 1);
    fail = true;
    await restart();
    assert.equal(active, 0);
    assert.equal(watchers, 0);
    assert.match(errors.at(-1), /controlled startup failure/);
    await restart();
    const pending = restart();
    await extension.deactivate();
    await pending;
    assert.equal(active, 0);
    assert.equal(watchers, 0);
});
