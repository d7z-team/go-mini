package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/ffilib/fmtlib"
)

const megaPipelineHelperModule = `
package helper

import "strings"

var HelperBoot = "helper-boot"
var Counter = 1

func Next() int {
	Counter = Counter + 1
	return Counter
}

func MakeAdder(seed int) func(int) int {
	total := seed
	return func(v int) int {
		total = total + v
		return total
	}
}

func Join(parts []string) string {
	return strings.Join(parts, "/")
}

func MakeDescriber(prefix string) any {
	obj := make(map[string]any)
	obj["Describe"] = func() string {
		return prefix + ":" + String(Counter)
	}
	return obj
}
`

type megaOutputRecorder struct {
	sb strings.Builder
}

func (o *megaOutputRecorder) Print(_ context.Context, s string) {
	o.sb.WriteString(s)
}

type megaPipelineSpec struct {
	source            string
	lineCount         int
	expectedOutput    string
	expectedTrace     string
	expectedJSONTrace string
	expectedScore     int64
	expectedPhases    int64
	expectedLast      string
}

func TestMegaPipelineOver1000Lines(t *testing.T) {
	spec := buildMegaPipelineSpec(180)
	if spec.lineCount <= 1000 {
		t.Fatalf("mega pipeline should exceed 1000 lines, got %d", spec.lineCount)
	}

	t.Run("source_runtime", func(t *testing.T) {
		exec, _ := buildPipelineFixture(t, "helper", megaPipelineHelperModule, spec.source)
		prog, err := exec.NewRuntimeByGoCode(spec.source)
		if err != nil {
			t.Fatalf("new runtime by source failed: %v", err)
		}
		assertMegaPipelineExecution(t, prog, spec)
	})

	t.Run("compiled_prepared", func(t *testing.T) {
		exec, compiled := buildPipelineFixture(t, "helper", megaPipelineHelperModule, spec.source)
		prog, err := exec.NewRuntimeByCompiled(compiled)
		if err != nil {
			t.Fatalf("new runtime by compiled failed: %v", err)
		}
		assertMegaPipelineExecution(t, prog, spec)
	})

	t.Run("bytecode_roundtrip", func(t *testing.T) {
		exec, compiled := buildPipelineFixture(t, "helper", megaPipelineHelperModule, spec.source)
		payload, err := compiled.MarshalBytecodeJSON()
		if err != nil {
			t.Fatalf("marshal bytecode failed: %v", err)
		}
		prog, err := exec.NewRuntimeByBytecodeJSON(payload)
		if err != nil {
			t.Fatalf("new runtime by bytecode json failed: %v", err)
		}
		assertMegaPipelineExecution(t, prog, spec)
	})
}

func assertMegaPipelineExecution(t *testing.T, prog *engine.ExecutableProgram, spec megaPipelineSpec) {
	t.Helper()

	recorder := &megaOutputRecorder{}
	ctx := fmtlib.WithOutputter(context.Background(), recorder)
	if err := prog.Execute(ctx); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if recorder.sb.String() != spec.expectedOutput {
		t.Fatalf("unexpected output order")
	}

	snapshot := prog.SharedState()
	if snapshot == nil {
		t.Fatal("expected shared state snapshot")
	}
	if !snapshot.HasModule("helper") {
		t.Fatal("expected helper module in cache")
	}

	traceVar, ok := snapshot.LoadGlobal("trace")
	if !ok || traceVar == nil || traceVar.Str != spec.expectedTrace {
		t.Fatalf("unexpected trace: %#v", traceVar)
	}

	scoreVar, ok := snapshot.LoadGlobal("score")
	if !ok || scoreVar == nil || scoreVar.I64 != spec.expectedScore {
		t.Fatalf("unexpected score: %#v", scoreVar)
	}

	phaseVar, ok := snapshot.LoadGlobal("phaseCount")
	if !ok || phaseVar == nil || phaseVar.I64 != spec.expectedPhases {
		t.Fatalf("unexpected phaseCount: %#v", phaseVar)
	}

	lastVar, ok := snapshot.LoadGlobal("lastTag")
	if !ok || lastVar == nil || lastVar.Str != spec.expectedLast {
		t.Fatalf("unexpected lastTag: %#v", lastVar)
	}

	childVar, ok := snapshot.LoadGlobal("childSum")
	if !ok || childVar == nil || childVar.I64 != 7 {
		t.Fatalf("unexpected childSum: %#v", childVar)
	}

	readyVar, ok := snapshot.LoadGlobal("groupReady")
	if !ok || readyVar == nil || !readyVar.Bool {
		t.Fatalf("unexpected groupReady: %#v", readyVar)
	}

	ifaceVar, ok := snapshot.LoadGlobal("ifaceText")
	if !ok || ifaceVar == nil || ifaceVar.Str != "iface:1" {
		t.Fatalf("unexpected ifaceText: %#v", ifaceVar)
	}

	typeVar, ok := snapshot.LoadGlobal("typeText")
	if !ok || typeVar == nil || typeVar.Str != "str:hello" {
		t.Fatalf("unexpected typeText: %#v", typeVar)
	}

	jsonVar, ok := snapshot.LoadGlobal("resultJSON")
	if !ok || jsonVar == nil {
		t.Fatal("missing resultJSON")
	}
	obj, ok := runtimeVarToInterface(jsonVar).(map[string]interface{})
	if !ok {
		t.Fatalf("resultJSON should decode to map, got %T", runtimeVarToInterface(jsonVar))
	}
	if got, _ := obj["Trace"].(string); got != spec.expectedJSONTrace {
		t.Fatalf("unexpected json trace: %#v", obj["Trace"])
	}
	if got, _ := obj["Last"].(string); got != spec.expectedLast {
		t.Fatalf("unexpected json last: %#v", obj["Last"])
	}
	if got, _ := obj["Iface"].(string); got != "iface:1" {
		t.Fatalf("unexpected json iface: %#v", obj["Iface"])
	}
	if got, _ := obj["Kind"].(string); got != "str:hello" {
		t.Fatalf("unexpected json kind: %#v", obj["Kind"])
	}
	assertPayloadNumber(t, obj["Score"], spec.expectedScore)
	assertPayloadNumber(t, obj["Count"], spec.expectedPhases)
	assertPayloadNumber(t, obj["Child"], 7)
	assertPayloadNumber(t, obj["Counter"], 2)
}

