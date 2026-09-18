package types

import "testing"

func TestRelationsIdenticalHandlesAliasAndStructuralTypes(t *testing.T) {
	table := &TypeTable{}
	intType := Builtin(PrimitiveInt64)
	if err := table.Add(TypeNode{ID: "node.score", Kind: Named, Identity: TypeKey{ModulePath: "example", DeclID: "Score"}, Underlying: intType}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.alias", Kind: Named, Identity: TypeKey{ModulePath: "example", DeclID: "Alias"}, Alias: true, AliasTarget: TypeRef{Kind: Named, Node: "node.score"}}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.slice.a", Kind: Slice, Elem: intType}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.slice.b", Kind: Slice, Elem: intType}); err != nil {
		t.Fatal(err)
	}
	relations := NewRelations(table)
	score := TypeRef{Kind: Named, Node: "node.score"}
	alias := TypeRef{Kind: Named, Node: "node.alias"}
	if got := relations.Identical(alias, score); !got.OK {
		t.Fatalf("alias should be identical to target defined type: %#v", got)
	}
	if got := relations.ResolveAlias(alias); got != score {
		t.Fatalf("resolved alias = %#v, want %#v", got, score)
	}
	if got := relations.Identical(alias, intType); got.OK || got.Code != RelationNotIdentical {
		t.Fatalf("alias should not erase target identity to primitive: %#v", got)
	}
	if got := relations.UnderlyingIdentical(alias, intType); !got.OK {
		t.Fatalf("alias underlying should resolve to primitive: %#v", got)
	}
	left := TypeRef{Kind: Slice, Node: "node.slice.a"}
	right := TypeRef{Kind: Slice, Node: "node.slice.b"}
	if left.Equal(right) {
		t.Fatal("test setup requires different node identities")
	}
	if got := relations.Identical(left, right); !got.OK {
		t.Fatalf("structural slice identity should not depend on node id: %#v", got)
	}
}

func TestRelationsAssignablePreservesNamedAndWaitableRules(t *testing.T) {
	table := &TypeTable{}
	intType := Builtin(PrimitiveInt64)
	if err := table.Add(TypeNode{ID: "node.score", Kind: Named, Identity: TypeKey{ModulePath: "example", DeclID: "Score"}, Underlying: intType}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.level", Kind: Named, Identity: TypeKey{ModulePath: "example", DeclID: "Level"}, Underlying: intType}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.chan", Kind: Waitable, Direction: ChannelBoth, Elem: intType}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.recv", Kind: Waitable, Direction: ChannelReceive, Elem: intType}); err != nil {
		t.Fatal(err)
	}
	relations := NewRelations(table)
	score := TypeRef{Kind: Named, Node: "node.score"}
	level := TypeRef{Kind: Named, Node: "node.level"}
	if got := relations.Assignable(score, intType); !got.OK {
		t.Fatalf("defined type should remain assignable to unnamed primitive boundary: %#v", got)
	}
	if got := relations.Assignable(score, level); got.OK || got.Code != RelationBothTypesNamed {
		t.Fatalf("distinct defined types should not be assignable: %#v", got)
	}
	if got := relations.Assignable(TypeRef{Kind: Waitable, Node: "node.chan"}, TypeRef{Kind: Waitable, Node: "node.recv"}); !got.OK {
		t.Fatalf("bidirectional waitable should assign to receive waitable: %#v", got)
	}
	if got := relations.Assignable(TypeRef{Kind: Waitable, Node: "node.recv"}, TypeRef{Kind: Waitable, Node: "node.chan"}); got.OK {
		t.Fatalf("receive waitable should not assign to bidirectional waitable: %#v", got)
	}
}

func TestRelationsTreatsDefinedAnyAsEmptyInterface(t *testing.T) {
	table := &TypeTable{}
	if err := table.Add(TypeNode{
		ID:         "node.token",
		Kind:       Named,
		Identity:   TypeKey{ModulePath: "example", DeclID: "Token"},
		Underlying: AnyType(),
	}); err != nil {
		t.Fatal(err)
	}
	token := TypeRef{Kind: Named, Node: "node.token"}
	if _, ok := table.IsInterface(token); !ok {
		t.Fatal("defined type with any underlying type must be an interface")
	}
	if _, _, typeSet, ok := View(table, token).Interface(); !ok || typeSet {
		t.Fatalf("defined any view = interface %v, type set %v", ok, typeSet)
	}
	if got := NewRelations(table).Assignable(Builtin(PrimitiveString), token); !got.OK {
		t.Fatalf("ordinary value should assign to defined empty interface: %#v", got)
	}
}

