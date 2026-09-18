export function call() {
  // Exercise unexpected worker exit, including a clean exit code.
  process.exit(0);
}