func buildMegaPipelineSpec(phases int) megaPipelineSpec {
	var src strings.Builder

	writeLine := func(s string) {
		src.WriteString(s)
		src.WriteByte('\n')
	}

	writeLine("package main")
	writeLine("")
	writeLine(`import "helper"`)
	writeLine(`import "encoding/json"`)
	writeLine(`import "fmt"`)
	writeLine(`import "time"`)
	writeLine("")
	writeLine("type Snapshot struct {")
	writeLine("\tScore int")
	writeLine("\tTrace string")
	writeLine("\tCount int")
	writeLine("\tLast string")
	writeLine("\tChild int")
	writeLine("\tIface string")
	writeLine("\tKind string")
	writeLine("\tReady bool")
	writeLine("\tCounter int")
	writeLine("}")
	writeLine("")
	writeLine("type Describer interface {")
	writeLine("\tDescribe() string")
	writeLine("}")
	writeLine("")
	writeLine("var bootValue = boot()")
	writeLine(`var trace = ""`)
	writeLine("var score = 0")
	writeLine("var phaseCount = 0")
	writeLine(`var lastTag = ""`)
	writeLine("var values []int")
	writeLine("var resultJSON any")
	writeLine(`var ifaceText = ""`)
	writeLine(`var typeText = ""`)
	writeLine("var childSum = 0")
	writeLine("var groupReady = false")
	writeLine("")
	writeLine("func boot() String {")
	writeLine(`	if helper.HelperBoot != "helper-boot" {`)
	writeLine(`		panic("helper boot mismatch")`)
	writeLine("	}")
	writeLine(`	fmt.Println("BOOT")`)
	writeLine(`	return "booted"`)
	writeLine("}")
	writeLine("")
	writeLine("func mark(label string) {")
	writeLine(`	trace = trace + "|" + label`)
	writeLine(`	fmt.Println(label)`)
	writeLine("}")
	writeLine("")
	writeLine("func risky(label string) string {")
	writeLine(`	defer mark("DEFER-" + label)`)
	writeLine("	defer func() {")
	writeLine("		if r := recover(); r != nil {")
	writeLine(`			mark("RECOVER-" + String(r))`)
	writeLine("		}")
	writeLine("	}()")
	writeLine(`	if label == "panic" {`)
	writeLine(`		panic("boom")`)
	writeLine("	}")
	writeLine(`	mark("BODY-" + label)`)
	writeLine(`	return label + ":ok"`)
	writeLine("}")
	writeLine("")
	writeLine("func useDesc(d Describer) string {")
	writeLine("\treturn d.Describe()")
	writeLine("}")
	writeLine("")
	writeLine("func checkType(v any) string {")
	writeLine("\tswitch x := v.(type) {")
	writeLine("\tcase string:")
	writeLine(`		return "str:" + x`)
	writeLine("\tcase int:")
	writeLine(`		return "int:" + String(x)`)
	writeLine("\tdefault:")
	writeLine(`		return "other"`)
	writeLine("\t}")
	writeLine("}")
	writeLine("")
	writeLine("func runInterfaceFlow() {")
	writeLine(`	ifaceText = useDesc(helper.MakeDescriber("iface").(Describer))`)
	writeLine(`	mark("IFACE")`)
	writeLine("}")
	writeLine("")
	writeLine("func runTypeFlow() {")
	writeLine(`	typeText = checkType("hello")`)
	writeLine(`	mark("TYPE")`)
	writeLine("}")
	writeLine("")
	writeLine("func runStringFlow() {")
	writeLine(`	lastTag = helper.Join([]string{"mega", "pipeline", "ok"})`)
	writeLine(`	if lastTag != "mega/pipeline/ok" {`)
	writeLine(`		panic("string flow mismatch")`)
	writeLine("	}")
	writeLine(`	mark("STRING")`)
	writeLine("}")
	writeLine("")
	writeLine("func runControlFlow() {")
	writeLine("\tlocal := 0")
	writeLine("\tfor _, i := range []int{0, 1, 2, 3, 4, 5} {")
	writeLine("\t\tswitch i {")
	writeLine("\t\tcase 0:")
	writeLine("\t\t\tlocal = local + 0")
	writeLine("\t\tcase 2:")
	writeLine("\t\t\tlocal = local + 2")
	writeLine("\t\tcase 3:")
	writeLine("\t\t\tlocal = local + 3")
	writeLine("\t\tcase 1:")
	writeLine("\t\t\tlocal = local + 1")
	writeLine("\t\t\tcontinue")
	writeLine("\t\tcase 4:")
	writeLine("\t\t\tlocal = local + 4")
	writeLine("\t\t\tbreak")
	writeLine("\t\tdefault:")
	writeLine("\t\t\tlocal = local + 5")
	writeLine("\t\t}")
	writeLine("\t\tlocal = local + 10")
	writeLine("\t}")
	writeLine("\tscore = score + 40")
	writeLine(`	mark("CONTROL")`)
	writeLine("}")
	writeLine("")
	writeLine("func runRangeFlow() {")
	writeLine("\tsum := 0")
	writeLine("\tarr := []int{3, 4, 5}")
	writeLine("\tfor i := range arr {")
	writeLine("\t\tswitch i {")
	writeLine("\t\tcase 0:")
	writeLine("\t\t\tsum = sum + 3")
	writeLine("\t\tcase 1:")
	writeLine("\t\t\tsum = sum + 4")
	writeLine("\t\tdefault:")
	writeLine("\t\t\tsum = sum + 5")
	writeLine("\t\t}")
	writeLine("\t}")
	writeLine(`	m := map[string]int{"a": 2, "b": 1}`)
	writeLine("\tfor k := range m {")
	writeLine(`		if k == "a" {`)
	writeLine("\t\t\tsum = sum + 2")
	writeLine("\t\t} else {")
	writeLine("\t\t\tsum = sum + 1")
	writeLine("\t\t}")
	writeLine("\t}")
	writeLine("\tscore = score + 15")
	writeLine(`	mark("RANGE")`)
	writeLine("}")
	writeLine("")
	writeLine("func runMultiAssign() {")
	writeLine("\ta, b := 1, 2")
	writeLine("\ta, b = b, a")
	writeLine("\tscore = score + 3")
	writeLine(`	mark("MULTI")`)
	writeLine("}")
	writeLine("")
	writeLine("func runTaskFlow() {")
	writeLine("\tadder := helper.MakeAdder(10)")
	writeLine("\tif adder(5) != 15 {")
	writeLine(`		panic("adder step 1")`)
	writeLine("\t}")
	writeLine("\tif adder(7) != 22 {")
	writeLine(`		panic("adder step 2")`)
	writeLine("\t}")
	writeLine("\tgo func(base int) {")
	writeLine("\t\tchildSum = helper.Next() + base")
	writeLine("\t}(5)")
	writeLine("\ttime.Sleep(1)")
	writeLine("\tif childSum != 7 {")
	writeLine(`		panic("unexpected go result")`)
	writeLine("\t}")
	writeLine("\tgo func() {")
	writeLine("\t\tgroupReady = true")
	writeLine("\t}()")
	writeLine("\ttime.Sleep(1)")
	writeLine(`	mark("TASK")`)
	writeLine("}")
	writeLine("")

	var phaseSum int64
	for i := 1; i <= phases; i++ {
		phaseName := fmt.Sprintf("PHASE-%03d", i)
		funcName := fmt.Sprintf("phase%03d", i)
		phaseSum += int64(i)
		writeLine(fmt.Sprintf("func %s() {", funcName))
		writeLine(fmt.Sprintf("\tscore = score + %d", i))
		writeLine("\tphaseCount = phaseCount + 1")
		writeLine(fmt.Sprintf("\tvalues = append(values, %d)", i))
		writeLine(fmt.Sprintf(`	lastTag = "%s"`, phaseName))
		writeLine(fmt.Sprintf(`	mark("%s")`, phaseName))
		writeLine("}")
		writeLine("")
	}

	writeLine("func runJSONFlow() {")
	writeLine("\tsnap := Snapshot{")
	writeLine("\t\tScore: score,")
	writeLine("\t\tTrace: trace,")
	writeLine("\t\tCount: phaseCount,")
	writeLine("\t\tLast: lastTag,")
	writeLine("\t\tChild: childSum,")
	writeLine("\t\tIface: ifaceText,")
	writeLine("\t\tKind: typeText,")
	writeLine("\t\tReady: groupReady,")
	writeLine("\t\tCounter: helper.Counter,")
	writeLine("\t}")
	writeLine("\traw, err := json.Marshal(snap)")
	writeLine("\tif err != nil {")
	writeLine("\t\tpanic(err)")
	writeLine("\t}")
	writeLine("\tdecoded, err := json.Unmarshal(raw)")
	writeLine("\tif err != nil {")
	writeLine("\t\tpanic(err)")
	writeLine("\t}")
	writeLine("\tresultJSON = decoded")
	writeLine(`	mark("JSON")`)
	writeLine("}")
	writeLine("")
	writeLine("func main() {")
	writeLine(`	if bootValue != "booted" {`)
	writeLine(`		panic("boot value mismatch")`)
	writeLine("\t}")
	writeLine(`	if risky("safe") != "safe:ok" {`)
	writeLine(`		panic("safe risky mismatch")`)
	writeLine("\t}")
	writeLine(`	risky("panic")`)
	writeLine("\trunInterfaceFlow()")
	writeLine("\trunTypeFlow()")
	writeLine("\trunStringFlow()")
	writeLine("\trunControlFlow()")
	writeLine("\trunRangeFlow()")
	writeLine("\trunMultiAssign()")
	writeLine("\trunTaskFlow()")
	for i := 1; i <= phases; i++ {
		writeLine(fmt.Sprintf("\tphase%03d()", i))
	}
	writeLine(fmt.Sprintf("\tif len(values) != %d {", phases))
	writeLine(`		panic("phase value count mismatch")`)
	writeLine("\t}")
	writeLine(fmt.Sprintf("\tif phaseCount != %d {", phases))
	writeLine(`		panic("phase counter mismatch")`)
	writeLine("\t}")
	writeLine("\trunJSONFlow()")
	writeLine(`	mark("END")`)
	writeLine("}")

	specialMarks := []string{
		"BODY-safe",
		"DEFER-safe",
		"RECOVER-boom",
		"DEFER-panic",
		"IFACE",
		"TYPE",
		"STRING",
		"CONTROL",
		"RANGE",
		"MULTI",
		"TASK",
	}

	// Rebuild output/trace in true execution order.
	var orderedOut strings.Builder
	var orderedTrace strings.Builder
	orderedOut.WriteString("BOOT\n")
	for _, label := range specialMarks {
		orderedOut.WriteString(label)
		orderedOut.WriteByte('\n')
		orderedTrace.WriteByte('|')
		orderedTrace.WriteString(label)
	}
	for i := 1; i <= phases; i++ {
		label := fmt.Sprintf("PHASE-%03d", i)
		orderedOut.WriteString(label)
		orderedOut.WriteByte('\n')
		orderedTrace.WriteByte('|')
		orderedTrace.WriteString(label)
	}
	jsonTrace := orderedTrace.String()
	for _, label := range []string{"JSON", "END"} {
		orderedOut.WriteString(label)
		orderedOut.WriteByte('\n')
		orderedTrace.WriteByte('|')
		orderedTrace.WriteString(label)
	}

	lineCount := strings.Count(src.String(), "\n")
	return megaPipelineSpec{
		source:            src.String(),
		lineCount:         lineCount,
		expectedOutput:    orderedOut.String(),
		expectedTrace:     orderedTrace.String(),
		expectedJSONTrace: jsonTrace,
		expectedScore:     phaseSum + 40 + 15 + 3,
		expectedPhases:    int64(phases),
		expectedLast:      fmt.Sprintf("PHASE-%03d", phases),
	}
}
