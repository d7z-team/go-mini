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
    let filePath;
    if (uri && uri.fsPath) {
        filePath = uri.fsPath;
    } else {
        const editor = vscode.window.activeTextEditor;
        if (editor) {
            filePath = editor.document.uri.fsPath;
        }
    }

    if (!filePath) {
        vscode.window.showErrorMessage('No file to run');
        return;
    }

    const execPath = getExecPath(context);
    const terminal = vscode.window.activeTerminal || vscode.window.createTerminal('Go-Mini');
    terminal.show();
    
    // 引号处理防止路径空格
    terminal.sendText(`"${execPath}" "${filePath}"`);
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
