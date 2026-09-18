const vscode = require('vscode');
const { LanguageClient } = require('vscode-languageclient/node');

let client;
let watcher;
let pending = Promise.resolve();
let disposed = false;

function serverPath() {
    const configured = vscode.workspace.getConfiguration('mini-go').get('lsp.path');
    return configured && configured.trim() !== ''
        ? configured
        : 'mini-go';
}

async function stop() {
    const previous = client;
    client = undefined;
    try {
        if (previous) await previous.dispose();
    } finally {
        if (watcher) watcher.dispose();
        watcher = undefined;
    }
}

async function start() {
    watcher = vscode.workspace.createFileSystemWatcher('**/*.{mgo,mrpc}');
    try {
    client = new LanguageClient(
        'miniGoLSP',
        'Mini-Go Language Server',
        { command: serverPath(), args: ['lsp'] },
        {
            documentSelector: [{ scheme: 'file', language: 'mini-go' }],
            initializationOptions: {
                module: vscode.workspace.getConfiguration('mini-go').get('module', 'app'),
                sources: vscode.workspace.getConfiguration('mini-go').get('sources', []),
            },
            synchronize: { fileEvents: watcher },
        },
    );
        await client.start();
    } catch (error) {
        await stop();
        throw error;
    }
}

function reportStartupError(error) {
    vscode.window.showErrorMessage(`Mini-Go LSP: ${error.message}. Install mini-go on PATH or set mini-go.lsp.path to its executable path.`);
}

function restart() {
    pending = pending.catch(() => {}).then(async () => {
        await stop();
        if (!disposed) await start();
    });
    return pending;
}

function activate(context) {
    disposed = false;
    context.subscriptions.push(
        vscode.commands.registerCommand('mini-go.restartServer', () =>
            restart().catch(reportStartupError),
        ),
        vscode.workspace.onDidChangeConfiguration((event) => {
            if (event.affectsConfiguration('mini-go')) {
                restart().catch(reportStartupError);
            }
        }),
    );
    restart().catch(reportStartupError);
}

function deactivate() {
    disposed = true;
    pending = pending.catch(() => {}).then(stop);
    return pending;
}

module.exports = { activate, deactivate };