func TestRelationsImplementsReturnsStableReasonCodes(t *testing.T) {
	table := &TypeTable{}
	intType := Builtin(PrimitiveInt64)
	stringType := Builtin(PrimitiveString)
	readerMethod := Method{Name: "Read", ModulePath: "example", Signature: FunctionSignature{Results: []TypeRef{intType}}}
	wrongMethod := Method{Name: "Read", ModulePath: "example", Signature: FunctionSignature{Results: []TypeRef{stringType}}}
	if err := table.Add(TypeNode{ID: "node.reader", Kind: Interface, Methods: []Method{readerMethod}}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.good", Kind: Struct, Methods: []Method{readerMethod}}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.empty", Kind: Struct}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.wrong", Kind: Struct, Methods: []Method{wrongMethod}}); err != nil {
		t.Fatal(err)
	}
	relations := NewRelations(table)
	reader := TypeRef{Kind: Interface, Node: "node.reader"}
	if got := relations.Implements(TypeRef{Kind: Struct, Node: "node.good"}, reader); !got.OK {
		t.Fatalf("matching method set should implement interface: %#v", got)
	}
	if got := relations.Implements(TypeRef{Kind: Struct, Node: "node.empty"}, reader); got.OK || got.Code != RelationMissingMethod {
		t.Fatalf("missing method should use stable reason code: %#v", got)
	}
	if got := relations.Implements(TypeRef{Kind: Struct, Node: "node.wrong"}, reader); got.OK || got.Code != RelationMethodSignatureMismatch {
		t.Fatalf("wrong method signature should use stable reason code: %#v", got)
	}
}

func TestRelationsPointerToAliasUsesTargetMethodSet(t *testing.T) {
	table := &TypeTable{}
	errorMethod := Method{Name: "Error", Signature: FunctionSignature{Results: []TypeRef{Builtin(PrimitiveString)}}}
	target := TypeRef{Kind: Named, Named: TypeKey{ModulePath: "io/fs", DeclID: "PathError"}, Node: "node.path-error"}
	alias := TypeRef{Kind: Named, Named: TypeKey{ModulePath: "os", DeclID: "PathError"}, Node: "node.alias"}
	if err := table.Add(TypeNode{ID: target.Node, Kind: Named, Identity: target.Named, Methods: []Method{errorMethod}}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: alias.Node, Kind: Named, Identity: alias.Named, Alias: true, AliasTarget: target}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.pointer", Kind: Pointer, Elem: alias}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.error", Kind: Interface, Methods: []Method{errorMethod}}); err != nil {
		t.Fatal(err)
	}
	result := NewRelations(table).Implements(
		TypeRef{Kind: Pointer, Node: "node.pointer"},
		TypeRef{Kind: Interface, Node: "node.error"},
	)
	if !result.OK {
		t.Fatalf("pointer to alias should implement target method set: %#v", result)
	}
}

func TestRelationsMethodIdentityUsesPackageOnlyForUnexportedNames(t *testing.T) {
	table := &TypeTable{}
	signature := FunctionSignature{Results: []TypeRef{Builtin(PrimitiveInt64)}}
	if err := table.Add(TypeNode{ID: "node.exported", Kind: Interface, Methods: []Method{{Name: "Read", Signature: signature}}}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.private", Kind: Interface, Methods: []Method{{Name: "read", ModulePath: "consumer", Signature: signature}}}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.value", Kind: Struct, Methods: []Method{
		{Name: "Read", ModulePath: "provider", Signature: signature},
		{Name: "read", ModulePath: "provider", Signature: signature},
	}}); err != nil {
		t.Fatal(err)
	}
	relations := NewRelations(table)
	value := TypeRef{Kind: Struct, Node: "node.value"}
	if got := relations.Implements(value, TypeRef{Kind: Interface, Node: "node.exported"}); !got.OK {
		t.Fatalf("exported method identity must not depend on package: %#v", got)
	}
	if got := relations.Implements(value, TypeRef{Kind: Interface, Node: "node.private"}); got.OK || got.Code != RelationMissingMethod {
		t.Fatalf("unexported method identity must retain package: %#v", got)
	}
}

func TestRelationsImplementsPromotedEmbeddedMethods(t *testing.T) {
	table := &TypeTable{}
	signature := FunctionSignature{Results: []TypeRef{Builtin(PrimitiveString)}}
	iface := TypeRef{Kind: Interface, Node: "node.stringer"}
	base := TypeRef{Kind: Named, Node: "node.base"}
	wrapper := TypeRef{Kind: Named, Node: "node.wrapper"}
	baseStruct := TypeRef{Kind: Struct, Node: "node.base.struct"}
	wrapperStruct := TypeRef{Kind: Struct, Node: "node.wrapper.struct"}
	method := Method{Name: "String", Receiver: base, Signature: signature}
	for _, node := range []TypeNode{
		{ID: iface.Node, Kind: Interface, Methods: []Method{{Name: "String", Signature: signature}}},
		{ID: baseStruct.Node, Kind: Struct},
		{ID: base.Node, Kind: Named, Underlying: baseStruct, Methods: []Method{method}},
		{ID: wrapperStruct.Node, Kind: Struct, Fields: []Field{{Name: "Base", Type: base, Embedded: true}}},
		{ID: wrapper.Node, Kind: Named, Underlying: wrapperStruct},
	} {
		if err := table.Add(node); err != nil {
			t.Fatal(err)
		}
	}
	if got := NewRelations(table).Implements(wrapper, iface); !got.OK {
		t.Fatalf("embedded value method should be promoted into method set: %#v", got)
	}
}

