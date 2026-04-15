const path = require('path');
const { spawn } = require('child_process');
const vscode = require('vscode');
const { LanguageClient } = require('vscode-languageclient/node');

let client;
let outputChannel;
const scriptExtension = '.mgo';
const diagnosticDebounceMs = 180;

function getServerPath(context) {
    const config = vscode.workspace.getConfiguration('go-mini');
    const customPath = config.get('lsp.path');
    if (customPath && customPath.trim() !== "") {
        return customPath;
    }
    // 默认回退到插件目录下的 bin/lsp-server
    return context.asAbsolutePath(path.join('bin', 'lsp-server'));
}

function getExecPath(context) {
    const config = vscode.workspace.getConfiguration('go-mini');
    const customPath = config.get('exec.path');
    if (customPath && customPath.trim() !== "") {
        return customPath;
    }
    // 默认回退到插件目录下的 bin/mini-exec
    return context.asAbsolutePath(path.join('bin', 'mini-exec'));
}

async function runFile(context, uri) {
    const targetDir = await resolveTargetDirectory(uri);
    if (!targetDir) {
        vscode.window.showErrorMessage('No package directory to run');
        return;
    }

    await saveTargetFile(uri);
    const execPath = getExecPath(context);
    await executeCommand({
        title: 'Run Current Package',
        command: execPath,
        args: ['-run', targetDir],
        cwd: targetDir
    });
}

async function compileFile(context, uri) {
    const targetDir = await resolveTargetDirectory(uri);
    const filePath = await resolveTargetFile(uri);
    if (!targetDir || !filePath) {
        vscode.window.showErrorMessage('No package directory to compile');
        return;
    }

    await saveTargetFile(uri);
    const defaultOutput = path.join(targetDir, `${path.basename(targetDir)}.json`);
    const targetUri = await vscode.window.showSaveDialog({
        defaultUri: vscode.Uri.file(defaultOutput),
        filters: {
            'Go-Mini Bytecode': ['json']
        }
    });
    if (!targetUri) {
        return;
    }

    const execPath = getExecPath(context);
    const ok = await executeCommand({
        title: 'Compile Current Package',
        command: execPath,
        args: ['-o', targetUri.fsPath, targetDir],
        cwd: targetDir
    });
    if (ok) {
        vscode.window.showInformationMessage(`Go-Mini bytecode written to ${targetUri.fsPath}`);
    }
}

async function disassembleFile(context, uri) {
    const targetDir = await resolveTargetDirectory(uri);
    if (!targetDir) {
        vscode.window.showErrorMessage('No package directory to disassemble');
        return;
    }

    await saveTargetFile(uri);
    const execPath = getExecPath(context);
    await executeCommand({
        title: 'Disassemble Current Package',
        command: execPath,
        args: ['-d', targetDir],
        cwd: targetDir
    });
}

async function resolveTargetFile(uri) {
    if (uri && uri.fsPath) {
        return uri.fsPath;
    }
    const editor = vscode.window.activeTextEditor;
    if (editor) {
        return editor.document.uri.fsPath;
    }
    return "";
}

async function resolveTargetDirectory(uri) {
    const filePath = await resolveTargetFile(uri);
    if (!filePath) {
        return "";
    }
    if (path.extname(filePath) !== scriptExtension) {
        return "";
    }
    return path.dirname(filePath);
}

async function saveTargetFile(uri) {
    if (uri && uri.fsPath) {
        const editor = vscode.window.activeTextEditor;
        if (editor && editor.document.uri.fsPath === uri.fsPath) {
            await editor.document.save();
        }
        return;
    }
    const editor = vscode.window.activeTextEditor;
    if (editor) {
        await editor.document.save();
    }
}

function getOutputChannel() {
    if (!outputChannel) {
        outputChannel = vscode.window.createOutputChannel('Go-Mini');
    }
    return outputChannel;
}

