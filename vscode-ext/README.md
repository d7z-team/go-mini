# Go-Mini Extension

Commands:

- `Go-Mini: Run Current Package`
- `Go-Mini: Compile Current Package to Bytecode`
- `Go-Mini: Disassemble Current Package`

Language files:

- `*.mgo`

Behavior:

- Commands run against the current `.mgo` file's containing directory, matching the backend's package-directory model.
- Run/compile/disassemble output is shown in a dedicated `Go-Mini` output panel instead of the active terminal.