func TestRelationsImplementsNestedPromotedUnexportedMethods(t *testing.T) {
	table := &TypeTable{}
	signature := FunctionSignature{Results: []TypeRef{Builtin(PrimitiveInt)}}
	iface := TypeRef{Kind: Interface, Node: "node.node"}
	position := TypeRef{Kind: Named, Node: "node.position"}
	branch := TypeRef{Kind: Named, Node: "node.branch"}
	leaf := TypeRef{Kind: Named, Node: "node.leaf"}
	positionStruct := TypeRef{Kind: Struct, Node: "node.position.struct"}
	branchStruct := TypeRef{Kind: Struct, Node: "node.branch.struct"}
	leafStruct := TypeRef{Kind: Struct, Node: "node.leaf.struct"}
	for _, node := range []TypeNode{
		{ID: iface.Node, Kind: Interface, Methods: []Method{{Name: "position", ModulePath: "example", Signature: signature}}},
		{ID: positionStruct.Node, Kind: Struct},
		{ID: position.Node, Kind: Named, Underlying: positionStruct, Methods: []Method{{Name: "position", ModulePath: "example", Receiver: position, Signature: signature}}},
		{ID: branchStruct.Node, Kind: Struct, Fields: []Field{{Name: "Position", Type: position, Embedded: true}}},
		{ID: branch.Node, Kind: Named, Underlying: branchStruct},
		{ID: leafStruct.Node, Kind: Struct, Fields: []Field{{Name: "Branch", Type: branch, Embedded: true}}},
		{ID: leaf.Node, Kind: Named, Underlying: leafStruct},
	} {
		if err := table.Add(node); err != nil {
			t.Fatal(err)
		}
	}
	if got := NewRelations(table).Implements(leaf, iface); !got.OK {
		t.Fatalf("nested unexported method should be promoted into method set: %#v", got)
	}
}

func TestRelationsPredicatesUseTypeView(t *testing.T) {
	table := &TypeTable{}
	intType := Builtin(PrimitiveInt64)
	if err := table.Add(TypeNode{ID: "node.slice", Kind: Slice, Elem: intType}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.array", Kind: Array, Length: 3, Elem: intType}); err != nil {
		t.Fatal(err)
	}
	relations := NewRelations(table)
	if got := relations.MapKeyAllowed(TypeRef{Kind: Slice, Node: "node.slice"}); got.OK || got.Code != RelationNotComparable {
		t.Fatalf("slice map key should be rejected: %#v", got)
	}
	if got := relations.MapKeyAllowed(TypeRef{Kind: Array, Node: "node.array"}); !got.OK {
		t.Fatalf("array of comparable elements should be valid map key: %#v", got)
	}
	if got := relations.NilAssignable(TypeRef{Kind: Slice, Node: "node.slice"}); !got.OK {
		t.Fatalf("slice should accept nil: %#v", got)
	}
	if got := relations.Ordered(Builtin(PrimitiveComplex128)); got.OK || got.Code != RelationNotOrdered {
		t.Fatalf("complex should not be ordered: %#v", got)
	}
}

func TestRelationsSignatureIdenticalChecksVariadicAndStructuralTypes(t *testing.T) {
	table := &TypeTable{}
	intType := Builtin(PrimitiveInt64)
	if err := table.Add(TypeNode{ID: "node.slice.a", Kind: Slice, Elem: intType}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.slice.b", Kind: Slice, Elem: intType}); err != nil {
		t.Fatal(err)
	}
	left := FunctionSignature{Params: []TypeParam{{Type: TypeRef{Kind: Slice, Node: "node.slice.a"}}}, Variadic: true}
	right := FunctionSignature{Params: []TypeParam{{Type: TypeRef{Kind: Slice, Node: "node.slice.b"}}}, Variadic: true}
	relations := NewRelations(table)
	if got := relations.SignatureIdentical(left, right); !got.OK {
		t.Fatalf("structurally identical variadic signatures should match: %#v", got)
	}
	right.Variadic = false
	if got := relations.SignatureIdentical(left, right); got.OK || got.Code != RelationSignatureVariadicMismatch {
		t.Fatalf("variadic mismatch should use stable reason code: %#v", got)
	}
}
