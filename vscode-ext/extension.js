const path = require('path');
const vscode = require('vscode');
const { LanguageClient } = require('vscode-languageclient/node');

let client;

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
    const filePath = await resolveTargetFile(uri);
    if (!filePath) {
        vscode.window.showErrorMessage('No file to run');
        return;
    }

    await saveTargetFile(uri);
    const execPath = getExecPath(context);
    executeInTerminal('Go-Mini', `"${execPath}" -run "${filePath}"`);
}

async function compileFile(context, uri) {
    const filePath = await resolveTargetFile(uri);
    if (!filePath) {
        vscode.window.showErrorMessage('No file to compile');
        return;
    }

    await saveTargetFile(uri);
    const defaultOutput = filePath.replace(/\.[^.]+$/, '.json');
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
    executeInTerminal('Go-Mini Compile', `"${execPath}" -o "${targetUri.fsPath}" "${filePath}"`);
}

async function disassembleFile(context, uri) {
    const filePath = await resolveTargetFile(uri);
    if (!filePath) {
        vscode.window.showErrorMessage('No file to disassemble');
        return;
    }

    await saveTargetFile(uri);
    const execPath = getExecPath(context);
    executeInTerminal('Go-Mini Disasm', `"${execPath}" -d "${filePath}"`);
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

function executeInTerminal(name, command) {
    const terminal = vscode.window.activeTerminal || vscode.window.createTerminal(name);
    terminal.show();
    terminal.sendText(command);
}

async function startClient(context) {
    const serverPath = getServerPath(context);

    let serverOptions = {
        command: serverPath,
        args: []
    };

    let clientOptions = {
        documentSelector: [{ scheme: 'file', language: 'go-mini' }]
    };

    client = new LanguageClient(
        'goMiniLSP',
        'Go-Mini Language Server',
        serverOptions,
        clientOptions
    );

    await client.start();
}

async function restartServer(context) {
    if (client) {
        await client.stop();
    }
    await startClient(context);
    vscode.window.showInformationMessage('Go-Mini Language Server restarted');
}

function activate(context) {
    // 注册重启命令
    context.subscriptions.push(
        vscode.commands.registerCommand('go-mini.restartServer', () => restartServer(context))
    );

    // 注册运行命令
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