function executeCommand({ title, command, args, cwd }) {
    const channel = getOutputChannel();
    channel.clear();
    channel.appendLine(`> ${title}`);
    channel.appendLine(`$ ${command} ${args.join(' ')}`);
    channel.appendLine('');
    channel.show(true);

    return new Promise((resolve) => {
        const child = spawn(command, args, { cwd });

        child.stdout.on('data', (data) => {
            channel.append(data.toString());
        });
        child.stderr.on('data', (data) => {
            channel.append(data.toString());
        });
        child.on('error', (err) => {
            channel.appendLine(`\n[spawn error] ${err.message}`);
            vscode.window.showErrorMessage(`Go-Mini command failed to start: ${err.message}`);
            resolve(false);
        });
        child.on('close', (code) => {
            if (code === 0) {
                channel.appendLine('');
                channel.appendLine('[done]');
                resolve(true);
                return;
            }
            channel.appendLine('');
            channel.appendLine(`[exit ${code}]`);
            vscode.window.showErrorMessage(`Go-Mini command failed with exit code ${code}`);
            resolve(false);
        });
    });
}

async function startClient(context) {
    const serverPath = getServerPath(context);

    let serverOptions = {
        command: serverPath,
        args: []
    };

    const pendingChanges = new Map();
    let clientOptions = {
        documentSelector: [{ scheme: 'file', language: 'go-mini' }],
        middleware: {
            didChange: (event, next) => {
                const document = getDocumentFromEvent(event);
                if (!document || !document.uri) {
                    return next(event);
                }
                const key = document.uri.toString();
                const existing = pendingChanges.get(key);
                if (existing) {
                    clearTimeout(existing.timer);
                }
                const timer = setTimeout(() => {
                    pendingChanges.delete(key);
                    next(event);
                }, diagnosticDebounceMs);
                pendingChanges.set(key, { timer });
            },
            didSave: async (document, next) => {
                const key = document && document.uri ? document.uri.toString() : "";
                if (key) {
                    const existing = pendingChanges.get(key);
                    if (existing) {
                        clearTimeout(existing.timer);
                        pendingChanges.delete(key);
                    }
                }
                return next(document);
            },
            didClose: async (document, next) => {
                const key = document && document.uri ? document.uri.toString() : "";
                if (key) {
                    const existing = pendingChanges.get(key);
                    if (existing) {
                        clearTimeout(existing.timer);
                        pendingChanges.delete(key);
                    }
                }
                return next(document);
            }
        }
    };

    client = new LanguageClient(
        'goMiniLSP',
        'Go-Mini Language Server',
        serverOptions,
        clientOptions
    );

    await client.start();
}

function getDocumentFromEvent(event) {
    if (!event) {
        return undefined;
    }
    return event.document || event;
}

async function restartServer(context) {
    if (client) {
        await client.stop();
    }
    await startClient(context);
    vscode.window.showInformationMessage('Go-Mini Language Server restarted');
}

function activate(context) {
    outputChannel = vscode.window.createOutputChannel('Go-Mini');
    context.subscriptions.push(outputChannel);

    // 注册重启命令
    context.subscriptions.push(
        vscode.commands.registerCommand('go-mini.restartServer', () => restartServer(context))
    );

    // 运行/编译/反汇编按当前 .mgo 文件所在目录作为包入口。
    context.subscriptions.push(
        vscode.commands.registerCommand('go-mini.runFile', (uri) => runFile(context, uri))
    );
    context.subscriptions.push(
        vscode.commands.registerCommand('go-mini.compileFile', (uri) => compileFile(context, uri))
    );
    context.subscriptions.push(
        vscode.commands.registerCommand('go-mini.disassembleFile', (uri) => disassembleFile(context, uri))
    );

    // 监听配置变更自动重启
    context.subscriptions.push(
        vscode.workspace.onDidChangeConfiguration(e => {
            if (e.affectsConfiguration('go-mini.lsp.path')) {
                restartServer(context);
            }
        })
    );

    startClient(context);
}

function deactivate() {
    if (!client) {
        return undefined;
    }
    return client.stop();
}

module.exports = {
    activate,
    deactivate
};
